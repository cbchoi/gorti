# LocalLRC recovery and callback report batching

## Scope

The changes preserve service validation, callback entry admission, callback
outcomes, error propagation, and checkpoint boundaries. They do not remove
confirmation from Time Management, regional callbacks, or control callbacks.
The optional `callback_async` build remains opt-in; the default build remains
`callback_sync`.

The quality criterion is the prepared IEEE 1516-2010 supported-profile evidence
manifest, plus focused delivery and SDK regressions. Passing this internal
suite is not an independent certification of full IEEE conformance.

## M1: Authenticated LocalLRC

- Plan: restore the existing Go LocalLRC path without relaxing server checks.
- Do: attach the joining federate's session token when opening its stream.
- Review: a new production TCP/SDK regression reproduced `Unauthenticated`
  before the change. After the change it checks unpublished-interaction
  rejection, ordered payload delivery, and complete cumulative ACK accounting.
  Float64 uses LocalLRC; Integer64 retains its existing confirmed fallback.
  Focused LocalLRC, MOM, checkpoint, callback, FQR and retraction regressions
  passed with both callback profiles.
- Reflect: recovering the sender path is necessary, but does not by itself
  remove the subscriber's callback-entry round trips.

The paired DEVStone-HLA run used identical async-profile binaries for both
transport choices, HI width/depth 10, 4,060 measured data callbacks, two native
Go federate processes, five warm-up pairs, twenty measured AB/BA pairs, and
128 warm-up operations per channel. All 25 pairs agreed on semantic evidence.
The timer runs from the subscriber's pre-START arm to the dequeue timestamp of
its last subsequently validated data callback. The final report ACK drain is
required before teardown but lies outside the timed interval. This is not
per-call server-confirmed latency or a Time Management benchmark.

| Metric | Confirmed producer | LocalLRC producer |
| --- | ---: | ---: |
| Mean completion time, seconds | 2.290207 | 2.288929 |
| Median completion time, seconds | 2.119628 | 2.198238 |
| Completion-time standard deviation, seconds | 0.979922 | 0.760059 |

The completion speedup was 1.00056, with a paired, order-stratified bootstrap
95% interval of [0.84931, 1.16996]. This does **not** establish a performance
improvement. LocalLRC was faster in 11 of 20 measured pairs.

## M2: Coalesced callback outcomes and entries (not retained)

- Plan: reduce separate report frames without allowing application code to
  run before successful callback-entry admission.
- Do: give an isolated asynchronous outcome a bounded 200-microsecond
  coalescing window. A subsequent entry or explicit report drain wakes it
  immediately. An isolated last outcome still progresses on its own.
- Review: focused tests passed for ordered entry/outcome delivery, tail drain,
  rejected entry, lost ACK identity, bounded queues, retained failures, service
  barriers, MOM, save/restore, retraction, and regional callbacks.
- Reflect: the 20 measured pairs averaged 2.186346 seconds before and 2.092782
  seconds after, a 1.04471 speedup with a 95% interval of [0.99248, 1.09957].
  This was not conclusive. The timer and enqueue wake-up changes were removed;
  the ordered-invocation and isolated-tail regression test was retained.

Both conditions used LocalLRC, `callback_async`, and `GOMAXPROCS=2`. These
absolute times should not be compared directly with M1, which used the runtime
processor default. All 25 warm-up/measured pairs matched semantic evidence.

## M3: Reuse immutable callback identity

`trackRuntimeCallback` previously recalculated SHA-256 and hex encoding for
every pending receipt whenever another callback arrived. The pending queue now
retains the identity already calculated for its detached wire image. Recovery
deduplication and completed-marker reconciliation compare that same identity;
the checkpoint format, receipt bytes, and callback body are unchanged.

A regression checks fresh recovery receipts with the same invocation identity,
detached payload ownership, completed-marker removal, and new queue membership
after retirement. Related SDK, server, checkpoint and MOM tests passed.

Three microbenchmark repetitions gave these median admission costs:

| Pending callbacks | Before, ns/op | After, ns/op | Before allocations/op | After allocations/op |
| --- | ---: | ---: | ---: | ---: |
| 16 | 3,573 | 897.5 | 42 | 10 |
| 128 | 21,307 | 1,135 | 266 | 10 |
| 1,024 | 165,613 | 3,320 | 2,058 | 10 |

At 1,024 pending callbacks, allocated bytes fall from 131,736 to 664 per new
callback. These are isolated queue-admission measurements, not RTI end-to-end
speedups. The end-to-end run and final quality campaign are reported below.

## Evidence

Raw results and execution logs remain outside the repository. The paths below
are relative to the repository root:

- `../benchmark-results/local-lrc-recovery-builds-20261005/`
- `../benchmark-results/local-lrc-recovery-paired-20261005/`
- `../benchmark-results/callback-coalesced-builds-20261005/`
- `../benchmark-results/callback-coalesced-paired-20261005/`
- `../benchmark-results/callback-identity-builds-20261005/`
- `../benchmark-results/callback-identity-paired-20261005/`

The first broad Go test launch was stopped before any test event was emitted.
Its empty transcript is retained in `performance-ieee-qc-20261005`. The retry
selects the evidence manifest's exact packages and limits Go to two scheduler
processors. It does not omit any of the manifest's 978 unique required tests.

The M2 assessment reconciled 978/978 required tests for each profile. Its wider
package results were 2,893 passes and six async-only skips for `callback_sync`,
and 2,899 passes with no skips for `callback_async`. Initial failures were a
missing descriptor baseline for the previously added optional report service,
an incorrect Linux `lxml` path in the Windows test environment, and unary-only
fault injection in the live recovery test. Corrections retain all historical
wire hashes and apply the same lost-ACK/unsent-outcome faults to both transports.

The M2 Linux race run passed for the Go SDK, gRPC adapter and application server.
Evidence is in `performance-ieee-qc-20261005-retry/reconciled.json` and
`callback-coalesced-builds-20261005/race-result.json`.

## Final quality results

M3 was assessed again from the final source, without the coalescing timer. Both
complete exact-package campaigns exited successfully without supplemental
reruns:

| Gate | Result |
| --- | --- |
| Prepared IEEE supported-profile manifest, sync | 978 / 978 required tests passed |
| Prepared IEEE supported-profile manifest, async | 978 / 978 required tests passed |
| Wider sync package campaign | 2,894 passed; 6 async-only skips; 0 failures |
| Wider async package campaign | 2,900 passed; 0 skips; 0 failures |
| Linux race: SDK, gRPC, application server | 907 top-level tests passed; 0 failures |

These counts include the live Float64/Integer64 restart and control-outcome
recovery tests. All historical descriptor projections and official FDD schema
oracles also pass. The final run did not disable validation, replace failed
cases with easier cases, or omit any manifest requirement.

See `../benchmark-results/performance-ieee-qc-20261005-final/summary.json` and
`../benchmark-results/callback-identity-builds-20261005/race-result.json`.
Python and Java whole-binding campaigns were not rerun in this Go SDK change.
The server executable is unchanged; no Portico performance run was made here.

## Final performance results

The final experiment compares M1 with M3, not with the inconclusive M2 timer.
Both conditions use LocalLRC, `callback_async`, `GOMAXPROCS=2`, the same FOM and
seed per pair, two independent Go federate processes, 4,060 measured callbacks,
128 per-channel warm-up operations, five warm-up pairs and twenty measured
AB/BA pairs. The server binary SHA-256 is identical in both conditions:
`3e7d283fb778c67b71d7ee5ad29818cc47bd1b294d636dfc3afc1c0f6e8cbd7b`.

| Metric | Before identity reuse | After identity reuse |
| --- | ---: | ---: |
| Mean completion time, seconds | 2.248922 | 1.457368 |
| Median completion time, seconds | 2.110356 | 1.454442 |
| Completion-time standard deviation, seconds | 0.476588 | 0.241942 |
| Aggregate callbacks/second (total callbacks / total duration) | 1,805.31 | 2,785.84 |

Completion time decreased by **35.20%**, corresponding to a **1.54314** speedup
and the same 54.31% aggregate-throughput gain. The paired, order-stratified
bootstrap 95% speedup interval is **[1.36779, 1.73706]**. The candidate was
faster in **19 of 20** measured pairs. All **50 trials** succeeded and all
**25 pairs** matched semantic counts and hashes; no failed trial was dropped.

The measurable gain comes from the Go federate SDK's callback bookkeeping,
not a faster HLA server algorithm or relaxed service checks. The identity
reuse also applies to the default SDK build, but the quantitative end-to-end
result above is specifically for the async report profile. This shared-host,
two-federate RO workload does not establish performance against Portico,
Python/Java client performance, or TM/scaling behavior. No other task-initiated
tests or builds ran during the timed campaign.

Reproduce with `benchmark/callback-reporting/compare_transports.py`, using the
recorded baseline and candidate binaries and `--baseline-mode local-lrc
--candidate-mode local-lrc`. The external manifest records binary hashes,
environment, plans, and the exact measurement boundary.

## M4: negotiated local callback entry

The next bottleneck was the receiver's entry ACK, not the producer's LocalLRC
queue. M3 still exchanged about 4,454 callback-report batches per trial while
the producer sent about 141 data frames, including warm-up and control traffic.
The M4 change delegates admission only for ordinary, untimed, nonregional RO
data callbacks from the authenticated server stream.

The application server explicitly advertises the local-entry capability for
its mandatory receipt-backed data path. A successful authenticated report is
required before the SDK activates it. The runtime callback ledger retains the
immutable eligibility decision, receiver, epoch and invocation identity.
Untracked events, recovery carriers, TSO, conveyed-region callbacks and controls remain confirmed;
a closed callback stream also restores the confirmed path for buffered events.

Local ENTRY and OUTCOME reports share the existing bounded queue and cumulative
ACK stream. Their failures have separate keys. Retry drains accepted work,
replays unacknowledged entries in local invocation order, and then reports
finished outcomes. It never reexecutes the application body. The explicit
ReportCallbackEntry API remains synchronous. Service-call ordering continues
to wait for preceding report ACKs, including calls from inside a callback and
save/restore services. MOM counters do not increase just because data arrived.

No Protobuf descriptor, RPC or checkpoint format was changed. The optimization
is negotiated by the `callback_async` Go SDK build; the default synchronous
build and Python/Java reporting paths are unchanged. The historical profile
string is retained, so binary hashes and the new LocalEntries counter distinguish
M4 from the older outcome-only implementation.

Two new tests failed against M3 before implementation: one blocked at the first
entry ACK, and one could not retain an executed result after an entry-report
failure. Both pass after M4. Additional tests cover backpressure deadlines,
legacy fallback, stale epochs, failed authentication, disconnection and excluded
callback categories. The production SDK test verifies Float64/Integer64 local
entry, payload order, and MOM counts of zero before dispatch and 64 after drain.

### M4 quality results

| Gate | Result |
| --- | --- |
| Prepared IEEE supported-profile manifest, sync | 978 / 978 required tests passed |
| Prepared IEEE supported-profile manifest, async | 978 / 978 required tests passed |
| Wider sync package campaign | 2,896 passed; 9 async-only skips; 0 failures |
| Wider async package campaign | 2,905 passed; 0 skips; 0 failures |
| Linux race: SDK, gRPC, application server | 912 top-level tests passed; 0 failures |

Both full campaigns include actual server restart and control-outcome recovery
with Float64 and Integer64, official FDD schema checks and wire descriptor
goldens. No required case was omitted or weakened. This is the prepared
supported-profile test result, not independent certification of all IEEE
requirements. Whole Python/Java binding campaigns were not rerun.

Evidence: `../benchmark-results/local-entry-ieee-qc-20261005/summary.json` and
`../benchmark-results/callback-local-entry-builds-20261005/race-result.json`.
The race source archive SHA-256 is
`085c41c9aa60c63654a72d8dfda560faa84df37d385bcaba05af12a7de1677c8`.

### M4 performance results

The paired experiment uses the same M4 server executable in both conditions:
`a6db19669f63de20160a1379ffd1eced91158e53cbbdeffdc378ad2fb4cd9fd0`.
The baseline uses the M3 SDK binary; it ignores the new capability and still
waits at each data entry. The candidate uses the M4 SDK. Both producers use
LocalLRC and both clients use the async outcome build, numeric-handle callbacks,
the same FOM/seed/workload, two independent federate processes, and
`GOMAXPROCS=2`. No task-initiated build or regression test ran during timing.

Five warm-up pairs precede twenty measured AB/BA pairs. Each trial contains
4,060 measured data callbacks and 128 warm-up operations per channel. All
50 trials succeeded and all 25 pairs matched callback counts, payload/order
hashes and terminal evidence. No failed trial was discarded.

| Metric | M3 SDK, confirmed entry | M4 SDK, local entry |
| --- | ---: | ---: |
| Mean completion time, seconds | 0.915796 | 0.153209 |
| Median completion time, seconds | 0.904788 | 0.153705 |
| Standard deviation, seconds | 0.099212 | 0.009356 |
| Aggregate callbacks/second | 4,433.30 | 26,499.75 |
| Mean subscriber report batch/ACK exchanges per trial | 4,436.95 | 181.45 |

Completion speedup is **5.97743**, with paired order-stratified bootstrap 95%
interval **[5.74474, 6.21381]**. Completion time falls **83.27%**. The candidate
is faster in **20 of 20** measured pairs. It records **4,316 local entries**
in every measured trial, including 256 warm-up callbacks. All data outcomes
are reported; receipt retries remain idempotent and do not repeat user callbacks.

The measurement endpoint remains the last subsequently validated data event's
dequeue timestamp, before DispatchEvent. Final ACK drain is mandatory but
outside the timer. This is delivered-workload throughput, not remote-counter-
settled throughput. The shared host was not isolated from user applications.
The contemporaneous baseline is faster than in the earlier M3 campaign, so
cross-campaign absolute durations must not be used as another speedup estimate.

M4 does not include a fresh Portico comparison. Its 5.98-fold result is a
within-gorti comparison; it cannot be multiplied into the historical Portico
ratio. A new paired Portico campaign is needed to measure the remaining gap.

Results, executable hashes and manifests are outside the repository:
`../benchmark-results/callback-local-entry-paired-20261005/` and
`../benchmark-results/callback-local-entry-builds-20261005/`.
