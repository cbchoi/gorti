# IEEE 1516-2010 Completion Check

Date: 2026-10-02. Verdict: **internal completion not passed; full conformance not established**.

This round checked previously unverified service boundaries against the supplied
2010 redlines. Six parallel workstreams covered federation management,
declaration/object management, DDM, MOM, logical time and applicability. A separate
FOM-admission check exercised the public Python ambassador against the production
server. No production implementation was changed in this round. The retained
failing tests are reproduction cases, not completed fixes.

## Reproduced Defects

| Family | Observation | Responsibility and source |
| --- | --- | --- |
| Disconnected creation | `createFederationExecution` returns success and caches federation inputs without a connection. No server request occurs. | Python facade, `standard.py:304-306`; S1516.1 4.5.3(a), 4.5.5(h), PDF page 53. |
| Nonmember calls | Connected but unjoined sync/save/restore calls raise generic `RuntimeError`, not the available `FederateNotExecutionMember`. Roster preservation, later join and actual synchronization controls pass. | Python facade `_fed()` at `standard.py:1861`; 4.11.5(c), 4.16.5(f), 4.24.5(c), PDF pages 59, 65, 73. |
| Duplicate-name join and retry | A different unjoined caller using an occupied name receives the already-member classification. Python wraps that definite rejection as commit-unknown and poisons the connection, blocking a unique-name retry. | Federation manager `manager.go:547-549`, gRPC exception mapping and Python `connection.py:747`; 4.9.3(d), 4.9.5(b), PDF page 56. Original owner remains usable; entrant retry recovery is not demonstrated. |
| Pending deletion equality | A TSO update at the same timestamp as a pending TSO deletion is rejected in both Float64 and Integer64, under the tested regulation/ownership conditions. | Object `update.go:660` uses `>=`; 6.10.3(i), 6.14.4(c)(1), PDF pages 125, 132. This is not permission to update after committed deletion. |
| Pending deletion error | An update later than the pending deletion returns raw gRPC `INTERNAL`, not the logical-time rejection. Rejection-state preservation and a valid earlier update retry pass. | Object error and adapter mapping; same clauses as above. Both scalar families reproduce the error. |
| Noncompliant FOM names | Creation accepts user classes `1Sensor`, `Sensor:Bad`, `Sensor Bad`, `HLAcustom` and `NA`. Independently generated name-valid control creation/destruction passes. | FOM parser/admission; S1516.2 3.3.1, PDF pages 31-32, and S1516.1 4.1.4/4.1.4.1, pages 47-48. Those interface clauses require rejecting noncompliant modules. |

The Python tests check abstract service behavior and the project's public exception
mapping. IEEE 1516-2010 does not define a Python language binding; these tests do
not constitute standardized Python-binding certification. Server defects and
SDK-local failures are distinguished above.

The naming rules and FOM-rejection clauses were visually inspected in the supplied
redlines. Reserved predefined MIM names are legitimate; a fix must distinguish
them from new user-defined names. The current negative tests cover object-class
creation only, not all identifier categories, case variants, join-time extension
or whole-module rejection atomicity. The positive control is an ordinary-name
admission control, not an independent certification of its complete FOM content.

## Fresh Execution

Only final selected runs are counted here. Earlier diagnostic attempts remain
outside Git, including the MOM test's compilation error and a naming-probe
Windows-path setup error; neither is represented as a semantic pass. The original
FM restore case did not reach restore admission: automatic-callback checkpoint
timing interrupted the save prelude, while cleanup obscured the primary error.
The callback signatures were already correct in the executed source snapshots.
After correcting only the test helper's cleanup and using evoked delivery to
finish callback bodies before checkpoint,
the selected rerun reaches all three nonmember calls and their poststate controls.
The earlier restore result is not used as restore-rejection evidence. Automatic
callback checkpoint timing remains a separate, unqualified diagnostic observation.

| Scope | Execution boundary | Passed | Failed | Errors / skips |
| --- | --- | ---: | ---: | ---: |
| FM | Public Python facade, evoked callbacks and real server; six parameter cases | 1 | 5 | 0 / 0 |
| DM/OM | Async Python SDK and real server; eight parameter cases | 4 | 4 | 0 / 0 |
| FOM names | Public Python facade and real server; six parameter cases | 1 | 5 | 0 / 0 |
| Time endpoints | Official Java API facade and real server; three parameter cases | 3 | 0 | 0 / 0 |
| DDM | Go gRPC client and real server; three top-level tests | 3 | 0 | 0 / 0 |
| MOM | In-process production components; one top-level Go test | 1 | 0 | 0 / 0 |

For these counting units: **27 cases/top-level tests, 13 passed and 14 failed**.
Ten additional Go subtests pass (eight DDM, two MOM); they are not added again to
the top-level total. This small heterogeneous total is a run summary, not a
coverage percentage or a comparison between language bindings.

Passing oracles include receiver-specific RO/TSO metadata and reflection before
grant, below-deletion update admission, save guards and rejected-label reuse,
finite Float64 endpoint/predecessor behavior, exact large Integer64 endpoints,
last-setter-wins region bounds, committed-query visibility, mixed-commit rejection
and recovery, subscription-protected region deletion, and authenticated MOM
callback-entry counters. Receipt/completion alone is not counted as successful
user callback entry or a reflected object.

Server identity for live checks:

```text
D:/workspace/gorti/rtid-defect-final-20261002.exe
SHA256 24b9c82590543159e6234c8dc7d473b6ee1699329c34677d1779035aad115b05
```

The server was not rebuilt during this round. SDK/test source correspondence is
recorded by the individual runs. Binary identity alone is not proof that every
dirty production source file currently matches the binary. MOM tests execute
current components in-process instead. Owned server/JVM processes were stopped.

### Evidence Locations

Raw execution remains outside the repository:

| Scope | Artifact under `D:/workspace/gorti/` |
| --- | --- |
| FM | `completion-check-fm-20261002/corrected-final/evoked/results.xml`, `pytest.log`, `evidence-manifest.json`; earlier `final/` is superseded |
| DM/OM | `completion-check-dm-om-20261002/run-150416/results.xml`, `pytest.log`, per-case boundary observations |
| FOM names | `completion-check-names-20261002/repository-results.xml`, generated FOMs, per-server `process.json`; diagnostic `final-results.xml` is a separate run |
| Java time | `completion-check-time-boundary-20261002/work/javasdk/client/target/surefire-reports/TEST-org.gorti.rti.LiveTimeCompletionBoundaryTest.xml`, `focused-maven.log`, `source-correspondence-summary.json` |
| DDM | `completion-check-ddm-bounds-20261002/final-go-test.jsonl`, `evidence.json` |
| MOM | `completion-check-mom-20261002/focused-go-02.jsonl`, `evidence.json` |

New literal declarations are in:

- `pysdk/tests/test_ieee2010_fm_completion_live.py`
- `pysdk/tests/test_ieee2010_order_completion_live.py`
- `pysdk/tests/test_ieee2010_fom_name_completion_live.py`
- `javasdk/client/src/test/java/org/gorti/rti/LiveTimeCompletionBoundaryTest.java`
- `rti/internal/hla/ddm/completion_bounds_test.go`
- `rti/internal/hla/mom/completion_counter_test.go`

Python reproduction uses `GORTI_RTID`, the SHA256 pin above,
`GORTI_INDEPENDENT_EVIDENCE` outside Git, `GORTI_RUN_LIVE_E2E=1`, and the same
schema-control/SDK `PYTHONPATH` as the preceding assessment. Run the three named
Python files with pytest, explicit asyncio plugin and an outside-Git JUnit path.
The DDM tests require `GORTI_COMPLETION_DDM_ENDPOINT` pointing to the owned server;
without it they skip, which is not completion evidence. Java uses the offline
Maven cache and an outside-Git build copy recorded in its execution log.

## Requirement Reconciliation

[Updated machine-readable record](ieee1516-2010-completion-check-20261002.json)
combines fresh final artifacts with explicitly identified historical evidence.
The preceding [whole-source review](ieee1516-2010-requirements-review-20261002.md)
and its JSON are preserved as the earlier checkpoint, not overwritten.

- 793 source pages; 1,856 candidate review units; 210 declared direct-body decompositions.
- 712 requirement records: 706 source-reviewed, six candidate; 639 applicable, 48 excluded, 25 unresolved.
- 347 oracle links: 331 have passing records with invocation completeness unassessed; 16 retain nonpassing variants.
- 472 records have no linked oracle; 867 overlapping open notes remain.
- `coverage_complete:false`, `full_conformance:false`; applicable semantic verdicts remain open.

Oracle links are not unique test cases, atomic obligations or verified clauses.
A failing parameter variant prevents crediting the linked literal method; the
16 nonpassing links are not 16 independent defects. Even a passing link does not
close its remaining source conditions, state transitions or exception facets.
The reconciliation tool now accepts supplemental Go JSON without hiding a fresh
failure behind a historical pass. Its supporting Python test suite passes 127
tests; these are evidence-tool tests, not additional HLA conformance results.

## Scope Review and Remaining Proof

The [independent scope review](ieee1516-2010-completion-scope-review-20261002.md)
assesses all 25 unresolved-applicability records. It proposes 13 user-federate
error conditions, seven definite RTI responsibilities and five unresolved
questions. These dispositions are not automatically applied to the slices.
Callback delivery, error handling and state transitions remain RTI obligations
even when the error itself originates in user callback code.

Outstanding proof includes complete statechart transitions and forbidden calls,
all callback branches and exact error-cause behavior through both Java evoke
methods, throwing `connectionLost` with teardown/reconnection, source/API
ambiguities, valid custom-time-provider admission where applicable, all FDD
identifier and merge conditions, source-table precedence, and errata reconciliation.
Table/schema/API decomposition is still incomplete. Independent external
assessment is additional work, not the only remaining item.

## Next Implementation Order

1. Correct duplicate-name classification and definite-rejection handling, preserving uncertain-commit protection for genuinely indeterminate failures; prove same-connection retry.
2. Correct the pending-deletion timestamp boundary and typed rejection; retain committed-deletion guards and prove fanout/state nonmutation for both scalar families.
3. Correct Python connection/member guards; prove no local cache mutation after rejected disconnected creation and exact public exception mapping.
4. Add origin-aware FDD name validation with predefined-MIM and valid Unicode controls; cover creation and dynamic join without partially extending the FDD.
5. Turn the remaining source dispositions into atomic obligations and executable callback/statechart oracles, then repeat targeted and broad regressions on a newly built server.

The next cycle should use these retained red tests as the TDD baseline. Earlier
broad green runs remain valid for their tested scope; they never covered all the
new boundaries. They do not override the failures found here.
