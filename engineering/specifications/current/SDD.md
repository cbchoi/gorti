# Software Design Description

Document version: 4.0
Status: Current
Updated: 2026-07-17

## 1. Architecture

gorti uses a standalone RTI process and separate federate processes. The
server owns authoritative federation state; SDKs translate language-specific
calls and callbacks to the common wire contract.

```text
Federate model
    -> Go, Python, or C++ SDK
    -> confirmed RPC or pipelined stream
    -> rtid transport
    -> federation service managers
    -> event log and per-federate outboxes
    -> ordered callback stream
```

The main packages are:

| Component | Responsibility |
|---|---|
| `rti/cmd/rtid` | Process lifecycle, configuration, and service composition |
| `rti/internal/federation` | Federation generation and membership |
| `rti/internal/declaration` | Publication and subscription state |
| `rti/internal/object` | Object registry, interaction validation, and fanout |
| `rti/internal/time` | Logical time, TSO queues, LBTS, requests, and grants |
| `rti/internal/ownership` | Attribute ownership state transitions |
| `rti/internal/ddm` | Dimensions, regions, and routing predicates |
| `rti/internal/eventlog` | Ordered persistence and replay evidence |
| `rti/internal/transport/grpc` | RPC handlers, streams, ACKs, and callbacks |
| `rti/pkg/federate` | Go federate client and callback dispatch |
| `pysdk` and `cppsdk` | Python and C++ public adapters |

## 2. State ownership

All mutable service state is keyed by federation name and generation. A newly
created federation receives a generation distinct from every prior instance
of that name. Requests, queued delivery, acknowledgements, and callbacks carry
or resolve against that generation so stale work cannot mutate a replacement
federation.

Each service manager serializes transitions over its own state. Cross-service
operations use an explicit order: validate membership and generation, validate
FOM/declaration state, reserve all required recipients, append durable state
where required, commit the transition, then expose the ACK or callback.

## 3. Object and interaction delivery

Once an operation reaches the server, the server-side path is:

1. Resolve the federation and verify the sender's current membership and any
   available generation evidence.
2. Validate class, attributes or parameters, publication, order, and timestamp.
3. Compute recipients from subscriptions and DDM predicates.
4. Reserve capacity for the entire recipient set.
5. Record the accepted operation and commit the reservations.
6. Return the applicable server result and deliver callbacks in deterministic
   order.

Receive-order events can be delivered immediately after commit. Timestamp-order
events enter the target federate's time queue and are released by the Time
Manager. Partial reservation, persistence, or commit failure aborts the
operation and is returned to the caller.

The optional LocalLRC path uses a bounded local queue, persistent pipelined
stream, sequence numbers, negotiated operation batches, and cumulative ACKs.
Batch size and ACK cadence are independent: batching amortizes framing while
ACKs define completion progress. Some Go untimed OM methods use this path by
default and return at local queue admission. Callers that require server
confirmation wait for the cumulative ACK or select an explicit confirmed path.
The lockstep ConfirmedObject stream is a separate protocol and does not use
LocalLRC cumulative-ACK semantics.

The callback transport opportunistically combines already-ready event batches
up to a fixed bound. It never waits to fill a larger frame, and a time-advance
grant forces an immediate boundary after all preceding eligible TSO events.
Each federate has a bounded server outbox, configured in event units; the
production default is 8,192 events with 32 events per internal batch and a
1 ms partial-batch flush bound.

## 4. Time Management

Each joined federate has current logical time, lookahead, regulation and
constraint flags, one optional pending advance request, and a TSO input queue.

For an accepted advance request the server:

1. records the request and computes the safe grant bound;
2. waits when a regulating peer, lookahead bound, or earlier TSO event blocks
   progress;
3. reserves all eligible TSO callbacks followed by one grant;
4. commits current time and clears pending state before making the grant
   visible; and
5. releases callbacks and the grant in that order.

Holding the state transition across the visibility boundary prevents a client
that immediately submits its next request from observing the previous pending
state. Delivery or reservation failure withholds the grant.

## 5. Federation lifecycle and teardown

Join installs all per-federate state before callbacks can be emitted. Resign
performs the selected ownership and object cleanup, removes declarations and
time state, and then removes membership. Destroy requires an empty federation,
closes optional plugin and transport resources, clears all manager state for the
generation, and only then allows the name to be reused.

## 6. Persistence and replay

The default composition root loads no audit provider and passes a nil event-log
dependency to service managers. Guarded producers therefore skip record
construction, encoding, and storage on the HLA core path.

The optional `auditreplay` plugin implements the `runtimeplugin` contract. It
records structural format 2 logs with a fixed 64-byte, little-endian header and
federation-generation identity. Readers reject truncated records. Its
service-manager hook absorbs recording failures so observation cannot change
HLA results; its explicit admin and replay surfaces return storage errors.
Replay remains bounded to the record types currently emitted.

Save bundles support legacy manifest version 1 for compatibility and manifest
version 2 as the current generation-aware format. Version 2 records FOM
SHA-256 provenance and structured manager snapshots in addition to an optional
event-log extent. The current bundle may record a zero-length extent. Restore
validates the bundle version, generation rules, and FOM identity before
rebuilding supported manager state. Unknown or incomplete bundles fail without
altering a running federation.

## 7. Observability

Structured logs use stable service and operation names. Performance
instrumentation distinguishes:

- caller-to-confirmed-ACK latency;
- server admission and service time;
- callback and completed-delivery latency; and
- TAR submission to exact TAG latency.

Measurement instrumentation is outside the semantic payload and is applied
equivalently to compared implementations.

## 8. Extension rules

New transports and research alternatives implement the existing contracts and
remain opt-in until they pass the current [STD](STD.md). An optimization is not
accepted when it changes callback order, error visibility, durability,
generation fencing, backpressure, or the measured completion boundary.
