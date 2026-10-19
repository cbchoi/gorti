# Batched callback delivery accounting

Date: 2026-10-07. Implementation and prepared service regression are complete
for homogeneous RO frames. Consistent end-to-end performance improvement is
**not established**. This is a bounded M2 increment of the
[performance plan](../design/consistent-performance-plan-20261007.md), not
completion of its performance target or a new Portico ranking.

## Plan

After the comparison-copy optimization, a callback frame still acquired the
receipt mutex for each event and created a timeout/context plus service admission
for each send completion. Amortize these operations for ordinary RO data while
retaining every receipt, payload check, entry/outcome report and checkpoint record.

## Do

- `callback_delivery_batch.go` prepares frames of 2-64 ordinary RO data callbacks
  under one ledger lock and commits successful transport accounting under one
  service admission and one ledger lock. Each callback keeps its own identity.
- `callback_required.go` shares the same locked preparation logic between single
  and batched paths, including scope, source validity and complete payload checks.
- `stream_receipts.go` selects this path only for a homogeneous eligible frame.
  Single-event, oversized, optional-receipt, mixed, TSO, regional, ownership and
  recovery frames retain the existing path. Restore handle transitions are not
  moved across data callbacks. Mixed-prefix batching remains unimplemented.
- `mom_receipts.go` preserves authenticated completed receipts when a late Send
  error arrives or delivery-accounting admission fails. Such receipts must not
  be deleted or decrement pending accounting a second time.

No ledger lock or service admission spans network Send. Failed transport and
preparation-prefix rollback remain per-ticket operations. If the shared success
admission fails, that failure applies to the first prepared ticket and subsequent
tickets use their original individual admission attempts. A transient failure
therefore cannot invalidate the otherwise valid suffix. Teardown/restore still
fences late commits through the original receipt identity check.

There are no new wire messages, SDK changes, options, worker goroutines or
disabled validation/reporting mechanisms.

## Review

### Test-driven corrections

The initial tests reproduced two failures before implementation: 32 separate
admissions for a 32-event frame, and loss of completed receipts after a late Send
error. In the latter case, two completed callbacks incorrectly removed two
additional pending counts: a four-callback scenario had pending=0 and only two
retained records instead of pending=2 and four records.

Review of the first batch implementation found another boundary: invalidating
the whole frame after one failed admission changed transient-error behavior.
A new test reproduced invalid=3/pending=0 instead of invalid=1/pending=2. The
final version applies the first failure individually and falls back for the
remaining tickets. The pre-final binaries and measurements are retained as such,
not presented as final validation.

Fourteen new top-level tests in `callback_delivery_batch_test.go` cover:

- Bounded shared admission, successful accounting and no lease/lock during Send.
- Failed/ambiguous Send, early entry and outcome reports, outcome retries and
  checkpoint phases, including completed control callbacks.
- Invalid payload, ticket, recipient, unadmitted receipt and stale source in the
  middle of a frame; prepared-prefix rollback without touching its suffix.
- Entered-callback suppression, order-preserving compaction and Ready markers.
- Resign, destroy and restore during Send; cancellation and replay retention.
- Persistent, nil-release and transient admission errors; completed receipts
  cannot be retired twice or invalidated by later accounting failure.
- Control/TSO/region/recovery/ownership/optional-receipt and size fallbacks,
  restore handle transitions, and obsolete guarded callbacks.

Existing payload comparison tests and their differential fuzz seeds also passed.

### Final regression

Both final builds reran the complete package set in the prepared IEEE evidence
inventory, the Go verifier, and live Float64/Integer64 checkpoint and control-
outcome recovery tests.

| Build | Required inventory | Top-level passed | Failed | Skipped |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,947 | 0 | 16 |
| `callback_async` | 978/978 | 2,963 | 0 | 0 |

The sync skips are async-only tests. Native race coverage remains outstanding:
the preceding attempt on this environment failed because CGo's C compiler was
unavailable. Lock/lease tests are not a substitute for a race-detector pass.
Full standalone Python/Java SDK campaigns were not repeated for this server-only
increment. Internal regression is not independent whole-standard certification.

### Allocation and admission counts

For 32 successful 64-byte data callbacks, the isolated stream-receipt benchmark
changed from 961 to 806 allocations and from 34,048 to approximately 24,800 bytes
per batch. The final-build 1,000-iteration allocation check reported 24,803 bytes.
This is about **27.2% fewer allocated bytes** in this operation, not a measurement
of total RTI memory use. Successful service admissions fell from 32 to one.
Microbenchmark timing varied substantially and is not used as throughput evidence.

### End-to-end campaigns

All campaigns used the immediately preceding comparison-copy server as baseline,
byte-identical Go SDK executables, runtime-default CPU settings, Windows 11 on a
20-logical-CPU host, Go 1.26.5, `callback_async`, and LocalLRC batch size 32.
Each condition used two independent federate processes and a fresh server.

FOM bytes, per-pair seed/plan, logging, callback validation and measurement
boundaries were identical. DEVStone-HLA HI width/depth 10, 100 external events,
produced 40,600 attribute and 40,600 interaction callbacks. There were 128 warm-up
operations per channel, five warm-up pairs and 20 measured pairs in alternating
AB/BA order per campaign. The subscriber timer runs from arming before VERIFY_START
to dequeue of the final subsequently validated callback, before dispatch. Outcome
ACK drain is required outside this timer. No builds, tests or profiling overlapped
timed trials.

| Campaign | Baseline mean s | Candidate mean s | Aggregate throughput gain | Speedup 95% CI | Faster pairs |
| --- | ---: | ---: | ---: | --- | ---: |
| Pre-final implementation | 1.406538 | 1.364971 | +3.05% | [0.999310, 1.061554] | 11/20 |
| Pre-final repeat | 1.374467 | 1.320466 | +4.09% | [1.011826, 1.068491] | 15/20 |
| **Final implementation** | **1.435380** | **1.405427** | **+2.13%** | **[0.994265, 1.048994]** | **12/20** |

The second campaign was added after an inconclusive first result. A third campaign
measured the final binary after the transient-admission correction. All campaigns
are reported; results from different binaries are not pooled to manufacture a
positive confidence interval.

Final aggregate received throughput was **56,570 -> 57,776 callbacks/second**,
computed as total callbacks / total measured seconds. Mean receive completion
decreased 2.09%, but median completion changed from 1.409740 to 1.419839 seconds.
The paired execution-order-stratified bootstrap speedup interval includes one,
so this campaign does **not** establish a throughput improvement.

Execution-order strata differed materially in the final campaign:

| Order, 10 pairs each | Baseline mean s | Candidate mean s | Aggregate gain |
| --- | ---: | ---: | ---: |
| AB: baseline first | 1.372804 | 1.471338 | -6.70% |
| BA: candidate first | 1.497955 | 1.339515 | +11.83% |

The two pre-final campaigns showed the same direction of order-associated
difference. This is not proof of a specific scheduler, cache or thermal cause.
Alternation also fixes seed parity within each order stratum; distinguishing
seed, order and time effects requires a crossover using both orders for the same
seeds or an appropriately randomized repeated-process design.

Each campaign accepted all 50 trials and matched all 25 paired semantic checks.
No observations were retried, removed or replaced. In every final trial the
subscriber received all 81,200 measured callbacks, retained 81,456 local entries
and 81,456 outcomes including warm-up, and sent 162,934 reports. Each publisher
had 81,456 ACKed operations and zero LocalLRC failures.

## Reflect

Retain the correctness fixes and bounded batching implementation: the reduced
allocation/admission cost is measured and prepared service behavior passes. Do
not mark M2's consistent-throughput acceptance criterion complete. There is no
demonstrated uniform speed gain, and Portico was not rerun in this increment.

Prioritize the M0 measurement investigation before treating another few-percent
change as a consistent advantage. Keep the existing metric while separating
delivery and settlement, balancing both orders for the same seeds, and comparing
long-lived warmed participants with fresh-process trials. M1 immutable payloads,
M3 static FOM indexes and M4 SDK reporting remain open; their benefits cannot be
inferred by adding the percentages in this report.

## Local artifacts

All execution data and binaries are outside Git under
`D:/workspace/gorti/benchmark-results/`:

- `callback-delivery-batch-20261007/`: red/green test logs, allocation benchmarks.
- `callback-delivery-batch-builds-20261007/` and `callback-delivery-batch-ieee-qc-20261007/`: explicitly pre-final build and regression evidence.
- `callback-delivery-batch-paired-20261007/` and `callback-delivery-batch-paired-repeat-20261007/`: complete pre-final campaigns.
- `callback-delivery-batch-final-builds-20261007/`: final binaries and increment-only source archive, not a clean repository snapshot.
- `callback-delivery-batch-final-ieee-qc-20261007/`: final `summary.json` and complete JSONL regression logs.
- `callback-delivery-batch-final-paired-20261007/`: final manifest, plans, summaries and all measured observations.

| Artifact | SHA-256 |
| --- | --- |
| Baseline async server | `463158ece286801f205cba935ae80b2fcb5e264e5dc04cb13c9b5bb4a0560bb6` |
| Pre-final async server | `7406cdc5f8324329d365722a4beb22870377bc25c8ad4b77b48a192d13d67868` |
| Final async server | `153c2664d3c3e88e640c51b4a3619c13c42dfdd917a173535af2e5127640031e` |
| Final sync server | `e8dfef5a22a51f273173ea511df4933ff766ee89576f96079d0edd28bd75e7c5` |
| Unchanged async SDK/verifier | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Final increment source archive | `d8aa78b7b53f45a6732111ab09a6e7978f8ecb7cd531822864100ce08a3f0a56` |
