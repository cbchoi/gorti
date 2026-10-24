# IEEE 1516-2010 test-case design and recorded results

Updated 24 October 2026. This guide describes reusable test cases for conformance
assessment and regression testing. Its contribution is the translation of reviewed
requirements into executable assertions, with a traceable connection to results.
It is not a whole-standard conformance certificate.

## Source and execution identity

The assessed RTI/SDK commit is `4b11da478f423c0776f6ba6ae580f1670b61f2a9`
on `codex/go-python-java-validation`. The SDK campaign started from source tree
`2353c684b2668e2e210abdacb1606e7f47297e14`; final Go and lint validation used
`61c7f5fa0b1e1012ae11b2df498a22fb8655d748`. The intervening changes were three
line-specific analyzer comments, without executable-statement changes. The
publication commit also records the validation report.

The 23 October model example, aggregation exporter and classification manifest,
and this 24 October guide are source additions, not files in that base commit.
They do not change the assessed server, SDKs, or linked service-test sources.

The [Linux campaign](linux-publication-validation-20261019.md) used Ubuntu 24.04
under WSL 2, Go 1.22.2, Python 3.12.3, Temurin JDK 11.0.32.1, Maven 3.9.16,
Buf 1.71.0, and golangci-lint 1.61.0. Source was on the native Linux filesystem.

| Execution scope | Passed | Skipped | Counting unit |
|---|---:|---:|---|
| Go default build with race detector | 3,533 | 19 | Top-level tests; 7,020 passing subtests in 80 packages |
| Go asynchronous build with race detector | 972 | 0 | Top-level tests; 2,357 passing subtests in three packages |
| Python SDK and selected HLA services | 2,127 | 0 | Reported tests |
| Java SDK and live services | 581 | 7 | Reported invocations |
| Internal IVCT-derived subset | 35 | 0 | Reported cases |

No failures or errors occurred in these final suites. Overlapping suites,
subtests and repeated runs are not additional independent requirements.
Sixteen default Go skips are asynchronous-only cases exercised in the tagged
suite; three require FDD-schema resources. Java skips comprise one external
IEEE API archive comparison, three FDD-schema checks, and three cross-binding
fixtures. Static/protocol checks, the strict documentation build and ten repeated
determinism runs also passed. These are local results, not hosted CI results.

## From requirements to tests

Records in [requirements2010](requirements2010/) identify the standard and
clause, applicability, implementation paths, named tests, and an oracle stating
the input conditions and expected observation. An oracle can address acceptance,
rejection, callback metadata/order, state changes, or recovery. One passing case
need not cover every condition in a requirement; open items retain that distinction.

The [23 October reconciliation](ieee1516-2010-execution-reconciliation-20261023.md)
matched all 205 linked test-source files to the retained campaign source byte
for byte. One reporting-guard result was added by a focused execution. The
service suites were not rerun wholesale on that date.

| Review group | Passing linked invocations only | With skips | Not applicable | Unresolved |
|---|---:|---:|---:|---:|
| Framework | 5 | 0 | 11 | 0 |
| Federation, synchronization, save/restore | 355 | 0 | 10 | 16 |
| Declaration and object | 88 | 0 | 3 | 1 |
| Ownership and DDM | 26 | 0 | 2 | 1 |
| Time and mapping | 31 | 0 | 0 | 2 |
| Support and MOM | 43 | 0 | 2 | 2 |
| Java annex | 62 | 3 | 2 | 2 |
| Object model template | 25 | 1 | 18 | 1 |
| Total | 635 | 4 | 48 | 25 |

Of 712 reviewed records, 639 are applicable. No applicable record lacks linked
results and no linked invocation failed. Of 877 requirement-test links, 870
contain only passing imported invocations and seven contain skips; one skipped
link belongs to a not-applicable record. The four applicable records with skips:

- `JAVA2010-API-EXACT-PACKAGE`
- `JAVA2010-FDD-SCHEMA-VALIDITY`
- `JAVA2010-VALID-FDD-ADMISSION`
- `OMTSLICE2010-DIF-VALID-XML`

These require an external IEEE API archive or FDD-schema resource not enabled in
the campaign. The 25 unresolved records retain source-interpretation questions,
callback-exception responsibilities, custom-time conditions, and state-chart or
table decomposition. The status describes linked executions, not a percentage
of the standard proved.

## Evidence classes

The [classification manifest](requirements2010-evidence-classes-20261023.json)
assigns one primary class to each path/name target, with a rationale and
source/oracle hashes. It was reviewed from recorded oracles and target inventories,
with source inspection for ambiguous boundaries. The exporter rejects missing,
duplicate or changed classifications.

| Class | Links | Named targets | Passing links | Links with skips |
|---|---:|---:|---:|---:|
| Service/SDK behavior | 766 | 388 | 762 | 4 |
| API/structure | 110 | 75 | 107 | 3 |
| Documentation/reporting | 1 | 1 | 1 | 0 |
| Total | 877 | 464 | 870 | 7 |

Behavior includes controlled unit/SDK tests as well as live service tests.
API/structure includes interface shape, encodings, time/handle helpers and
FOM/MIM/XML parsing. The reporting guard prevents unsupported certification claims
by the report tool; it is not an RTI service. MOM reporting remains a service case.

The 639 applicable records have mutually exclusive profiles: 562 behavioral-only
(all passing), 62 API/structure-only (60 passing, two with skips), 14 mixed
(12 passing, two with skips), and one reporting-only (passing).

## Representative cases

Java classes below are in `javasdk/client/src/test/java/org/gorti/rti/`.
All named invocations in these five cases passed in the retained Linux reports.

| Record; IEEE 1516.1-2010 clause | Test target | Assertion |
|---|---|---|
| `MAP-LANGUAGE-TRANSPARENCY`; 12.1 | `JavaLanguageTransparency20261004Test.simultaneousJavaAndPythonParticipantsExchangeOpaqueInteractionBytesThroughPublicApis` | Two joined Java/Python peers exchange exact parameter bytes, tags, receive order, reliable transport, producer identity and evoked dispatch. |
| `TM-LOOKAHEAD-SEND-FLOOR`; 8.1.4 | `LiveTimeArithmeticTest.sendGaltAndRetractionUseTheSameRepresentableBound` | For time 1 with `Double.MIN_VALUE` lookahead, and time 2^53 with lookahead 1, reject the current timestamp and accept the next representable bound. Additional service/Integer64 conditions remain open. |
| `FM2010-SAVE-INITIATION-TEMPORAL-GUARD`; 4.1.1, 4.17 | `LiveSaveRestoreTest.timedSaveDefersGrantsAndRestoreResumesTheSavedAdvance` | No premature save/grant; initiate at time 2, retain pending grants, reject with `SaveInProgress`, grant to 3 after save, resume saved advance after restore. |
| `OM2010-DELETE-KNOWLEDGE-PRIVILEGE`; 6.14.1-6.14.5 | `pysdk/tests/test_ieee2010_independent_contract_live.py::test_delete_unknown_precedes_privilege_and_preserves_object` | Exact unknown-object/privilege exceptions, no mutation after rejection, later owner update, tagged removal and rejection of repeated deletion. |
| `DDM2010-DELETE-GATES`; 9.4 | `LiveDdmTest.regionsRouteByAttributePairsAndCommittedBoundsAcrossThreeSessions` | Reject associated-region deletion; permit deletion after moving associations; reject subsequent queries with `InvalidRegion`. |

The Java language peer is `javasdk/client/src/test/resources/java-language-peer-20261004.py`.
It receives bytes `00 80 FF 7F` with tag `FE 00 21`, and replies with
`7F FF 80 00` and the empty tag specified by its untimed facade. The record includes
`JAVA_TO_PYTHON_ASSERTIONS_PASSED` alongside the Java JUnit result.

The Go/Python cases are `pysdk/tests/spec/m21/test_time_client.py` targets
`test_209_2_go_regulator_python_constrained`,
`test_209_3_python_regulator_go_constrained`, and
`test_209_4_mixed_primitives_cross_language`. The shared helper
`_processes.py::assert_cross_language_delivery` checks distinct process IDs and
federate handles: no event while a regulating bound prevents progress, then
`Tick(0x10203040)` at time 4 followed by the grant to 5. The mixed case covers
TAR, TARA, NMR, NMRA, and FQR with controlled peer bounds.

The adapter case
`pysdk/tests/spec/m21/test_time_service_cross_language.py::test_ac_3_8_pyjevsim_time_advance_runs`
uses the existing `GrantDrivenModel(3.0)` beside two regulating Python peers;
ten iterations assert time 3k, k cycles, and k outputs. All these cases passed.

## Reproduce the tables and model example

The external reproduction archive supplies `traceability/reconciled-input.json`,
the class manifest and original reports. In a fresh extraction, run:

```text
python reproduce_tables.py --output ../gorti-tables
```

The destination must not exist. Python 3.11+ and its standard library suffice;
no Git, network access, private path or IEEE PDF is needed. The command verifies
packaged hashes and 74 linked execution-report hashes, then regenerates the
summary and two CSV tables byte for byte. It reproduces aggregation of reviewed
metadata, not a new standard interpretation or new RTI execution. Optional source
checking uses the assessed checkout and the exporter's `--source-root` argument.
Seven focused exporter tests exercise classifications, mixed profiles, skips,
missing/failed results and hash rejection; they are not HLA service tests.

For the model experiment, use release `1.0.0` or the current checkout, which
already includes the source additions, and follow the
[Python-Go example](../../examples/multilanguage-queue/README.md).
Install `./pysdk[dev,pyjevsim]`, generate bindings with
`python -m rti1516e._proto`, and run:

```text
python examples/multilanguage-queue/run.py --output <new-external-directory> --jobs 100 --repeats 3 --seed 1516
```

Go 1.22+ and Python 3.11+ are required. The SDK extra pins pyjevsim 2.1.2 source
`9893099b47aae89a3432c668b18ec4b6b15c043e`. When WSL reads a Windows-managed
worktree, pass `--source-commit` with the full identifier verified by Windows Git;
the manifest labels it caller-supplied and retains file hashes.

The final model campaign used Ubuntu 24.04/WSL 2, Go 1.22.2 and Python 3.12.3,
with the repository accessed through the Windows mount. Nine executions of 100
jobs passed: final departures 102, 202 and 206; mean waits 0, 49.5 and 49.85;
mean sojourns 1, 51.5 and 51.89; zero logical-time trajectory error for service
durations 1, 2 and seeded 1-3 respectively. The 900 jobs generated 1,800 data
callbacks. These are functional results, not wall-clock performance measurements.

Each trial retains inputs, reference/observed trajectories, checks and process
identities. Preparation failures (missing generated bindings, example class-name
handling, and Windows-worktree Git metadata under Linux) remain separate; the
complete final campaign succeeded after setup/example corrections. No server or
SDK behavior changed, and no failed run was counted in those nine executions.

## Educational use

Use the [SRS](../specifications/current/SRS.md) to identify behavior, the
[SDD](../specifications/current/SDD.md) for state ownership and service structure,
the [IDD](../specifications/current/IDD.md) for interfaces, and the
[STD](../specifications/current/STD.md) for test design. Suggested exercises:

1. Change the workstation service profile and explain the resulting queue growth.
2. Trace a time-management requirement through `rti/internal/hla/time` and its
   callback assertions, separating domain behavior from delivery and gRPC code.
3. Modify an algorithm in the responsible service, add a case expressing the
   intended difference, and run affected service and cross-language checks.

These are supported uses of the artifacts, not measured classroom outcomes.
Raw execution logs, licensed IEEE PDFs and extracted standard pages remain
outside Git. Source, test definitions, classifications and maintained result
summaries are version-controlled.
