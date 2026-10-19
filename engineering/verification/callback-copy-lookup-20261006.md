# Profile-guided callback and MOM optimization

The subsequent [fresh Portico comparison](portico-copy-lookup-20261006.md) uses
these candidate binaries with runtime-default CPU settings. Its result and
Portico runtime variability are separate from the gorti-only experiment below.

## Plan

Profile the preceding server and its two Go federates before choosing another
optimization. Preserve IEEE-service behavior, callback receipt verification,
ownership authority, replay isolation and MOM accounting. Compare the previous
and candidate servers with identical SDK executable bytes. Use the prepared
whole-service regression inventory for both callback build profiles.

## Measurement and implementation

An external Go build overlay added an eight-second CPU, allocation, mutex and
block-profile capture after a three-second startup delay. No profiling hooks
were added to production sources. Two diagnostic runs used the existing
DEVStone-HLA HI 10 x 10 workload with 1,000 external events, independent
publisher/subscriber processes, LocalLRC batch size 32, `callback_async` and
`GOMAXPROCS=2`. Diagnostic timings are not used as performance results.

In the first server CPU profile, `mom.ServiceGroup` and its `strings.Fields`
calls accounted for 1.06 of 13.92 sampled CPU seconds (7.61%, cumulative).
The second profile also showed repeated field splitting (9.70%). The first
allocation-profile difference attributed about 0.58 GiB of 5.28 GiB to
`strings.Fields`. These are sampled allocations during the capture, not peak
resident memory. Every observed operation was reparsing the service catalog.

The first server profile also attributed 17.60% of sampled CPU time to
`proto.Clone` across several call sites. Inspection found that
`ownershipReceiptBatchesStream.Send` cloned the entire batch, discarded the
cloned event list, then cloned each original event again. This increment removes
that duplicate copy, not all callback cloning or payload verification.

Two production files changed:

- `rti/internal/hla/mom/service_groups.go`: construct an immutable lookup map
  once at package initialization. Keep exact name matching, existing group
  numbers, unknown-name behavior and first-match handling of duplicate names.
- `rti/internal/adapters/grpc/stream_ownership_receipts.go`: clone the batch once,
  prepare and filter its cloned events in place, then clear discarded tail
  references. The retained outbox and transmitted data remain deeply isolated.
  Protobuf unknown fields and the ready marker survive the clone unchanged.

Authentication, receipt checks, per-event ownership preparation, rollback,
duplicate suppression, transport completion and MOM reports are unchanged.
Neither SDK source nor the wire protocol changed. Both reporting profiles use
the optimization; `callback_async` remains opt-in.

### Focused measurements

These single microbenchmark runs explain local costs; the paired workload
experiment below is the end-to-end evidence.

| Operation | Previous | Candidate |
| --- | ---: | ---: |
| `ServiceGroup(updateAttributeValues)`, ns/op | 2,600 | 6.448 |
| Same lookup, bytes / allocations per op | 1,440 / 3 | 0 / 0 |
| `ServiceGroup(sendInteraction)`, ns/op | 2,595 | 5.718 |
| Unknown service lookup, bytes / allocations per op | 3,040 / 7 | 0 / 0 |
| Ownership wrapper, 64 data callbacks, ns/batch | 90,016 | 42,743 |
| Same wrapper, bytes / allocations per batch | 89,699 / 1,161 | 45,641 / 585 |

## Review

The allocation regression test was added first and failed on the old lookup:
three allocations for ordinary object-service names and seven for unknown
names. It passes after the change. Catalog tests exercise every listed service
and reject case/whitespace variants and unknown names.

A new batch test verifies deep input isolation, ready and unknown-field
preservation, and independent payload/receipt mutation in the transmitted copy.
Existing ownership tests exercise invalid later entries, rollback, duplicate
filtering, transport failure, early outcome reporting and checkpoint recovery.
The targeted tests passed before the full-service campaign.

Both whole-service runs completed against the candidate production executables:

| Build profile | Required inventory | Top-level passes | Failures | Skips |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,924 | 0 | 16 async-only cases |
| `callback_async` | 978/978 | 2,940 | 0 | 0 |

The campaign included live Float64/Integer64 restart and control-result recovery,
callback reconnect/restore, ownership, MOM, wire projection and retained-FDD
schema checks. No required test was skipped or weakened. These are prepared
internal service regressions, not independent full IEEE certification. Complete
Python and Java SDK campaigns were not repeated. A fresh race run was not
performed because the native C compiler required for Go race builds is missing.

## Paired performance experiment

Both conditions used the same FOM bytes, per-pair seed, workload plan and
unchanged Go SDK binaries. Each trial started a fresh server and two independent
federates. The workload was DEVStone-HLA HI 10 x 10 with 100 external events:
40,600 attribute callbacks plus 40,600 interaction callbacks in the measured
phase. Both conditions used `GOMAXPROCS=2`, `callback_async`, LocalLRC batch size
32, handle callbacks and compact summary logging.

Five warm-up pairs preceded 20 measured AB/BA pairs. Every trial also included
128 warm-up operations per channel. No profiling, build or regression-test
process ran during measurement. Other host background activity was not isolated.

Timing starts when the subscriber is armed before VERIFY_START and ends at the
dequeue timestamp of the last subsequently validated callback, before
DispatchEvent. Final callback-report ACK drain remains required outside the
timer. This is delivery completion, not handler-completion latency.

All 50 trials were accepted and all 25 semantic pairs matched. Every measured
trial delivered 81,200 callbacks with no dropped, duplicate, invalid, unexpected
or rejected callbacks. All observations, including the slower pair 12, were kept.

| Metric | Previous server | Candidate server |
| --- | ---: | ---: |
| Mean completion, seconds | 1.505689 | 1.267717 |
| Median completion, seconds | 1.481165 | 1.250534 |
| Standard deviation, seconds | 0.091215 | 0.058268 |
| Aggregate callbacks/second | 53,928.8 | 64,052.1 |
| Mean per-trial callbacks/second | 54,084.1 | 64,168.1 |

Mean completion decreased **15.80%**; aggregate throughput increased **18.77%**.
The mean-time speedup is **1.18772**, with an execution-order-stratified paired
bootstrap 95% interval of **[1.17387, 1.20186]**. All 20 measured pairs favored
the candidate. Aggregate throughput is total callbacks divided by total measured
time, not the mean of the per-trial rates.

Every trial retained 162,934 subscriber reports, 81,456 local data entries and
81,456 asynchronous outcomes, including warm-up and control traffic. Mean report
frames were 5,277.3 versus 4,819.8; mean publisher operation frames were 2,678.35
versus 2,687.45. These frame counts depend on scheduling. No report or logical
operation was removed to obtain the improvement.

## Reflect

Retain both changes: they remove repeated work without changing service
boundaries or introducing timing-dependent batching delays. The experiment
measures their combined effect, not the separate end-to-end contribution of
each optimization.

Remaining profile-guided candidates include cloning and equality checks in
`CallbackReceipts.preparedDelivery` while holding the receipt mutex, and
recipient/FOM snapshot allocations. A subsequent change must preserve payload
mismatch detection and reconnect/restore evidence; disabling those checks is
not equivalent. New profiles should re-rank costs after this increment.

Portico was not rerun for this increment. The preceding
[Portico campaign](portico-server-callback-batch-20261006.md) used runtime-default
CPU settings and a different collection period. Its numbers must not be combined
with this `GOMAXPROCS=2` experiment to claim a new cross-RTI speedup or victory.

## Local evidence

Execution artifacts remain outside the repository:

- `D:/workspace/gorti/benchmark-results/callback-profile-20261006`: diagnostic
  overlay, instrumented binaries, two long workloads and profiles for all three
  processes. The first server capture is `rtid.exe-393144.*.pprof`.
- `D:/workspace/gorti/benchmark-results/callback-copy-lookup-builds-20261006`:
  non-instrumented candidate executables and `source-increment.tgz`, containing
  the four changed/new Go files plus `go.mod` and `go.sum`. This is an increment
  snapshot, not a complete source-tree archive.
- `D:/workspace/gorti/benchmark-results/callback-copy-lookup-paired-20261006`:
  manifest, plans, summaries, semantic evidence and paired comparison.
- `D:/workspace/gorti/benchmark-results/callback-copy-lookup-ieee-qc-20261006`:
  whole-service regression output.

SHA-256 identities:

| Artifact | SHA-256 |
| --- | --- |
| Previous async server | `768765cfd9265c0d316705c657f3fdcf18eae51db0c54ffa1722a743ed094dab` |
| Candidate async server | `4f77da92379c3efa61f52dc295a7a1e6fc0be9b52811766b253e25bbce0d82e4` |
| Candidate sync server | `41b1ad14ef4f4067d529016ee5bf8bdd21ab722fe12e08ecbec7033102746cdc` |
| Async SDK, identical on both sides | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Source increment archive | `7428fe1ff92b8bb09b3d1d48d1a9b0f47b86b9b85cf3081cd117acf79b05dded` |
