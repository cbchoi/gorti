# IEEE Service Full Rerun After Reconnect Test Synchronization

Date: 2026-10-04

## Result

All executed tests in the selected Go/Python/Java IEEE service campaign
passed. There were no failed or skipped test cases, and neither Linux race
run reported a data race. The corrected reconnect test passed within the
full server suite, not only in its earlier focused repeat.

This is a fresh execution after the test-only synchronization correction.
The earlier full-run failure and diagnostic probes remain separate evidence.
No production or test source was changed during this rerun.

## Counts

Go counts are top-level tests; nested cases are shown separately. Repeated
platform runs and fixture-mode invocations are not additional unique tests.

| Execution | Passed | Failed | Skipped | Detail |
|---|---:|---:|---:|---|
| Windows Go RTI and conformance packages | 3,349 | 0 | 0 | 72 packages; 6,698 nested passes |
| Linux Go RTI and conformance packages, `-race` | 3,349 | 0 | 0 | 72 packages; 6,698 nested passes |
| Windows Go SDK actual-server checkpoint tests | 2 | 0 | 0 | 4 nested passes; Windows-only build tag enabled |
| Python SDK/service suite | 2,157 | 0 | 0 | Live-server tests enabled; C++ legacy and RL/PPO excluded before collection |
| Java SDK complete `*Test` selection | 591 | 0 | 0 | Live server, source diagnostics and checkpoint fixture readers enabled |
| Executable IVCT-derived subset | 35 | 0 | 0 | Internal subset |
| Windows Go standard vectors, architecture and examples | 14 | 0 | 0 | 5 packages; 107 nested passes |
| Linux same supplemental Go packages, `-race` | 14 | 0 | 0 | 5 packages; 107 nested passes |
| IEEE traceability/tooling unit tests | 133 | 0 | 0 | Separate from service behavior; 112 additional pytest subtests passed |

The tooling unit run emitted one pytest configuration warning with automatic
plugin loading disabled. It did not fail or skip tests.

## Explicit Fixture Modes

The supplemental checkpoint campaign enabled optional environment-controlled
test paths instead of counting their default early-return behavior as fixture
exchange coverage:

- Current Go SDK writers: 2 top-level tests and 4 nested cases passed,
  producing base and version-2 recovery fixtures for both time representations.
- Current Python codec helpers generated and checked base, recovery and local
  notification fixtures for both time representations.
- Server SDK-state adapter exchange: 4 top-level tests and 16 nested cases
  passed using the Go/Python outputs and copied Java base/local/recovery inputs.
- Current Go SDK readers: 2 top-level tests and 4 nested cases passed after
  the adapter rebound the generated state to a new federation generation.
- Python independently decoded all 6 corresponding rebound files, preserving
  exact times, object knowledge, retraction state, known callback outcomes and
  local notification identities as applicable.

Java's broad suite used the existing base/local checkpoint goldens and its
configured version-2 fixtures. This does not claim a new all-language fixture
generation campaign. Fixture-mode runs overlap the main test inventory and
are reported separately rather than added to its unique test count.

## Requirement Execution Reconciliation

The supplied source records and current requirement files were reconciled
against only this campaign's execution artifacts:

- 712 requirement records: 639 applicable, 48 not applicable, 25 unresolved.
- All 639 applicable records have test links; no applicable record is unlinked.
- All 877 test links have passing execution records only.
- No linked test has a failed/skipped variant or missing execution record in
  this campaign.
- The earlier nonpassing links for `FM2010-FAULT-REPORT` and
  `FM2010-SAVE-FAILURE-FAULT` now have passing records in both full Go runs.

Execution reconciliation is not an independent standards-coverage assessment.
The review tool retains `coverage_complete=false` and `full_conformance=false`;
this report establishes the result of the prepared tests, not certification.

## Scope and Commands

The agreed completion scope is Go/Python product interfaces and the selected
Java standard mapping. The C++ legacy interoperability test is explicitly
outside this campaign, not silently skipped or reported as passing. RL/PPO
tests, general Python lint, performance/soak runs, optional DDS build tags and
a full C++ campaign were not selected.

Go test-result caching was disabled with `-count=1`:

```sh
go test -json -p 2 -count=1 ./rti/... ./tests/conformance/rti/...
go test -race -json -p 2 -count=1 -timeout 10m ./rti/... ./tests/conformance/rti/...
go test -json -p 2 -count=1 -timeout 5m ./tests/spec/... ./tests/architecture/... ./examples/...
go test -race -json -p 2 -count=1 -timeout 5m ./tests/spec/... ./tests/architecture/... ./examples/...
```

Windows additionally executed `TestGoSDK.*ActualServer` in `rti/pkg/federate`
with `-tags=runtime_checkpoint_live`. These tests are Windows-only; no empty
Linux selection is counted as their execution. Live DDM endpoint and official
FDD schema/interpreter settings were supplied to the full Go campaigns.

Python selection, with live RTID and explicit SDK/schema paths configured:

```sh
python -m pytest -c pysdk/pyproject.toml -p no:cacheprovider \
  -p pytest_asyncio.plugin pysdk/tests \
  --ignore-glob='*test_rl_*.py' --ignore-glob='*test_joined_reference_ppo*.py' \
  --ignore=pysdk/tests/test_lint_strict.py \
  --ignore=pysdk/tests/spec/m17/test_cpp_python_interop.py -q -ra
```

The existing Java runner selected `*Test` with `-BroadSuite -SourceDiagnostics`.
It built the reactor in a fresh external work directory and used a freshly
built server, official Java API archive, FDD schema and Python peer. The MIM
source diagnostic and live Support-name exception tests ran and passed.

## Provenance and Evidence

The worktree was based on
`a9d9932fda3d1bd7018dec2434d94a2bda9cd2bf`, branch
`codex/ieee2010-feature-completion`. The Windows executable records that
revision and `vcs.modified=true`; the commit alone does not identify all
worktree changes. Java records include executed source and input fixture
hashes. Existing local changes were preserved.

Server SHA-256 values:

- Windows Go 1.26.5: `f97499a7b83a90727ccb8e6eaa33e9f11555ab21a50f821ab008e6214f667a3c`.
- Linux race-enabled: `936d377e59c9928b4ee5eb4c26af2316e779caf8604c84addd4bb727a80cf681`.

Raw logs, fixtures, executables and execution JSON/JUnit files remain outside
the repository:

```text
D:/workspace/gorti/ieee-service-full-rerun-post-sync-20261004/
  go-reconciled.jsonl
  go-sdk-reconciled.jsonl
  go-reconciled-completion.json
  linux-race.jsonl
  linux-completion.json
  go-supplemental.jsonl
  linux-supplemental-race.jsonl
  python-core.xml
  ivct.xml
  traceability-tooling.xml
  go-fixture-emit.jsonl
  sdkstate-fixture-exchange.jsonl
  go-fixture-readback.jsonl
  python-fixture-readback.log
  reconciliation.json
  reconciliation.log
  fixtures/

D:/workspace/gorti/applicable-fixes-20261004/java/full-service-post-sync/
  completion.json
  run-config.json
  executed-source-hashes.json
  input-fixture-hashes.json
  maven.log
  work/javasdk/client/target/surefire-reports/
```

All commands completed. Owned Go/Java server shutdown was recorded by the
runners and their server PIDs were absent on the final check. Python tests
used their existing owned-process cleanup. No commit or push was performed.
