# IEEE 1516-2010 Service Test Rerun, 2026-10-04

Historical result. The subsequent
[full rerun after test synchronization](ieee2010-full-service-rerun-post-sync-20261004.md)
passed the selected Go/Python/Java campaign and requirement execution links.
The original failure records below are retained unchanged.

## Verdict

The prepared service suite is **not entirely green**. Windows Go, Python
service tests, Java and the executable IVCT-derived subset passed. The full
Linux Go race run failed one reconnect test. A C++/Python interoperability
test skipped on Windows was subsequently executed on Linux and failed, but
that legacy compatibility check is outside the selected Go/Python/Java
completion scope and is not a blocker for that scope.

Scope clarification after review of the 2026-09-22 Java binding decision:
C++ completion was deferred and existing code/evidence were to be preserved.
It was incorrect to present its example failure alongside the reconnect
failure without clearly separating required and legacy checks. The raw C++
result remains recorded; it is not relabeled as passing or deleted.

This run did not change production code or test assertions. The failures
remain open; repeat passes have not replaced the original failed records.

A subsequent test-only synchronization correction passed focused Windows and
Linux/race checks, including 30 repetitions of the reconnect case. See the
implementation follow-up in
[the reconnect analysis](callback-reconnect-scope-analysis-20261004.md#test-only-implementation-follow-up).
This report retains the original full-run results; the focused follow-up is
not a new whole-suite execution.

The tested worktree was on `codex/ieee2010-feature-completion`, based on
`f257c3aa73570df38670f711af21067aa2c21c00`, with existing uncommitted changes.
The commit alone therefore does not identify all tested source changes.

## Results

Counts are top-level tests or JUnit cases. Nested Go tests, platform repeats
and supplementary runs are not added together as unique test coverage.

| Execution | Passed | Failed | Skipped | Detail |
|---|---:|---:|---:|---|
| Windows Go RTI and conformance packages | 3,349 | 0 | 0 | 72 passing packages; 6,698 nested passes |
| Windows Go SDK actual-server checkpoint tests | 2 | 0 | 0 | `runtime_checkpoint_live` build tag; 4 nested passes |
| Python SDK/service suite, excluding RL/PPO and general lint | 2,157 | 0 | 1 | C++ publisher binary unavailable on Windows |
| Java SDK full test selection | 591 | 0 | 0 | Live server, source diagnostics and checkpoint fixture readers enabled |
| Executable IVCT-derived subset | 35 | 0 | 0 | Internal subset, not external certification |
| Linux Go RTI and conformance packages, `-race` | 3,348 | 1 | 0 | 71 passing packages, 1 failing package; 6,697 nested passes, 1 nested failure |
| Linux C++ publisher / Python subscriber follow-up | 0 | 1 | 0 | Previously skipped test executed with freshly built C++ publisher |
| SDK-state Java fixture reader supplement | 3 | 0 | 0 | 6 nested passes against copied base, local and recovery fixtures |
| IEEE traceability/tooling unit tests | 133 | 0 | 0 | Separate from RTI behavior; 112 additional pytest subtests passed |

The Windows and Linux full Go runs used `-count=1`; Go test-result caching
was not accepted as execution. No Go data-race detector report was emitted.
The Linux failure is an assertion failure during stream reconnection.

The Java MIM source diagnostic ran as part of the full suite and passed.
Both time representations, the revised MIM handling, live Support lookup
errors, callback recovery and the available save/restore tests were included
in their respective suites. The Windows Python C++ skip is retained in the
original JUnit file, independently of the failing Linux follow-up.

## Open Failures

### Reconnection before callback subscriber cleanup

`TestFederateFault20261004ReportAndVoluntaryDistinction/Float64/reconnect`
failed in the full Linux race run:

```text
stream not authenticated/ready: <nil> rpc error: code = Internal
desc = rtid: subscriber already registered for federation "fault"
```

The test cancels a raw gRPC callback stream and immediately opens another
stream for the same federate. Cancellation does not wait for the old server
handler to release the subscriber. `MultiOutbox.Subscribe` rejects an
overlapping subscriber while that cleanup is pending.

An unchanged-source repeat of the entire top-level test under `-race
-count=30` produced 25 passes and 5 failures: two Float64 reconnect failures
and three Integer64 reconnect failures. All five reported the same duplicate
subscriber error. The full-run failure and these repeat failures are both
retained in the execution reconciliation.

Relevant code:

- `rti/internal/application/server/federate_fault_20261004_test.go`: raw
  callback stream cancellation followed by immediate reconnect.
- `rti/internal/adapters/grpc/stream.go`: subscriber release on handler exit.
- `rti/internal/adapters/delivery/outbox.go`: duplicate-reader rejection.

The evidence establishes a reproducible reconnect/teardown timing problem,
not a memory data race. It does not by itself establish an IEEE service
violation: the raw-RPC reconnect contract and test synchronization must be
resolved before choosing a production or test correction. No retry, sleep,
assertion weakening or duplicate-subscriber bypass was added in this run.

### C++ example registers an unreserved object name

`test_spec_m17_7_cpp_publisher_python_subscriber` failed when executed on
Linux using the newly built `cpp_ieee1516e_publisher`:

```text
publisher error: registerObjectInstance:
object instance name has not been reserved
```

`examples/cpp-ieee1516e-smoke/publisher.cpp:92` calls
`registerObjectInstance(vehicle, "cpp-car-1")` without first reserving that
name. Registration fails, so the Python subscriber receives no discovery
callback and the test assertion fails. This is evidence of an outdated
example/test interaction with the server's name-reservation contract, not
evidence that the server should accept the unreserved name. The example must
reserve the name and handle the reservation outcome before registration.

## Requirement Record Reconciliation

The current requirement files were reconciled with this run's Go JSON,
Python/Java JUnit, IVCT subset, supplementary fixture checks and failed
reconnect repeats. Failed or skipped variants were not discarded.

- 712 requirement records: 639 applicable, 48 not applicable, 25 unresolved.
- All 639 applicable records have test links; no applicable record is unlinked.
- 877 test links: 875 have passing records only; 2 have a nonpassing variant.
- Both nonpassing links reference the same reconnect test, in
  `FM2010-FAULT-REPORT` and `FM2010-SAVE-FAILURE-FAULT`.
- No linked test is missing execution evidence in this reconciliation.

The C++ interoperability failure is additional suite evidence; it is not
represented by those two failing requirement links. Passing linked tests
does not establish exhaustive standard coverage. The existing review's
`coverage_complete` and `full_conformance` remain `false`.

## Scope and Execution Notes

This is the IEEE service rerun requested in the preceding discussion, not a
claim that every repository tool, optional backend and research experiment
was exercised. PPO/RL tests and their reference-data checks were excluded as
requested. General SDK lint, performance benchmarks, soak tests, optional DDS
build tags and a full C++ conformance campaign were not run.

The two Go SDK actual-server tests have a Windows-only build constraint.
They ran on Windows. An additional tagged Linux invocation selected zero
tests and is not counted as a pass of those cases.

The fixture supplement explicitly enabled Java-generated base, local and
recovery input readers that otherwise return early without their optional
environment variables. It reused copied checkpoint fixtures; it was not a
fresh all-language fixture-generation campaign. Other optional fixture
writer/rebound modes are not claimed as executed merely because their
top-level tests passed without those variables.

The first Java staging attempt stopped before tests because the fresh server
binary was not yet available. The successful full run is `full-service-rerun-r2`.
The tooling unit run reported one pytest configuration warning with plugin
autoload disabled; it had no failed or skipped tests.

Owned server processes were shut down by the runners. Completion records
confirm Windows server shutdown; the Linux runner used an exit trap and its
server PID was absent after completion. Python interoperability subprocesses
exited through the test's cleanup path.

## Evidence and Reproduction

Raw logs, executables and JUnit/Go JSON output remain outside Git:

```text
D:/workspace/gorti/ieee-service-full-rerun-20261004/
  go-reconciled.jsonl
  go-sdk-reconciled.jsonl
  go-reconciled-completion.json
  python-core.xml
  ivct.xml
  linux-race.jsonl
  linux-reconnect-repeat.jsonl
  linux-cpp-interop.xml
  sdkstate-golden.jsonl
  traceability-tooling.xml
  reconciliation.json
  reconciliation.log
  run-linux-race.sh

D:/workspace/gorti/applicable-fixes-20261004/java/full-service-rerun-r2/
  completion.json
  run-config.json
  executed-source-hashes.json
  input-fixture-hashes.json
  maven.log
  work/javasdk/client/target/surefire-reports/
```

Windows server SHA-256:
`14ce07728bfb00fae12fba2c1de84e4451eaa63021f2216f8370379c0c7a2a3f`.

Linux race-enabled server SHA-256:
`b2e0dc7ce0f858f5696f3de30cfc172e531d0d7688229f031f4ce9d33d85b135`.

Core Go commands, with the runner-provided live endpoints, official FDD XSD
and Python interpreter configured:

```sh
go test -json -p 2 -count=1 ./rti/... ./tests/conformance/rti/...
go test -race -json -p 2 -count=1 -timeout 10m ./rti/... ./tests/conformance/rti/...
go test -race -json -p 2 -count=30 -timeout 5m \
  ./rti/internal/application/server \
  -run '^TestFederateFault20261004ReportAndVoluntaryDistinction$'
```

Python selection:

```sh
python -m pytest -c pysdk/pyproject.toml -p no:cacheprovider \
  -p pytest_asyncio.plugin pysdk/tests \
  --ignore-glob='*test_rl_*.py' --ignore-glob='*test_joined_reference_ppo*.py' \
  --ignore=pysdk/tests/test_lint_strict.py -q -ra
```

The Java runner selected `*Test` with `-BroadSuite -SourceDiagnostics` and a
fresh server binary. Its run configuration and source/fixture hashes are in
the evidence directory above.
