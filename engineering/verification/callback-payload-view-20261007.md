# Callback comparison without payload copying

Date: 2026-10-07. Status: M1's comparison-only copy removal is implemented and
measured. Immutable retained bodies and the other milestones remain open in the
[consistent-performance plan](../design/consistent-performance-plan-20261007.md).

## Plan

The latest server profiles showed payload cloning and equality checks inside
`CallbackReceipts.preparedDelivery`, where delivery competes with callback
reports for the receipt mutex. Remove the clone used only to normalize authority
before comparison. Do not remove payload checks, receipt authentication, callback
entry/outcome reports, source validity checks, or recovery bookkeeping.

## Do

`rti/internal/adapters/grpc/callback_required.go` now constructs a temporary
read-only envelope referencing the transport-owned payload. It omits the receipt
already used for authenticated ledger lookup, preserves all other headers and
unknown fields, and applies the existing ownership callback-ID/attempt rules.
`proto.Equal` still performs the complete comparison against the retained event.

The view exists only during comparison under the existing ledger lock. It is not
stored or returned, does not copy generated message lock state, and never edits
the wire input or ledger body. Admission, dispatch and transport defensive copies
remain. Retry, checkpoint ordering, obsolete-advisory checks and transport commit
behavior are unchanged. No protocol, SDK or runtime configuration was changed.

## Review

New tests in `callback_payload_test.go` cover all 43 callback variants, header
coverage, modified payloads/map keys/tags, knowledge epochs, unknown fields,
ownership attempt normalization, recovery identity/completion, admission
isolation and concurrent readers. Nil/empty maps and bytes, NaN, typed-nil
oneofs and differently ordered unknown fields retain Protobuf equality semantics.
The pre-change clone-and-normalize implementation is retained as a test oracle.

The initial deterministic tests passed before and after the implementation change;
the equivalent-encoding test and differential fuzzing were then added. Fuzzing
executed 55,960 inputs in a 30-second budget without finding a result difference.
After the full run, the concurrent-reader test was changed to use `Add`/`Done`
instead of the newer `WaitGroup.Go` API; all focused tests and fuzz seeds passed
again. This test-only edit does not change either measured executable.

| Prepared-delivery microbenchmark | Before | After |
| --- | ---: | ---: |
| Allocations/op, 64-byte and 4-KiB payloads | 35 | 25 |
| Bytes/op, 64-byte payload | 1,336 | 736 |
| Bytes/op, 4-KiB payload | 5,368 | 736 |

These are allocations in the isolated preparation operation, not total server
allocations per delivered callback. Six microbenchmark repetitions per case had
substantial timing variation; their timing means are not used as speedup evidence.

### IEEE service regression

Both builds ran the packages in the prepared IEEE evidence inventory, the Go
federate verifier, and live Float64/Integer64 checkpoint/control-outcome recovery.

| Build | Required inventory | Top-level passed | Failed | Skipped |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,933 | 0 | 16 |
| `callback_async` | 978/978 | 2,949 | 0 | 0 |

The 16 sync skips are async-only tests. Native race testing could not execute:
the default environment has CGo disabled, and an explicit `CGO_ENABLED=1` attempt
failed because `gcc` is unavailable. No race pass is claimed. Full standalone
Python/Java SDK campaigns were not repeated for this server-only change. These
results establish the prepared internal regression result, not independent
whole-standard certification.

### Paired end-to-end measurement

The benchmark used the immediately preceding production server and the new
server, with byte-identical Go SDK executables. Both conditions used Windows 11,
Go 1.26.5, runtime-default CPU settings on a 20-logical-CPU host, `callback_async`,
LocalLRC batch size 32, identical FOM bytes, same per-pair seed/plan, and two
independent federate processes plus a fresh server per trial.

DEVStone-HLA HI width/depth 10 with 100 external events produced 40,600 attribute
and 40,600 interaction callbacks. Each trial warmed 128 operations per channel.
Five warm-up pairs preceded 20 measured pairs in alternating AB/BA order.
The subscriber timer starts before VERIFY_START and ends at the dequeue timestamp
of the last subsequently validated data callback, before dispatch. Final outcome
ACK drain is required outside this timer. Logging is compact summaries only.
No build, test, fuzzing or profiling ran during the timed campaign.

| Metric | Before | After |
| --- | ---: | ---: |
| Mean completion, seconds | 1.474248 | 1.364082 |
| Median completion, seconds | 1.455744 | 1.321917 |
| Standard deviation, seconds | 0.090232 | 0.092941 |
| Aggregate received callbacks/second | 55,079 | 59,527 |

Aggregate throughput is total measured callbacks divided by total measured
seconds, not the arithmetic mean of per-trial rates. Completion decreased
**7.47%**, and aggregate throughput increased **8.08%**. The ratio-of-means speedup
is **1.080762**, with paired execution-order-stratified bootstrap 95% CI
**[1.052102, 1.110865]**. The candidate was faster in 15 of 20 measured pairs.

All 50 trials were accepted, and all 25 paired semantic checks matched. Every
subscriber received 81,200 measured callbacks, retained 81,456 local entries and
81,456 asynchronous outcomes including warm-up, and sent 162,934 callback reports.
Every publisher had 81,456 ACKed operations and zero LocalLRC failures. No trial
was retried or excluded. The improvement did not come from suppressing reports.

## Reflect

Retain this change: memory allocation decreases and the fresh paired campaign
shows a positive mean improvement with unchanged semantic evidence. It does not
establish superiority over Portico: Portico was not rerun in this increment, five
measured pairs were slower, and the current mean remains above the plan's target.
Do not compare this campaign's absolute time directly with an older campaign to
infer a code gain or loss; CPU settings and host/runtime variation matter.

Next, use the remaining M1 immutable-body work and M2 delivery-ledger batching to
address copies, per-callback admission/context creation and mutex traffic. Do not
treat a shared body as sufficient proof of current delivery permission or remove
recovery state. This result is a measured first step, not completion of M1-M5.

## Local artifacts

All execution logs, trial data and binaries remain outside the repository under
`D:/workspace/gorti/benchmark-results/`:

- `callback-payload-view-20261007/`: before/after allocation benchmarks, differential fuzz log and race-attempt diagnostics.
- `callback-payload-view-builds-20261007/`: sync/async binaries and an increment-only source archive, not a clean repository snapshot.
- `callback-payload-view-ieee-qc-20261007/summary.json`: required-test reconciliation and full JSONL run logs.
- `callback-payload-view-paired-20261007/`: manifests, plans, all observations and `comparison.json`.

SHA-256 identities:

| Artifact | SHA-256 |
| --- | --- |
| Baseline async server | `4f77da92379c3efa61f52dc295a7a1e6fc0be9b52811766b253e25bbce0d82e4` |
| Candidate async server | `463158ece286801f205cba935ae80b2fcb5e264e5dc04cb13c9b5bb4a0560bb6` |
| Candidate sync server | `e26ebb5f75abb9ce0e176bf4efa07b22ba525a506a75586f1b2f5907485e8453` |
| Unchanged async SDK/verifier | `a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd` |
| Increment source archive | `194b14647224a562f5d36180e383b42acf2a888728f67bd4003145b08ed95803` |
