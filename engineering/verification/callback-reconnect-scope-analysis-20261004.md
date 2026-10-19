# Callback Reconnection and Language Scope Review

Date: 2026-10-04

The analysis and overlay results below predate the test-only implementation
recorded in the final section. Production behavior remains unchanged.
The subsequent [full service rerun](ieee2010-full-service-rerun-post-sync-20261004.md)
also passed, including the corrected reconnect test in the Linux race suite.

## Conclusions

1. C++ completion is outside the selected Go/Python/Java completion scope.
   Existing C++ code was retained, not removed. The legacy C++ example failure
   is not a blocker for that selected scope.
2. The reconnect failure's immediate cause is a missing server-cleanup
   synchronization step in the test. It is not an observed memory data race
   or evidence of incorrect HLA time or federation semantics.
3. Synchronizing the test is the smallest appropriate correction. Improving
   transport error classification is separate product work. Transparent SDK
   reconnection is a larger feature, not something already provided by all
   current SDKs.

No production code or checked-in test was modified for this analysis. The
diagnostic test variants used external Go overlays; their passes do not close
the original failed test in the full-suite evidence.

## C++ Scope

The decision in `engineering/design/ieee1516-2010-java-binding-plan.md:30`
selects Go/Python as primary product interfaces and Java as the standard
language mapping. It explicitly preserves existing C++ code and evidence
without making C++ completion a prerequisite. The current W7 section of
`engineering/design/ieee1516-2010-conformance-execution-plan.md:368` repeats
that decision and supersedes earlier C++-first work.

Repository organization and public descriptions have not fully caught up:

- `README.md:71` still lists C++ in the supported deployment; its following
  paragraph explains that C++ is not the current completion target.
- `docs/index.md` and `docs/architecture.md` still describe Go/Python/C++.
- `pysdk/tests/spec/m17/test_cpp_python_interop.py` has ordinary `spec` and
  `integration` markers, so broad Python collection includes it. It skips
  only when its C++ executable or Go prerequisite is absent.
- `.github/workflows/conformance.yml` is a manually dispatched legacy DLC/C++
  workflow, not the selected Java assessment.

The appropriate cleanup is to make Go/Python/Java the explicit current scope
in the public descriptions and test selection, and keep C++ checks in an
explicit legacy compatibility lane if the code is retained. Deleting the
entire C++ SDK is a different decision; the existing plan did not authorize
that removal. No C++ files were deleted or repaired in this analysis.

The preceding full-rerun report now distinguishes this legacy failure from
the in-scope reconnect failure. Original logs and numerical results remain
unchanged.

## Failure Sequence

The reconnect branch at
`rti/internal/application/server/federate_fault_20261004_test.go:190` does:

```text
cancel client callback stream
immediately open a new stream for the same federate
require the first ready response to succeed
```

Client cancellation does not acknowledge completion of the server handler.
In `rti/internal/adapters/grpc/stream.go`, the old handler first marks
eligible callback receipts for redelivery and then releases its outbox
subscription through deferred cleanup. Until that release,
`MultiOutbox.Subscribe` sees `readerAttached == true` and rejects the new
reader. Its ordinary Go error currently falls through to gRPC `Internal`.

Rejecting two simultaneous readers is intentional: allowing both would split
the callback queue between consumers. The test incorrectly equates a locally
cancelled RPC with a server-side released subscription.

An existing production-server test already handles this distinction.
`conveyedProductionFixture.cancelAndDrainStream`, at
`rti/internal/application/server/conveyed_production_test.go:222`, drains the
cancelled client stream and waits until the old recipient is absent from
`OutboxStats`. Its comment explicitly explains that client cancellation does
not acknowledge the receipt and subscriber cleanup steps.

## Diagnostic Runs

All runs selected
`TestFederateFault20261004ReportAndVoluntaryDistinction`, with the real local
gRPC server, both time representations, all four modes, `-race`, and
`-count=30`. The two overlay runs were not performance comparisons.

| Variant | Top-level passes | Top-level failures | Nested passes | Nested failures |
|---|---:|---:|---:|---:|
| Original test, prior full-rerun follow-up | 25 | 5 | 235 | 5 |
| Wait for server subscriber removal before reopening | 30 | 0 | 240 | 0 |
| Retry only the specific duplicate-subscriber rejection | 30 | 0 | 240 | 0 |

The condition-based probe used `OutboxStats`, a 40 ms maximum wait and
`runtime.Gosched`, without an unconditional sleep. The retry probe used a
40 ms retry budget and 1 ms between attempts, only for the observed duplicate
subscriber error. Both retained the original 80 ms auto-resign grace and all
membership/report assertions. Those diagnostic timings are not proposed
production configuration defaults.

The retry run actually encountered and recovered from four duplicate-reader
rejections. Neither variant emitted a data-race report. These results support
the timing explanation; they are bounded diagnostic evidence, not a proof
of arbitrary network-failure recovery or a replacement full-suite run.

## Recommended Changes

### 1. Correct the test boundary

Wait for actual server-side subscriber release before opening the replacement
stream, following the existing conveyed-region test. Use a bounded condition
wait and a clear timeout failure. Keep the assertions that the federate remains
joined after the grace interval, no false federate-lost report is sent, and
the surviving federates can continue advancing time.

Do not add a fixed sleep, increase grace solely to hide the failure, remove
the reconnect case, or add production accessors solely for this test.

### 2. Classify duplicate subscriptions explicitly

Introduce a typed transport/delivery error for an already attached callback
subscriber and translate it into a distinguishable gRPC status/reason rather
than generic `Internal`. A valid active duplicate should remain rejected.
If a separate closing state is introduced, that state can advertise a bounded
retryable condition. Do not retry arbitrary `Internal` responses or use error
text matching as the production retry contract.

Preserve cleanup ordering: finish the old handler's receipt bookkeeping and
unsubscribe before admitting a replacement reader. Any lifecycle changes
must guard federation generation, membership incarnation and subscription
identity so an old handler cannot clear a replacement's state. Do not wait
while holding locks needed by cleanup, resignation or restoration.

### 3. Treat transparent SDK recovery as an explicit feature

Current behavior differs by SDK:

| SDK | Unexpected callback stream termination |
|---|---|
| Go | `drainEventBatches` exits and closes the public event channel; it does not reopen the RPC. |
| Python | `_drain_events` delivers the error; the standard facade records it and invokes `connectionLost` when provided. |
| Java | `EventSubscription` reports failure; the facade dispatches `connectionLost` and tears down local connection state. |

The server's auto-resign grace permits an authenticated replacement stream;
it does not itself implement an SDK reconnect loop. A product recovery feature
would need bounded retry, ready-handshake confirmation, generation/session
checks, preserved callback identities/order, and tests for callbacks whose
application execution or outcome reporting was interrupted. Failure after the
recovery budget should remain visible. HLA service commands must not be blindly
resent as part of callback-stream recovery.

A forced takeover of the old subscription is not recommended as a quick fix.
It could leave the old reader running or allow late cleanup to interfere with
receipt recovery. First fix the test synchronization, then assess whether
transparent recovery is an intended product guarantee.

## Validation After Implementation

- Repeat the corrected reconnect case under race detection for both time types.
- Keep explicit rejection of a second genuinely active subscriber.
- Check disconnect grace expiry, voluntary resignation, stale/wrong tokens,
  federation recreation and restore/rekey interleavings.
- Recheck pending callback redelivery, completed-outcome recovery, callback
  ordering and continued time advancement after replacement attachment.
- Run the affected server, delivery, session and callback-recovery packages;
  rerun the selected full suite before changing its overall verdict.

## Evidence

The original failure remains in
`D:/workspace/gorti/ieee-service-full-rerun-20261004/linux-reconnect-repeat.jsonl`.

Diagnostic artifacts remain outside the repository under
`D:/workspace/gorti/reconnect-analysis-20261004/`:

- `federate_fault_barrier_probe_test.go`, `linux-barrier-overlay.json`,
  `linux-barrier-probe-r2.jsonl` and `.stderr`.
- `federate_fault_retry_probe_test.go`, `linux-overlay.json`,
  `linux-retry-probe-r2.jsonl` and `.stderr`.

The initial overlay invocations failed before test execution because the
command-line overlay path was truncated. The quoted, separately passed overlay
argument was used for both `r2` executions reported above. These setup failures
are retained separately and are not counted as behavioral test results.

## Test-Only Implementation Follow-Up

The reconnect branch in `federate_fault_20261004_test.go` now waits until
`OutboxStats` no longer contains the old federation/federate subscriber before
opening the replacement callback stream. The wait uses the existing test
context deadline, reports a specific cleanup timeout and yields with
`runtime.Gosched`. No production accessor, retry, fixed sleep or longer
auto-resign grace was introduced. All existing semantic assertions remain.

Verification used the actual edited test file, without the diagnostic overlays:

| Execution | Result |
|---|---|
| Windows related server/delivery/session/callback tests | 16 top-level tests passed, 18 nested passes, 4 packages |
| Linux related tests with `-race` | 16 top-level tests passed, 18 nested passes, 4 packages |
| Linux `TestFederateFault20261004*`, `-race -count=30` | 4 tests repeated 30 times: 120 top-level passes and 420 nested passes |

All three executions exited zero, with no failed or skipped tests. The Linux
runs emitted no data-race reports. The repeated cases cover fault reports and
voluntary resignation/reconnection, instructed-save failure, directory-only
assignment, and atomic fault-report/save admission. Related checks retain
active duplicate-reader rejection, cancellation/re-subscription, stale-token
and timer behavior, pending copied-region callbacks and redelivery ordering.

Fresh raw evidence is outside the repository:

```text
D:/workspace/gorti/reconnect-test-sync-20261004/
  windows-related.jsonl
  windows-related.stderr
  linux-related-race-r2.jsonl
  linux-related-race-r2.stderr
  linux-fault-race-repeat.jsonl
  linux-fault-race-repeat.stderr
```

The initial Linux related-test command failed in shell argument parsing before
Go tests started. The `r2` command used WSL direct execution and ran the full
selected expression. Its setup failure remains separate from test results.

Only the test source and these verification notes changed in this follow-up.
Transport error classification and SDK reconnection behavior were not changed.
The complete repository/IEEE service suite was not rerun after this narrow
test correction; earlier whole-suite results are preserved as historical
evidence, not replaced with these focused passes.
