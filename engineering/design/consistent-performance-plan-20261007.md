# Consistent-performance improvement plan

Status: M1's comparison-only copy removal is implemented and measured. M2's
homogeneous RO-frame batching and error corrections are implemented; its
same-seed crossover shows a modest throughput gain, but cross-session consistency
is still open. M0a's order/seed crossover is complete. The rest of M0 and M1,
mixed-prefix batching, M3 dynamic routing, M4's continuous window and M5 remain
open. M4's lazy completion signals are implemented and measured on 2026-10-08.
M3 static FOM
metadata indexes are implemented and measured. The original review below used diagnostic
executables and a CPU-setting experiment separate from the Portico campaign.

The [first M1 increment](../verification/callback-payload-view-20261007.md)
preserves full payload comparison while removing its temporary deep clone.
A fresh 20-pair campaign reduced mean completion by 7.47% and increased aggregate
throughput by 8.08%; both builds passed all 978 required regression tests. Native
race validation remains unavailable without a C compiler. This does not complete
the full immutable-body milestone or establish a new Portico ranking.

The [M2 increment](../verification/callback-delivery-batch-20261007.md) reduces
per-batch allocation and admission overhead and fixes lost completed receipts
after late transport errors. Both final builds pass 978/978 required tests.
Its original final paired campaign increased throughput 2.13%, but the 95%
speedup interval includes one and the two order strata differ markedly. That
result prompted M0's order/seed/process-lifetime investigation below.

The subsequent [M0a crossover study](../verification/callback-crossover-20261007.md)
runs both AB and BA for every seed with a fixed A/A control first. All 160 trials
passed. M1 versus final M2 throughput was 58,446 -> 60,854 callbacks/s (+4.12%),
with a whole-seed-block 95% speedup interval [1.022449, 1.059576]. Both order
strata favor M2; the large prior sign reversal did not recur. The A/A apparent
gain was 1.87%, with an interval containing one. This supports a modest gain in
the new campaign, not cross-session consistency or a new Portico ranking.

The [M3 static-index increment](../verification/fom-static-metadata-20261007.md)
preserves immutable FOM view/handle semantics and passes 978/978 required cases
in both profiles. Its fixed crossover gives 60,508 -> 62,882 callbacks/s (+3.92%)
with speedup CI [1.027088, 1.051105]. However, AB is nearly tied and BA favors the
candidate; the A/A control also shows a position effect. Treat this as a modest
session-specific gain, not consistent dominance. Dynamic routing is unchanged.

The [2026-10-07 Portico crossover](../verification/portico-fom-static-metadata-20261007.md)
uses that unchanged gorti binary and accepts all 100 trials. Portico's mean was
0.919762 seconds and 88,284 callbacks/s; gorti's was 1.162298 seconds and 69,862
callbacks/s. Portico leads 17/20 seed-block means and 35/40 within-order pairs.
Five long Portico observations widen the gorti/Portico mean-duration interval
to [0.956849, 1.720296]. The campaign does not establish consistent gorti
superiority; M0 runtime/timing decomposition and session replication remain needed.

The [2026-10-08 Portico crossover after lazy report signals](../verification/portico-report-signal-20261008.md)
accepts all 100 trials and reverses that observed ranking: gorti/Portico mean
completion is 1.413107/2.271821 seconds and aggregate throughput is 57,462/35,742
callbacks/s. The gorti/Portico throughput ratio is 1.607678, CI
[1.319672, 1.924452], with gorti ahead in 17/20 block means and 26/40 matched
pairs. Portico artifacts are unchanged and both implementations show within-seed
variation. This is not the isolated SDK effect (measured separately as +4.88%)
or evidence of a session-independent lead. M0 long-lived-participant and
independent-session work therefore remains open.

## Reassessment after the current Portico comparison

The 2026-10-07 reassessment read the then-current implementation and reused the
forty measured gorti trials in the latest Portico campaign. No new timing
experiment or production change was made during that reassessment. The original
profiles below precede M1, M2 and static M3; they
identify historical costs, not the current distribution of CPU or lock delay.

The observed mean gap requires about 20.87% less gorti completion time to match
Portico's current mean. Matching the median requires about 41.43% less time
(1.148051 -> 0.672423 seconds). These are descriptive gap sizes, not attainable
optimization forecasts. The mean-ratio interval includes one; neither general
superiority nor equivalence follows from this campaign.

Reaggregating the saved transport counters, including in-trial warm-up and
control traffic, gives:

| Measured-trial counter | Total over 40 trials | Mean entries per frame |
| --- | ---: | ---: |
| LocalLRC sent operations / operation frames | 3,258,240 / 111,569 | 29.20 of 32 |
| Callback reports / acknowledged report batches | 6,517,360 / 234,030 | 27.85 of 64 |

Thus publisher frames are about 91.3% full, while report batches are about 43.5%
full. The SDK counts report batches on ACK receipt; in these successful streams
each ACK covers one submitted frame. These are not timed-region-only counters
and do not reveal in-flight occupancy, queue waiting or network saturation. Low
fill alone is not a defect, and delaying callbacks to fill a batch can hurt latency.
Nevertheless, the counters support examining reporting before merely increasing
publisher batch size.

### Revised implementation order

| Priority | Remaining change | Source evidence at the 2026-10-07 review | Required behavior to retain |
| --- | --- | --- | --- |
| 1 | M4: continuous bounded report progress and cheaper task completion | `callback_report_transport.go:308` waits for a whole snapshot's exchange before releasing its tasks; each task has two channels at line 148. The writer already pipelines frames. | Exact accepted/error prefix, service-call report watermark, session fencing, reconnect, and capacity bounded through local retirement |
| 2 | M1 remainder: one owned immutable callback body, separate mutable envelopes | `mom_receipts.go:157`, `callback_required.go:118` and SDK `runtime_checkpoint.go:143` still deep-copy retained or exposed wire events. | Caller mutation isolation, complete payload/unknown-field checks, recovery identity and ownership-attempt authority |
| 3 | M4 companion: index pending SDK callbacks without changing arrival order | `runtime_checkpoint.go:133` scans pending identities at each arrival; `runtimeEventConsumedLocked` scans runtime IDs. | Duplicate suppression, ordered checkpoint images, out-of-order removal, epoch reset and restore reconstruction |
| 4 | M3 remainder: reuse unchanged nonregional routing and scope | `ObjectDeliverySnapshots` and `prepareAttributeScopeLockedOf` still rebuild per-update maps/snapshots. | Subscription, known-class, object, ownership, region, FOM and restore invalidation; unchanged DDM fallback |
| 5 | M5: amortize service work within an admitted LocalLRC frame | `applyLocalLRCFrame` shares a lease but still invokes each operation separately. | Per-operation order/error prefix, atomic all-recipient admission, and no coalescing of distinct callbacks |

These priorities are engineering hypotheses based on remaining work, not a new
profile-based attribution of the completion gap. Refresh server and subscriber
profiles separately from timed comparisons; if reporting neither consumes much
CPU nor causes meaningful backpressure, move it below the measured dominant cost.
M0 timing decomposition should accompany that diagnosis without delaying core
research behind unrelated smoke, staging or freeze-state checks.

For priority 1, make allocation reduction and continuous-window scheduling
separate measurable increments. The completion worker is already session-owned;
do not describe this as replacing a goroutine per completed callback. The wire
runner still exits on an empty queue and restarts on new work. Prefer bounded
sequence-based waiters and reusable queue storage over indiscriminate pooling;
do not reuse a task while a caller, completion worker or checkpoint drain can
still reference it. Return wire credit as a valid ACK prefix arrives, but return
local-completion credit only after the associated bookkeeping finishes. Keep
control/barrier progress independent of checkpoint locks and batch-filling waits.

For priority 3, add identity-to-ID and ID-membership indexes alongside the ordered
queue, rather than replacing checkpoint order with map iteration. Ordinary FIFO
removal already avoids shifting the entire queue in `runtime_callback_queue.go`;
that optimization is complete. Measure backlog distributions before claiming
that the remaining linear searches dominate. Admission, failure, recovery and
restore must update the queue and indexes together.

The next diagnostic should attribute producer queue/capacity wait, server service
and callback preparation, subscriber decode/dispatch, report ACK wait, local
retirement and final drain separately. Record allocation bytes per delivered
callback, report/frame sizes and outstanding-work high-water marks. Preserve the
current primary timer and report the additional boundaries separately. Use
same-process monotonic durations, not subtraction of unsynchronized clocks.

Do not remove the two logical facts of invocation entry and outcome merely to
reduce report count. A negotiated compact protocol may eventually represent them
more efficiently, but must preserve a service invoked from inside a callback and
failed or not-yet-reported outcomes. It is a later protocol project, not equivalent
to switching off confirmation or deferring all recovery state until save time.

## Objective and baseline

The original target rationale used the
[earlier paired comparison](../verification/portico-copy-lookup-20261006.md), which
gave gorti a 1.363763-second mean and 59,541 received callbacks/second. Portico's
overall mean was slower, but its five fast observations averaged 0.720361 seconds
against 1.358741 seconds for their paired gorti trials. Those paired observations
leave about a 1.89x completion gap. Portico's preceding campaign averaged 0.681147
seconds without changing its implementation artifacts.

Beating a slow Portico campaign mean alone is not a useful optimization target.
That historical fast-group gap was about 47%; it must not replace the latest
all-trial comparison or justify selecting observations. A provisional engineering
target for this particular 81,200-callback N=2
RO workload is a mean at or below 0.60 seconds and p95 at or below 0.65 seconds,
approximately 135,333 aggregate callbacks/second at the mean target. These are
targets, not forecasts or measured results. They do not promise superiority for
every workload, payload, federation size or network topology.

Do not add projected milestone percentages together. Removing one cost changes
the bottleneck, and current evidence does not prove that the existing topology
can reach this target through small changes alone.

## Original diagnostic evidence (before M1-M3)

Two diagnostic repetitions used the then-current sources, an external Go overlay,
the normal Go participants, `callback_async`, LocalLRC batch size 32, runtime-
default CPU settings and 1,000 DEVStone-HLA external events. Each repetition
delivered 812,000 measured callbacks and passed the normal semantic checks.
CPU, allocation, mutex and block profiles cover eight seconds after a three-
second delay. The runner labels the identical builds baseline/candidate; there
is no implementation change between these two diagnostic repetitions.

In the first server capture:

| Profile observation | Value | Interpretation |
| --- | ---: | --- |
| `proto.Clone`, cumulative CPU samples | 9.25% | Payload copying remains after the last duplicate-copy fix |
| `proto.Equal`, cumulative CPU samples | 5.52% | Repeated envelope comparison is still visible |
| `CallbackReceipts.preparedDelivery`, cumulative CPU samples | 8.21% | Includes work inside the receipt critical section |
| Mutex delay attributed to `preparedDelivery` | 27.59% | A contention location, not a percentage of wall time |
| Mutex delay attributed to `tryDataReportPrefix` | 14.04% | Report-side and delivery-side ledger work contend |
| `ObjectDeliverySnapshots`, flat sampled allocation bytes | 4.02% | DDM snapshots are allocated on the update path |
| `prepareAttributeScopeLockedOf`, flat sampled allocation bytes | 3.20% | Desired scope maps are rebuilt for updates |
| `FOM.AdvisorySwitches`, flat sampled allocation bytes | 2.53% | Static defaults are copied repeatedly |

These profile rows overlap and must not be summed into a speedup prediction.
The server also showed roughly 41% of sampled CPU in `runtime.runqgrab`/yield
in both captures. Profiling and the larger workload can affect this behavior.
It is a reason to test scheduling, not proof that 41% of uninstrumented request
time can be removed. Diagnostic trial times of 34.43/37.80 seconds are not used
as production benchmark estimates or compared to the shorter 100-event trials.

### CPU-setting check

An uninstrumented exploratory run used the exact candidate production binaries,
the 100-event workload, two warm-up blocks and six measured blocks. Each block
ran default, `GOMAXPROCS=2` and `GOMAXPROCS=4` settings with the same seed. The six
measured blocks cover all six execution permutations. Each setting affected
the server and both Go federates, not only the server. All 24 trials passed and
all eight three-way semantic checks matched.

| Setting | Mean seconds | Median seconds | Aggregate callbacks/second |
| --- | ---: | ---: | ---: |
| Runtime default | 1.484341 | 1.423627 | 54,704 |
| `GOMAXPROCS=2` | 1.467287 | 1.423724 | 55,340 |
| `GOMAXPROCS=4` | 1.476096 | 1.435150 | 55,010 |

Two processors beat default in four of six blocks; four processors also did so
in four of six. Mean differences are about 1.2% or less, with no convincing
consistency in this small sample. Do not hardcode a low processor count, claim
a twofold gain from runtime tuning, or infer a code regression from differences
between this pilot and earlier campaigns. Broader server-only tuning remains
possible, but it is not the main recommendation.

## Milestones

### M0: Separate control, delivery and settlement timing

M0a progress: same-seed randomized ABBA/BAAB scheduling, whole-block bootstrap
analysis and a fixed A/A plus A/B campaign are complete. Benchmark instructions
describe the new crossover option. The timestamp decomposition and long-lived
participant work below remain unimplemented; no causal explanation of Portico's
variation has been established.

Keep the existing primary metric; add timestamps on both implementations for
subscriber arming, START completion, first data callback and final data callback.
Record post-delivery completion/ACK drain separately. Use same-process monotonic
durations; do not subtract clocks from different hosts without synchronization.

Add repeated batches in long-lived federations alongside fresh-process trials.
Warm the actual process used for measurement, without discarding unfavorable
measured batches. A protocol-level readiness barrier is preferable to fixed
sleeps. Preserve identical workload plans, per-channel order, payload checks and
logging conditions. For diagnostic-only Java runs, capture JVM/GC/JFR information
separately from primary uninstrumented measurements.

This can explain Portico's variation; it does not make gorti faster. It can run
alongside M1 and must not become an unrelated staging or freeze-state gate.

### M1: Reduce callback envelope copies without weakening validation

Progress: comparison-only deep clone removed and verified on 2026-10-07. The
immutable retained-body portion below is still a proposal.

Primary locations:

- `rti/internal/adapters/grpc/mom_receipts.go`: `prepareWithCredits`.
- `rti/internal/adapters/grpc/callback_required.go`: `requiredCallbackEvent.Inner`
  and `preparedDelivery`.
- `rti/internal/adapters/grpc/stream_ownership_receipts.go`: batch ownership wrapper.

At the plan's baseline, the reservation ledger retains a clone, dispatch obtains a separate
envelope, the ownership wrapper clones the batch, and prepared delivery clones
again to normalize receipt fields before equality checking. The preceding
optimization removed one redundant clone, not this complete chain.

The implemented first step removes the comparison-only deep clone through a
read-only comparison of all relevant fields with explicitly normalized authority.
It preserves Protobuf unknown fields, nil/empty semantics, complete payload
mismatch detection and ownership delivery-attempt rules. It does not temporarily
mutate shared events or copy generated message lock state.

Then define a private immutable callback body with separately owned mutable
receipt/recovery/transport headers. Materialize and validate it once at admitted
publication, while retaining isolation at caller and wire boundaries. Pointer
identity can substitute for a repeated deep comparison only when the internal
provenance and immutability are established. Untrusted/deserialized or restored
inputs retain complete checks. Guarded/advisory callbacks must still resolve
their current delivery validity immediately before transport.

Acceptance: mutation after admission cannot change ledger or another recipient;
forged bodies, stale identities, repeated restore, ownership retry attempts,
unknown fields and failed transport behave exactly as before. Measure allocations
per delivered callback and the end-to-end paired result, not only the microbench.

### M2: Batch delivery-ledger preparation and completion

Progress: homogeneous 2-64-event ordinary RO frames now share preparation and
successful-send accounting. Mixed/control/TSO/region/recovery frames and failure
paths retain individual processing. Completed reports survive late Send or
accounting-admission errors. Allocation reduction and full prepared regression
are verified, but the final throughput result is inconclusive. The original
proposal below still describes the broader mixed-prefix extension.

Update after M0a: a predeclared crossover campaign using the same final binaries
found +4.12% throughput with a speedup interval excluding one. The earlier
inconclusive result is retained. Cross-session consistency and tail acceptance
are still open; this is not a new implementation increment.

`receiptBatchesStream.Send` already sends a batch but calls `preparedDelivery`
once per event and invokes a completion closure once per event afterward.
`CallbackReceipts.commit` creates a timeout context and reacquires admission for
each callback. The report-receive path was batched in the previous increment;
the corresponding delivery-side processing remains a separate opportunity.

Introduce a bounded batch token for eligible same-scope RO callbacks. Validate
and prepare a prefix with one ledger critical section, perform network Send with
no RTI/ledger locks held, then record the send result under one scoped admission
and one ledger critical section. Retain individual receipt identities and ordered
state transitions; batching is an implementation detail, not callback merging.

Split at control, restore-handle changes, ownership, TSO or other ineligible
events and retain the existing path. Preserve failure precedence, early entry/
outcome reports, failed-send replay, source invalidation, exact checkpoint
publication order and transactional all-recipient admission. A frame is not
permission to lose the valid prefix of a failed operation sequence.

Acceptance: successful/failed/ambiguous Send, early reports, reconnect, resign,
save/restore and cancellation interleavings; no scope held during Send/Recv;
identical pending counters and replay evidence. M2 follows M1 to avoid concurrent
changes to the same receipt ownership and lifetime rules.

### M3: Precompute stable FOM metadata; avoid unchanged routing snapshots

Progress: static class-name indexes, ancestry tables and advisory defaults are
implemented for initial load, live extension and snapshot restore. Caller-owned
ancestry results, ambiguous names, stable handles and old immutable views are
preserved. New differential/lifecycle/allocation tests and both full prepared
regressions pass. The crossover mean favors the change, while execution-position
effects and native race coverage remain unresolved. The dynamic routing cache
below is still a proposal.

At the original baseline, `fom_advisories.go` walked parent-name lookups and
constructed a visited map for each ancestry query; `AdvisorySwitchDefault`
obtained a copied defaults list. The implemented static step precomputes ancestry,
class-name indexes and advisory defaults per immutable `fomBindings` view, and
publishes them through the existing FOM revision/extension transaction. It keeps
stable handles on extension and restore, ambiguous-name behavior and caller-owned
return values. Returning a shared mutable slice would break that contract.

For `ObjectDeliverySnapshots` and `prepareAttributeScopeLockedOf`, add a no-change
path for provably unchanged subscription/routing state. A cache keyed only by
class name or FOM revision is insufficient. Relevant changes include declarations,
known-class/rediscovery state, region association and committed bounds, object
lifetime, ownership and restore. Reuse immutable routing plans under revision
checks; rebuild when any relevant version changes. Start with the demonstrably
nonregional path, retaining full DDM processing elsewhere.

Acceptance: dynamic FOM join/rollback, ancestor subscriptions, defensive slices,
rediscovery, region commit/delete/reassociation, default-region overlap, ownership
changes and restore invalidate the right state. Static FOM indexing is lower risk
than dynamic DDM caching and should be a separate measurable substep.

### M4: Continuous bounded reporting pipeline in LocalLRC

Progress on 2026-10-08: the first allocation-focused increment is implemented.
The continuous sliding window and persistent wire runner below are not yet
implemented. No server, wire schema, public API, callback report count or queue
limit changed in this increment.

#### First increment: lazy ACK and retirement signals

Two fresh diagnostic repetitions used the current pre-change server and Go SDK,
the existing external profiling overlay, and 812,000 measured callbacks per trial.
Both passed semantic checks. In the subscriber's eight-second allocation profiles,
`enqueueReserved` accounted for 13.52% and 14.01% of sampled allocation bytes.
These are sampled allocation shares, not percentages of completion time. The
diagnostic runs used identical binaries; their differing durations are not an
optimization result.

`callback_report_signal.go` now provides a private one-shot signal. It creates a
channel only when somebody actually waits before completion. Without an early
waiter, an atomic terminal pointer publishes completion and preceding state;
late waiters use a shared closed channel. ACK and local retirement remain
independent signals. Tasks are never reset or pooled while references exist.

The transport publishes each task's result and ACK before exposing that task to
the completion worker. This avoids allocating ACK waiters inside that worker.
Service barriers still wait for ACK, while explicit drain waits for local
bookkeeping. Existing wait sites and tests use the new private signal without
changing their assertions about errors, ordering, completion or backpressure.

The allocation regression test failed on the original implementation with three
allocations per enqueue, then passed with one. The isolated enqueue benchmark
uses a preallocated queue and request; it does not represent the entire report
or callback lifecycle:

| Enqueue metric | Before | After |
| --- | ---: | ---: |
| Allocations per report task | 3 | 1 |
| Allocated bytes per report task | 288 | 64 |

The 77.78% byte reduction applies to this enqueue path only, not total SDK memory,
resident memory or overall per-callback allocation. Four new tests cover the
allocation budget, early/late waiters, concurrent completion publication and
allocation-free completion when no early waiter exists. Existing report, ordering,
failure and drain tests also passed five repeated runs.

Fresh prepared IEEE-service/SDK regression results:

- `callback_sync`: 978/978 required cases; 2,956 top-level passes, no failures,
  16 async-only skips.
- `callback_async`: 978/978 required cases; 2,972 top-level passes, no failures
  or skips.
- Both include the live checkpoint/control-outcome restore tests selected by
  the existing quality runner. Standalone Python and Java campaigns were not
  rerun for this Go-SDK-only change.
- Native Go race testing was attempted but could not build: GCC is absent from
  PATH. Race coverage remains unverified; the ordinary runs are not a substitute.
- Final `go vet -tags callback_async ./rti/pkg/federate` passed after replacing
  two newer test-helper APIs with equivalents compatible with the module's Go
  version. The focused report tests passed another five repeats. These test-only
  corrections did not alter the production binaries used for regression or timing.

The uninstrumented DEVStone-HLA HI N=2 comparison kept the existing primary timer,
81,200 measured callbacks, identical FOM/seed plans, LocalLRC batch 32 and two
independent Go federates. A fresh RTI server was started per trial; baseline and
candidate server binaries have identical SHA-256 hashes. Five warm-up and twenty
measured four-trial blocks used balanced randomized ABBA/BAAB order (seed 20261008).
All 100 trials passed with matching within-block semantic evidence. No trial was
retried, removed or replaced; tests and profiling did not overlap measurement.

| Measured result | Before | After |
| --- | ---: | ---: |
| Trials | 40 | 40 |
| Mean completion, seconds | 1.530478 | 1.459250 |
| Median completion, seconds | 1.516260 | 1.442826 |
| Aggregate received callbacks/second | 53,055 | 55,645 |

Throughput increased **4.88%**, and mean completion decreased **4.65%**. The
whole-seed-block stratified bootstrap 95% speedup interval is
**[1.013765, 1.088442]**. The candidate was faster in 14/20 seed-block means.
Both order strata favor it, but by different amounts: AB +1.78%, BA +8.07%.
Each build still reported 6,517,360 entries/outcomes/control reports over its
forty measured trials, including their operation warm-up and controls; reporting
was not disabled. There were no LocalLRC failures.

This is a single-session before/after benefit, not a new Portico comparison or
proof of consistent superiority. Host variation is visible in both builds. The
baseline binaries reproduce the preceding campaign's hashes, but its absolute
throughput must not be compared directly with this session's lower rates to infer
a source regression. An additional A/A or independent-session campaign was not
run in this increment.

Artifacts remain outside Git under `D:/workspace/gorti/benchmark-results/`:

- `report-progress-profile-20261008/`: pre-change diagnostic binaries and profiles.
- `report-signal-baseline-builds-20261008/` and
  `report-signal-candidate-builds-20261008/`: uninstrumented sync/async binaries.
- `report-signal-ieee-qc-20261008/`: regression JSON, live evidence and enqueue
  benchmark output.
- `report-signal-crossover-20261008/`: manifest with build hashes, pre-execution
  schedule, all trial evidence, observations and bootstrap comparison.

#### Remaining continuous-window work

`callbackReportTransport.run` snapshots up to 256 queued tasks and waits for the
entire submitted window's ACKs before submitting the next snapshot. The writer
already pipelines 64-report frames inside a window; a proposal to merely add
asynchronous writes would duplicate existing work. The runner also exits on an
empty queue and is restarted when new reports arrive. The previous unconditional
two-channel allocation per task has been removed by the first increment above.

Investigate one persistent session worker and a credit-bounded sliding window:
as a cumulative ACK retires a prefix, available capacity can admit subsequent
frames without waiting for the remainder of the old window. Keep total unfinished
work bounded through local completion, not only wire ACK. Reduce transient task,
slice and wakeup-channel allocations where ownership permits reuse.

Keep exact failed-prefix handling, per-task outcomes, canceled-caller behavior,
stream fallback/reconnect and session fencing. Service calls must still wait for
their preceding report watermark; local completion/checkpoint drain remains a
separate boundary. Callback reentry cannot be reordered behind its service call.
Do not delay control barriers merely to fill a batch or introduce fixed sleeps.

Acceptance: partial ACK, stale/duplicate ACK, interceptor failure, stream EOF,
reconnect, cancellation, shutdown, queue saturation, reentrant service calls and
save/restore with ACKed-but-not-locally-retired outcomes. Check goroutine count,
bounded memory, median completion and tail behavior.

### M5: Frame-level object-service work and scalability

`applyLocalLRCFrame` already amortizes membership fencing, but still applies
each operation through its individual object update/interaction path. After
M1-M4, reprofile before attempting a domain batch interface. Reuse validated
class/routing snapshots within an admitted frame and reserve callbacks in bounded
groups while retaining the exact operation order and failure prefix.

Do not collapse multiple attribute updates into the final value, merge logically
distinct interactions, change reliable delivery or let one failed recipient
produce partial fanout. Test N=2, 4, 8 and 16, mixed publishers, different payload
sizes and a separate TM workload. Ledger sharding by federation/recipient may
help larger federations but must not be assumed to improve the single-subscriber
case. Global capacity, ordering, checkpoint snapshots and teardown need an explicit
cross-shard protocol before changing the lock structure.

A peer-to-peer LocalLRC data plane or compact range-report protocol is a later
architectural experiment, not a shortcut in these milestones. It adds routing
revision, authentication, backpressure, failure, TM and restore obligations. Keep
the centralized implementation until evidence shows the narrower changes cannot
meet the scoped target.

## Quality and adoption criteria

For each milestone, use plan, implement, review and reflect with saved evidence:

1. Establish the behavioral tests and an unchanged executable baseline first.
2. Implement one isolated concern; keep mixed/unsupported cases on the original path.
3. Run the new negative/interleaving tests and both prepared IEEE-service profiles:
   all 978 currently required cases, plus new cases and the surrounding SDK/service
   suite. Preserve Float64/Integer64, TSO/TAR/FQR/retract, ownership, DDM, MOM and
   recovery behavior. Wire descriptors and persistence fixtures must not drift.
4. Run live restore/reconnect and race tests on a suitable toolchain. The current
   Windows host lacks the native C compiler for Go race builds; use an available
   supported runner rather than claiming the ordinary test run provides race evidence.
5. Measure the previous/candidate binaries using identical FOM/seed/choreography.
   For the before/after fresh-process workload, use five warm-up and 20 measured
   four-trial seed blocks with balanced randomized ABBA/BAAB sequences. Run a
   fixed-size same-binary A/A control first; predeclare both sample sizes. Resample
   whole seed blocks for uncertainty, not their individual repeated trials. Keep
   tests/profiling off the timing host during measurement. Preserve all observations
   and require matching payload, order, callback-count and completion/drain evidence.
6. Adopt a change for repeatable benefit without new tail-latency, memory or
   correctness regressions. Reprofile and revise the next priority. Record a null
   or adverse result instead of stacking an unproven optimization into the next build.

For the final cross-RTI claim, declare the target workload matrix first, report
cold/fresh-process and warmed steady-state results separately, and repeat campaigns
in at least three independent sessions. Compare receiver completion and a
comparable application-settled boundary; do not mix local admission and remote
completion API times. Estimate p95/p99 only with adequate samples; 20 trial means
do not establish a robust p99 message-latency claim. Use confidence intervals
that respect paired execution and observed temporal clustering, with a held-out
confirmation run after tuning. Report cells that do not favor gorti.

No finite campaign proves superiority in every circumstance. A defensible claim
is repeatable superiority on the declared matrix, with no failed semantic cases,
not a universal claim about Go, Java or all IEEE 1516 implementations.

## Priority and ownership

The original order was M0 alongside M1, then M2, static M3, dynamic M3, M4 and
M5. After the implemented increments and latest Portico comparison, use the
reassessment above: M4, immutable-body M1, pending-callback indexing, dynamic M3,
then M5, with current profiling allowed to revise that order. SDK reporting and
pending-callback changes share checkpoint/lifetime invariants and need coordinated
ownership. Server routing is a separate implementation area; callback-body reuse
crosses adapter and SDK boundaries. Concurrent implementation is possible, but
benchmark campaigns must run serially on the same host.

Do not prioritize disabling validation, authentication, MOM accounting, callback
entry/outcome evidence, or ACK/drain semantics. Moving all restore bookkeeping
to the instant of a restore request loses evidence of callbacks already executed.
Changing transport solely because it is presumed faster or increasing goroutine
counts without a profile does not address the confirmed work in this code.

## Evidence locations

`D:/workspace/gorti/benchmark-results/performance-priorities-20261007/` contains
instrumented executables and CPU/allocation/mutex/block profiles. The first
server is `rtid.exe-542180`, the second `rtid.exe-601304`. `diagnostic/` contains
the two same-build long workload repetitions. `probe_cpu.py` and `cpu-probe/`
contain the uninstrumented exploratory experiment and all observations.

The profiling overlay and helper are reused unchanged from
`D:/workspace/gorti/benchmark-results/callback-profile-20261006/` and are not
compiled into the production binaries. Instrumented server SHA-256:
`bd54d4e96a2e8e9c31cf1d293217891370f25bb3dbf839cc7f21ba47462f8b92`.
The CPU probe uses production server SHA-256
`4f77da92379c3efa61f52dc295a7a1e6fc0be9b52811766b253e25bbce0d82e4`.

No full IEEE regression was rerun solely for this investigation, because no
production behavior changed. The prior production-build regression evidence
remains linked in the optimization and Portico-comparison reports.
