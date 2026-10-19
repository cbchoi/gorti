# Bounded LocalLRC callback backpressure

The subsequent [completed-receipt retention change](completed-receipt-retention-20261006.md)
measures and reduces the ledger-copying cost identified below. This report retains
the backpressure increment's original binaries and results.

The current LocalLRC path now completes the historical 81,200-callback workload
without raising the receipt limit or disabling callback evidence. This restores
the ability to run the large experiment, not the old implementation's throughput.
The preserved release remains substantially faster in this workload. The small
workload shows no statistically resolved performance change from the immediately
preceding IEEE-tested implementation.

## Plan

Keep the current callback entry, outcome, replay and restore behavior. Replace
terminal capacity errors with producer backpressure only when the domain service
certifies that a direct data operation has not appended its journal record or
published any callback. Keep bounded queues, atomic fanout and cumulative ACKs.

The preceding experiment is documented in
[the preserved-reference report](legacy-performance-reference-20261006.md).
Its 81,200-callback runs failed at the default 8,192 pending receipt limit.
Receipts remain pending after delivery until callback outcomes are reported;
outbox capacity and receipt capacity are therefore separate limits.

## Implementation

- Mandatory callback admission reserves credits for the entire recipient fanout
  before preparing individual receipts. Other admissions account for these
  reserved credits. Rollback releases all receipts and unused credits.
- Temporary receipt pressure does not mark MOM callback coverage unavailable.
  Completion, rollback and lifecycle cleanup notify capacity waiters.
- Object wire operations identify reservation failures before publication with
  `core.UncommittedWireError`. Internal outbound sequence ranges may have gaps
  after failed reservations; delivered callbacks remain ordered.
- LocalLRC preserves the committed prefix, sends its ACK outside service leases,
  and retries only the next unpublished operation. It does not replay a frame.
- All membership and checkpoint leases are released before capacity waits and
  ACK sends. Resume reacquires admission and checks federation generation and
  membership. Production composition also compares authenticated incarnation and
  callback epoch using its existing transportation scope provider.
- Temporary outbox saturation is retryable only when the complete fanout fits an
  empty queue. Its raw receive channel has no consumption notification, so the
  pressure path uses a 1 ms retry timer. Receipt pressure uses notifications.
  A 100 ms lifecycle check detects a producer leaving without freeing receipts.
- MOM records one completed service invocation, not each transient retry.
  Management operations, oversized fanouts, arbitrary errors and errors after
  publication are not retried. Stream cancellation ends the capacity wait.

The wire schema, persisted formats, SDK API and default synchronous reporting
profile are unchanged. Unary and confirmed producer paths retain their existing
capacity error behavior. This change applies to the supported direct LocalLRC
data path; it does not add LocalLRC to the Integer64 confirmed-only backend.

## Review

The initial deterministic regression failed because a full fanout validated two
recipients, briefly prepared one receipt and marked callback coverage unavailable.
It passes after whole-fanout credit reservation. Added tests cover capacity wakeup,
credit exclusion, impossible fanout, single and batched operation order, duplicate
prevention, one-time MOM observation, cancellation, generation change, checkpoint
admission and restore epoch change. Targeted tests passed repeated runs.

The first large-load candidate solved receipt pressure but failed at outbox
saturation. That failed trial is retained, not replaced or included as a successful
measurement. Extending the unpublished-operation wait to the bounded outbox fixed
the second limit. No queue or receipt capacity was increased.

Fresh Go-based IEEE service regression passed for both build profiles:

| Profile | Required inventory | Top-level passes | Failures | Skips |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,906 | 0 | 10 async-only cases |
| `callback_async` | 978/978 | 2,916 | 0 | 0 |

The campaign used `-count=1`, the live runtime-checkpoint build tag, the final
server binaries and the official retained-FDD XSD. It includes current wire
goldens, Float64/Integer64 services, live restart and callback outcome recovery,
and the nine added top-level pressure tests. There are no missing or skipped
required inventory cases. This is a pass of the prepared internal tests, not a
new claim of full IEEE conformance or independent certification. The complete
Python and Java SDK suites were not rerun in this increment.

Race verification was unavailable: Docker's Linux-engine pipe was absent and
neither GCC nor Clang was available on the native PATH. The repeated concurrency
contract tests do not substitute for a race-detector run.

## Experiments

Both campaigns use the existing DEVStone-HLA HI 10 by 10 plan, identical FOM bytes
and per-pair seeds, two independent federate processes, LocalLRC batch size 32,
numeric-handle callbacks, compact summaries, fresh processes and `GOMAXPROCS=2`.
Each has five warm-up pairs and twenty measured AB/BA pairs. The candidate uses
`callback_async`; callback reporting remains enabled. Each pair must match callback
counts and ordered payload hashes, and complete the final report drain.

Timing ends at the final subsequently validated callback's dequeue, before its
dispatch. Final report drain is required outside that timer. The old reference
does not implement today's callback-recovery reporting, so it is not an
equal-conformance replacement or evidence that the old release passes today's tests.
No Portico run is part of these campaigns.

### Large workload against the preserved release

The workload has 100 external events and 81,200 measured callbacks. All 50 trials
completed, including warm-up, and all 25 semantic pairs matched.

| Metric | Preserved release | Current backpressure build |
| --- | ---: | ---: |
| Measured trials | 20 | 20 |
| Mean completion, seconds | 0.227354 | 5.068582 |
| Median completion, seconds | 0.225884 | 4.445263 |
| Standard deviation, seconds | 0.010494 | 1.452572 |
| Mean per-trial callback throughput, callbacks/s | 357,849.9 | 16,896.3 |

The mean-time speedup (reference/current) is 0.044856, with paired
order-stratified bootstrap 95% interval [0.039837, 0.050031]. The current build was
slower in every measured pair. Its median duration is about 19.7 times the
reference median. Slow trials, including a 9.761-second current run, remain in
the results; their cause has not been isolated.

### Small workload against the preceding current build

This control comparison uses five external events and 4,060 measured callbacks.
Both conditions use the current SDK's callback evidence and asynchronous profile.
All 50 trials completed and all 25 semantic pairs matched.

| Metric | Preceding FIFO build | Backpressure build |
| --- | ---: | ---: |
| Mean completion, ms | 150.672 | 150.232 |
| Median completion, ms | 149.619 | 149.594 |
| Standard deviation, ms | 7.379 | 5.421 |

Mean-time speedup is 1.002932, with 95% interval [0.983172, 1.022275]. Nine of
twenty measured pairs favored the candidate. This does not demonstrate a speed
improvement or a significant regression under these conditions.

## Reflect

Retain bounded backpressure for complete, lossless execution at sustained load.
Do not describe it as recovery of the old performance advantage. The prior
implementation could not complete this large workload, so it has no valid large
workload timing denominator for a before/after speedup.

The next performance investigation should isolate the current server's receipt
completion bookkeeping and SDK report processing from transport waits. In
particular, `trimCompletedLocked` still shifts completed receipt identifiers when
retiring the oldest entry from a full ledger. That is a code-level candidate, not
a measured attribution of the observed gap. Any change must retain duplicate
report handling, recovery-marker retention, callback order and checkpoint replay.

## Evidence

Raw logs, binaries, plans and result JSON remain outside Git, under
`../benchmark-results/`:

| Directory | Evidence |
| --- | --- |
| `legacy-pressure-long-screen-20261006` | First candidate's retained outbox-overflow failure |
| `legacy-pressure-v2-long-screen-20261006` | Successful single-run large-load investigation |
| `legacy-pressure-long-paired-20261006` | Final large-load paired campaign |
| `pressure-short-paired-20261006` | Final preceding-current versus candidate campaign |
| `callback-pressure-final-builds-20261006` | Final binaries and source archive |
| `pressure-ieee-qc-20261006` | Current two-profile IEEE service regression |

The candidate source archive SHA256 is
`87179278d73400352f007b4d82e1f22e900d5983cce1395d3cb89b8609fcbac5`.
The async server SHA256 is
`ba58c1b416e507274fa53d7858413970976f3e86e90fe43b16ebfd8ea24a4b73`;
the sync server SHA256 is
`2d4984ba7db656a73e727f03aee78715cf05cd0da9465cd03953b7beabc8c5c9`.
Both campaign manifests record executable hashes, workload hashes and conditions.
