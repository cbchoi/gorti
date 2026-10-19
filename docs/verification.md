# Verification and interoperability

Verification connects service requirements, implementation code, executable
assertions, and recorded test results. It has four levels:

1. Unit and integration tests exercise service invariants and generated
   protocol handlers.
2. Cross-language tests compare Go and Python encodings and callbacks. Java
   binding tests exercise its API, transport, and live-server callbacks.
3. Clause-level requirements link to tests and their execution records.
4. Two-federate scenarios compare service results and callbacks with reference
   RTIs.

## Service coverage

Internal tests cover federation, declaration, object, time, ownership, DDM,
synchronization, save/restore, MOM, Support, FOM parsing, and encodings. Live
tests exercise dynamic FOM joins, Float64 and Integer64 execution, timed saves,
callback recovery, restored SDK state, and authenticated session controls.

The cross-RTI scenarios cover FM, DM, OM, and selected TM sequences. Their
canonical logs and performance statistics are separate from the internal
service-test inventory.

## Recorded execution results

The [4 October 2026 full service run](https://github.com/cbchoi/gorti/blob/main/engineering/verification/ieee2010-full-service-rerun-post-sync-20261004.md)
recorded:

| Selection | Passed | Failed | Skipped |
|---|---:|---:|---:|
| Windows Go RTI and conformance tests | 3,349 | 0 | 0 |
| Linux same Go selection with `-race` | 3,349 | 0 | 0 |
| Python SDK/service suite, live server enabled | 2,157 | 0 | 0 |
| Java SDK suite, live and diagnostic cases enabled | 591 | 0 | 0 |
| Internal IVCT-derived subset | 35 | 0 | 0 |

Go counts are top-level tests. Repeated platform runs are not additional unique
tests. The Python service selection excludes legacy C++ interoperability and
RL/PPO work. The report lists additional checkpoint, architecture, and tooling
tests separately.

The [8 October callback-report optimization](https://github.com/cbchoi/gorti/blob/main/engineering/design/consistent-performance-plan-20261007.md)
passed all 978 required IEEE-service cases in each Go reporting build, including
the selected live checkpoint cases. The full Python, Java, and Linux race
campaign was not repeated for that change. Each report identifies the source,
build, configuration, and execution artifacts to which its result applies.

## Requirements traceability

The [requirements inventory](https://github.com/cbchoi/gorti/tree/main/engineering/verification/requirements2010)
records the standard and clause, applicability, implementation locations, test
names, and expected behavior. The 4 October reconciliation linked all 639
applicable records to passing executions across 877 test links. Of 712 total
records, 48 were not applicable and 25 remained unresolved.

This is internal verification evidence. The remaining source interpretations
and independent assessment are tracked separately; formal certification has not
been obtained. The [Software Test Description](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/STD.md)
defines test selection and acceptance criteria.

## Canonical logs

Each implementation writes a detailed raw log. A validator projects it to the
same FM, DM, OM, and TM records only after checking required calls, callbacks,
payloads, synchronization points, and logical times. Semantic equality is a
full canonical-record comparison, not an event-count comparison.

Reference RTI evidence consists of standard API returns, callbacks, and
runtime records. gorti's event journal is optional and enabled only for
profiles that require replay evidence. Performance profiles record summaries
without per-message diagnostic logging.

## Run the release checks

Run the local implementation checks first:

```text
go test ./...
python -m pytest pysdk/tests verification/common verification/gorti
mvn -f javasdk/pom.xml verify
```

The reproducible Portico/gorti comparison is launched from PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass `
  -File verification/portico/RunComparison.ps1
```

The comparison defaults to five warm-up pairs and thirty measured pairs with
alternating AB/BA order. The October callback-reporting runner also supports
ABBA/BAAB crossover blocks. See [Reproducibility](reproducibility.md) for both
designs and [Performance](performance.md) for the recorded results.

Live SDK tests need their documented endpoint and fixture settings. Use the
commands and environment recorded in the full-run report when reproducing its
counts; the default test commands alone do not enable every optional live case.
