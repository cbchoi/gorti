# LocalLRC fast path

LocalLRC is the Go SDK's same-host fast path for receive-order attribute
updates and interactions. The authoritative federation, declaration,
ownership, Object Management, and Time Management state remains in `rtid`.

## Completion semantics

The default Go receive-order methods return after a bounded local queue accepts
an immutable copy of the operation. A successful return means local admission;
it does not mean that `rtid` accepted the operation or that a subscriber
processed its callback.

One sender goroutine transmits a gapless FIFO sequence over a persistent
stream. A cumulative ACK for sequence N means every operation through N
completed its normal server service path. Subscriber callback delivery is a
separate measurement and completion boundary.

TSO operations and message retraction remain on confirmed paths. LocalLRC does
not change logical-time or grant semantics.

## Choosing a boundary

Use the standard API when local admission is the intended boundary:

```go
err := fed.SendInteractionByHandle(ctx, classHandle, parameters, nil)
```

Use the queued API when the application needs a completion token:

```go
sequence, err := fed.QueueInteractionByHandle(ctx, classHandle, parameters)
if err != nil {
    return err
}
if err := fed.AwaitLocalLRC(ctx, sequence); err != nil {
    return err
}
```

`QueueAttributeValuesByHandle` follows the same contract.
`FlushLocalLRC` waits for a cumulative ACK through the queue snapshot taken by
the call. `Resign` stops new admission, drains required work, and closes the
stream before membership is removed.

## SDK admission modes

The Go SDK exposes the completion contract as
`ConnectOptions.AdmissionMode`:

- **`pipelined`** (default, also selected by the empty string) — standard
  receive-order calls complete on bounded LocalLRC local admission. IEEE 1516
  requires no server confirmation for receive-order delivery, so this is the
  production mode. The pipelined LocalLRC transport, the shared synchronous
  streams, and the fixed transport contract (8192-entry outbox, 32-op batches,
  1 ms flush, 32-op LRC frames, ack-every 32) all stay active.
- **`confirmed`** — a documented **debug option**. Every standard
  receive-order call waits for the server result on the unary confirmed path;
  the LocalLRC fast path and shared streams are forced off for the
  Connection. Use it to isolate admission behavior or to obtain a
  server-confirmed return boundary in experiments; it is not the production
  default.

```go
// Default: pipelined LocalLRC admission.
conn, err := federate.ConnectWithOptions(ctx, address, federate.ConnectOptions{})

// Debug: server-confirmed unary path.
conn, err = federate.ConnectWithOptions(ctx, address, federate.ConnectOptions{
    AdmissionMode: federate.AdmissionModeConfirmed,
})

err = fed.SendInteractionByHandleConfirmed(ctx, classHandle, parameters, nil)
```

The older `ReceiveOrderTransport: federate.ReceiveOrderTransportConfirmed`
option remains honored for compatibility; `AdmissionModeConfirmed` implies it
and additionally forces the unary path. Setting `AdmissionMode: "pipelined"`
together with `ReceiveOrderTransportConfirmed` is rejected as contradictory.

Transport selection is independent of dial tuning: supplying
`ExtraDialOptions` (keepalive, window sizes, custom dialers) does **not**
downgrade the SDK to the unary path. Only per-RPC bearer-token auth options
(`BearerToken` / `BearerTokenProvider`) currently disable the long-lived
streams.

The fair-comparison command exposes the same choice. LocalLRC is the
receive-order default:

```powershell
verification\portico\RunComparison.ps1
```

Use `-GortiTransport confirmed` only when the experiment requires a
server-confirmed return boundary.

The same choice can be stored in a validated JSON profile:

```powershell
verification\portico\RunComparison.ps1 `
  -TransportConfig verification\profiles\local-lrc.json
```

`verification/profiles/confirmed.json` selects confirmed mode. Command-line
values take precedence over profile values, and the selected file's SHA-256 is
recorded in comparison metadata.

## Capacity, frames, and ACKs

Configure the maximum number of admitted but unacknowledged operations, the
requested transport-frame size, and the requested ACK interval when connecting:

```go
conn, err := federate.ConnectWithOptions(ctx, address, federate.ConnectOptions{
    LocalLRCQueueCapacity: 1024,
    LocalLRCAckEvery:      32,
    LocalLRCBatchSize:     32,
})
```

Capacity includes operations waiting in the client queue and operations sent
but not cumulatively acknowledged. Batch size is negotiated independently of
the ACK interval. Zero selects the compatible default of 32 operations;
explicit requests may be 32, 64, 128, or 256, and the server clamps the result
to its advertised maximum. Concurrent admissions share one observable FIFO
order, and cancellation does not create a sequence gap.

`Federate.LocalLRCStats()` reports the requested size, peer limit, and largest
frame actually sent. These values make it possible to verify that a benchmark
used the intended transport contract rather than inferring batching from
elapsed time.

## Callback representation

The Go SDK normally resolves callback handles to FOM names. Applications that
already work with numeric handles can avoid that projection and receive the
wire-owned maps directly:

```go
conn, err := federate.ConnectWithOptions(ctx, address, federate.ConnectOptions{
    CallbackRepresentation: federate.CallbackRepresentationHandles,
})
```

Handle mode emits `ReflectAttributeValuesByHandle` and
`ReceiveInteractionByHandle`. Name mode remains the default and retains the
existing callback API. The maps and byte slices belong to the emitted event;
the SDK does not reuse them, so a callback consumer may retain or modify them.

## Error handling

The local mirror rejects known publication, ownership, handle, and parameter
errors before admission. An authoritative rejection after admission is
asynchronous and is associated with its exact sequence. Observe it with
`AwaitLocalLRC`, `FlushLocalLRC`, a later admission, or the resign drain. Work
admitted after the failed sequence may be indeterminate.

Protocol, cancellation, or ambiguous transport failures reset the stream.
Federation generation and membership are revalidated by the server. Older
servers fall back before mutation to a supported confirmed transport.

LocalLRC is disabled when bearer credentials, OIDC, or custom dial options are
used because authenticated stream identity and reconnect deduplication are not
yet part of this path. Standard calls then use the confirmed path, while an
explicit queue call returns `ErrLocalLRCUnavailable`.

## Verification

The two-process Go workload can record queue admission, cumulative-ACK
completion, and subscriber delivery as separate samples:

```powershell
powershell -ExecutionPolicy Bypass -File verification/gorti-go-fair/run.ps1 `
  -Fom verification/gorti/federation.fom.xml -Seed 1516 -Count 2000 `
  -ReceiveOrder -LocalLRCQueue 1024 -LocalLRCAckEvery 32 `
  -LocalLRCBatchSize 32 -CallbackRepresentation handles
```

Queue-admission latency is not comparable to another implementation's
server-confirmed call latency. Cross-implementation results must use the same
completion boundary, FOM bytes, seed, process choreography, callback and
logging conditions, warm-up count, measured-run count, and AB/BA order.
