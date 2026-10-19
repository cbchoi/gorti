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

The stream carries the session token issued at join, binding its operations to
that federate. Bearer credentials and OIDC still disable this path; custom dial
options alone do not. The Integer64 backend currently uses the confirmed route.
When LocalLRC is unavailable, standard calls use the confirmed path, while an
explicit queue call returns `ErrLocalLRCUnavailable`.

## Recovery after restore

After `FederationRestored`, the Go SDK uses confirmed calls until
`Federate.ReinitializeLocalLRC(ctx)` succeeds. Suspend new service calls during
reinitialization, but keep consuming `Events()` in another execution path.
The SDK installs the authenticated FOM, declarations, object knowledge and
ownership image only when the matching ordered callback marker arrives.

A timeout or stale marker leaves the confirmed path active. Reinitialization
does not replay pre-restore operations or reuse their public tickets for new
stream sequence numbers.

## Local callback admission

The optional Go build tag `callback_async` also supports receiver-side local
admission. Build the server and Go SDK application with this tag. The default
build retains synchronous callback reporting; Python and Java are unchanged.

After the server advertises support and accepts an authenticated report, the
SDK can enter ordinary, untimed, nonregional receive-order data callbacks
without waiting for each entry ACK. Only callbacks tracked from the server's
stream qualify. Entry and outcome reports share an ordered, bounded queue;
receiving an event into that queue does not count as invoking its callback.

Use `DispatchEvent` to execute a callback and `FlushCallbackInvocationReports`
to establish remote completion and wait for local report bookkeeping, including
work whose ACK has already arrived. Subsequent RTI service calls wait for preceding
wire ACKs. This also orders reentrant services, save/restore services and resign
after local callback state. A failed report is retained for retry without
reexecuting the application callback. A full queue applies backpressure and
honors the dispatch caller's deadline before entry is accepted.

The Go report transport can send up to four 64-report frames before waiting
for the whole window to complete. A stream-owned writer runs independently of
ACK reads; partial rejection preserves the acknowledged prefix. A separate,
session-owned worker performs local completion bookkeeping in report order.
Asynchronous work remains within a 256-slot budget until that bookkeeping
finishes, including work already acknowledged on the wire. Confirmed control
reports do not require those completion slots, allowing checkpoint service
barriers to progress while local bookkeeping waits for the checkpoint lock.

ACK and retirement signals allocate a channel only when a caller waits before
completion. Already-completed reports reuse a closed signal. This reduces
per-report allocation without changing acknowledgement or drain behavior.

After an outcome is acknowledged and its local entry bookkeeping is complete,
the SDK releases the retained application event. It keeps the invocation
identity, result and receipt needed for duplicate detection and result-only
recovery. Unentered events and unacknowledged results retain their replay or
retry state. Checkpoint serialization and the saved-state format are unchanged.

TSO, callbacks carrying region handles, ownership, checkpoint, recovery and
other control callbacks retain confirmed entry. Older servers and closed callback streams use confirmed entry
as well. `ReportCallbackEntry` itself remains an explicitly confirmed operation.
Inspect `CallbackReportTransportStats().LocalEntries` to check whether local
admission was actually used; the build tag alone is not evidence of negotiation.

Server-side MOM counters are updated when ordered entry reports arrive, not
when data is merely delivered. An SDK service call after local dispatch waits
for those reports; asynchronous observations from other federates can precede
their arrival. Use the report drain when an experiment needs a settled boundary.

Within a report frame, the production server processes a successful prefix of
ordinary RO data reports under one authenticated membership/checkpoint lease.
It updates receipt state and MOM accounting in report order without cloning
each callback payload. The lease ends before sending an ACK. Detailed MOM
service or exception reporting disables this shortcut atomically with the
accounting decision. TSO, conveyed regions, controls, recovery, failed outcomes
and custom observer compositions retain individual processing. The first
ineligible report falls back to that path; an error still acknowledges only
the successfully applied prefix. Receipt retention and eviction rules are unchanged.

The pending callback receipt budget is separate from outbox capacity. The default
ledger permits 8,192 pending invocations, including callbacks already delivered
but not yet outcome-reported. A producer can exhaust this budget even while
outbox slots become available. LocalLRC admission and send ACKs do not establish
receiver completion or release those receipts.

The production server leaves 64 slots within that limit for non-data callbacks.
New reflection/interaction fanouts must fit below the shared 8,128-slot threshold;
control callbacks may use the remaining headroom, still within the 8,192 total.
This keeps a saturated data stream from consuming all synchronization or
save/restore admission capacity. It does not provide unlimited control capacity
or discard already-pending callbacks during restore.

On the direct LocalLRC data path, a full receipt ledger or callback outbox now
pauses an uncommitted operation instead of terminating its stream. The server
reserves receipt capacity for the whole recipient fanout, acknowledges the
committed prefix and releases membership and checkpoint leases before waiting.
Receipt completion signals available space; outbox saturation uses a bounded
1 ms retry delay because its subscribers consume a raw channel. Before retrying,
the server rechecks membership and generation, and, in the authenticated server
composition, the member incarnation and callback epoch. Save/restore admission
is reacquired. A changed execution scope rejects the old operation.

Keep processing and reporting callbacks while awaiting sender completion.
Local queue acceptance still does not guarantee remote execution. Cancelling
the stream terminates its wait. An atomic fanout too large for an empty queue,
management requests, and failures after publication are not retried. Unary and
confirmed transports retain their existing error behavior. No callback receipt
checks are disabled and the pending limit remains 8,192.

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
