# IEEE 1516-2010 Full Validation Run

Date: 2026-10-03. **Overall verdict: NOT PASSED.**

This run executes the available Go, Python HLA/SDK, Java, independent live and
IVCT-inspired checks, then examines their requirement coverage. It extends the
[defect-correction campaign](ieee1516-2010-defect-fixes-20261003.md): that campaign's
passing targeted results did not cover every separate legacy conformance suite.
No production code or test oracle was changed in this run.

## Fresh Execution

The Windows server was rebuilt from current sources:

```text
D:/workspace/gorti/full-consistency-20261003-r1/rtid.exe
SHA256 fc2bd04a5e64970e2dd0f6cb37f98741a257a578e273cae6458d4fd9602d75e8
```

Its hash matches the preceding corrected server. Go, Python and Java campaigns
record their own configuration, source correspondence and process cleanup.
Raw logs, generated fixtures, binaries and external API builds stay outside Git.

| Scope | Passed | Failed | Errors | Skipped |
| --- | ---: | ---: | ---: | ---: |
| Windows Go: RTI, tests and Go examples | 3,258 top-level | 0 | 0 | 0 |
| Linux full Go race, initial campaign | 3,257 top-level | 1 | 0 | 0 |
| Separate Linux full Go race retry | 3,258 top-level | 0 | 0 | 0 |
| Windows tagged Go runtime/control live tests | 1 top-level | 1 | 0 | 0 |
| Separate tagged cross-generation retry | 0 top-level | 1 | 0 | 0 |
| Python HLA/SDK: 139 files | 2,068 | 0 | 0 | 1 |
| Java full initial campaign: 57 suites | 537 | 3 | 0 | 0 |
| Separate Java restart-suite retry | 6 | 0 | 0 | 0 |
| Independent live contracts and defect boundaries | 26 | 0 | 0 | 0 |
| Independent support/lifecycle regressions | 2 | 0 | 0 | 0 |
| IVCT-inspired local subset | 30 | 5 | 0 | 0 |
| Evidence-tool tests | 127 top-level | 0 | 0 | 0 |

These scopes overlap; their counts must not be summed into a unique-test total.
Windows Go additionally has 6,531 nested passing records. Evidence-tool tests
have 112 passing subtest records and one warning. Neither is added to its
top-level count.

The broad Go runs use default build tags. Windows `runtime_checkpoint_live`
cases are subsequently executed separately, as recorded below. Four optional generated-SDK fixture-exchange tests
return early without their input environment variables; their passing events
do not establish those exchanges. The separate Java campaign records its own
fixture scope. Ten Go packages have no test files, rather than skipped cases.

The first Linux race run reports no data-race warnings. Its sole failure,
`TestIntegration_SpawnRtidAndRenderEmptySnapshot`, occurs when its internal
server build exceeds a two-minute deadline, before snapshot assertions run.
Cold-cache cost is suspected but not isolated. A separate full retry passes
3,258 top-level and 6,531 nested records without data-race warnings. The
unchanged failed test passes in 36.44 seconds, versus its initial 120.18-second
build failure. Both executions use WSL Ubuntu 24.04, Go 1.22.2 and the same
Linux race-server SHA256
`5d3839fe450c5adb2a07ad416e3bf63225f1d0e99a823e43e56667942d782fb1`.
The retry cannot replace the initial failed execution.

All owned campaign servers were stopped. No production, SDK or test source
was changed; this run adds only reports and outside-repository launch artifacts.

The supported-profile manifest also executed successfully: **226 groups,
985 required declaration references, 978 unique package/test pairs**. Its
passing result is bounded engineering evidence, not full-standard coverage.

## Failed Executions

### Go Cross-Generation Runtime Live Test

The additional `runtime_checkpoint_live` run uses the same fresh binary bytes.
The control-outcome test requires a historical executable basename, so an
outside-repository copy is named `rtid-control-outcomes-final-20261001.exe`;
its hash is pinned to the current server, not a historical build.

`TestGoSDKControlOutcomesActualServer` passes both Float64 and Integer64
subcases. `TestGoSDKCrossGenerationRuntimeActualServer` fails both subcases at
`runtime_checkpoint_live_test.go:264`: its final empty-queue assertion receives
`federate.InitiateFederateSave`. A separate unchanged-source repeat reproduces
both failures. Earlier restored payload/time/identity and dispatch assertions
reach that boundary. An unexpected remaining callback is demonstrated;
whether it is incorrect replay or an inadequately acknowledged control in
the test requires further isolation. It is not waived as an environment issue
or silently counted passed.

### IVCT-Inspired Subset

All 35 existing cases were run with their assertions unchanged. The original
fixture hardcodes `bin/rtid`; an outside-Git launch plugin changes only that
binary selection to the fresh server. Current generated protocol classes were
preloaded. This is the repository's Python-native subset, not the official IVCT
framework or an RTI certification campaign.

Five cases fail:

- `test_tc001_create_join_resign_happy_path`
- `test_tc001_double_create_raises`
- `test_tc001_destroy_with_joined_federates_raises`
- `test_tc015_plain_acquire_requests_release_from_owner`
- `test_tc002_duplicate_label_registration_fails`

The three lifecycle failures reach raw `DestroyFederation` without the current
required federation-generation field. The ownership case reaches a pending
acquisition during teardown and receives `OwnershipAcquisitionPending`.
The synchronization case expects an RPC exception for a duplicate label,
whereas the current service reports registration failure by callback.
These observations require correcting or reconciling the legacy helpers and
oracles; they are not automatically five proven IEEE implementation deviations.
They still fail this executed suite and are not waived or relabeled as passes.

An initial command-line syntax mistake did not start any IVCT test. The selected
35-case result above is the subsequent actual execution, retained separately.

### Java Restart And Fresh-JVM Cases

The initial Java campaign has three child-deadline failures in
`LiveControlOutcomeRestoreTest.pendingControlAndKnownOutcomeSurviveServerRestartAndFreshJvm`:

| Parameter invocation | Observation at deadline |
| --- | --- |
| `[1]`, Float64/success | Restore child last recorded `joined`. |
| `[2]`, Float64/failure | Save child printed `SAVE_OK` but did not exit within its budget. |
| `[3]`, Integer64/success | Restore child last recorded `start`, before `joined`. |

The shared live endpoint passes all 66 invocations. The six restart/fresh-JVM
invocations initially split three pass/three fail; 462 other unit/in-process
invocations and six historical fixture exchanges pass. Encoding contributes
86 passing invocations within those totals.

A separate restart-suite retry passes all six with the same sources, assertions
and 55-second child limits. This establishes that the failure did not reproduce
in that retry, not that its cause is fixed. Initial failures remain primary
campaign evidence. CPU/startup load or shutdown behavior is not a proven cause.

## Python Inventory And Exclusions

The fresh campaign executes all 106 files selected by the checked-in HLA
inventory plus 33 newer HLA/SDK files. All 2,069 collected cases have execution
records; none is deselected. The old expected-node manifest has four stale IDs
and omits 597 newly collected IDs. It therefore needs maintenance even though
actual collection and execution agree.

Forty-six files remain outside this RTI behavioral campaign: 39 RL application,
six bridge application and one static-quality file. Their case counts were not
collected and they are not claimed passed.

The sole C++ interoperability case skips because its publisher is not built.
An outside build attempt finds MSVC/CMake but fails to find Protobuf libraries
and headers; no dependency download or repository modification was made. Java
is the selected standard binding, so this is not evidence that C++ is required
for that binding claim. The selected Python campaign therefore still does not
pass a no-skip criterion.

## External Java API

Comparison against the separately compiled official API finds matching
public/protected signatures for 204 classes; 202 source files are byte-identical.
Two discovery-helper adaptations are explicitly distinguished. Original
discovery helpers reproduce Java 11 ImageIO-SPI compatibility rejections;
direct loading passes 50 checks and adapted discovery passes 54. Native modern-JDK
discovery with unchanged original helpers is not claimed.

A separate external-API invalid-name probe passes eight assertions covering
exact `ErrorReadingFDD`, no created execution and a successful valid-FOM retry.
API/exception classes come from the external official JAR and provider classes
are independently compiled. These checks are separate from the 540 JUnit
invocations and do not establish all overload/state/callback semantics.

## Requirement Coverage

The three supplied redlines still produce a candidate census of 793 pages,
1,856 review units and 210 direct-body decomposition declarations. There are
712 requirement records: 639 applicable, 48 excluded and 25 unresolved.
Of the applicable records, **402 have no linked oracle**. In total, 472 records
have no oracle link. These are missing assessed mappings, not proof that every
corresponding feature is absent from the implementation.

All 639 applicable records retain open semantic assessments. The 867 overlapping
open notes are not a defect count, and candidate heading counts are not an
exhaustive atomic-obligation inventory.

The `full-2010` readiness check fails with 43 entries, including incomplete
normative coverage, inventory/manifest mismatch, 37 missing clause locators,
missing independent reviewed/executed evidence, and unresolved claim scope.
This check starts no tests; the actual behavioral campaigns above execute
independently and are not blocked by those readiness prerequisites.

`coverage_complete:false` and `full_conformance:false` remain the correct flags.
Neither a large passing test count nor compile-time API correspondence closes
unlinked source obligations or erases observed failures.

The Go/Python/Java/IVCT execution reconciliation finds passing records for all 347 existing
oracle links, with completeness unassessed. None of the eight Java/IVCT failed
invocations (six literal methods) has a current requirement link. The failure
index retains those failures, including Java after its successful retry; this
is a traceability gap, not a parser suppression or 347 verified requirements.
The later tagged Go and Linux race runs are recorded separately and are not
inputs to that reconciliation; it is not a complete index of this final report.

## Evidence Locations

All paths below are under `D:/workspace/gorti/full-consistency-20261003-r1/`:

- `go/full.jsonl`, `go/summary.json`: complete configured Windows Go run.
- `race/full.jsonl`, `race/first-attempt-summary.json`: initial full Linux race run, including the internal-build deadline failure.
- `race/retry/full.jsonl`, `race/retry/summary.json`: separate passing full Linux race retry, including cleanup and unchanged-test evidence.
- `go-tagged.jsonl`, `go-tagged-recheck.jsonl`, `go-tagged/`: actual Windows runtime/control callback campaign and reproduced cross-generation failure.
- `python/results.xml`, `python/evidence-manifest.json`, `python/inventory-reconciliation.json`: fresh Python scope and inventory findings.
- `java/results-summary.json`, `java/work/javasdk/client/target/surefire-reports/`: initial Java run, including failures.
- `java/restart-recheck/results-summary.json`: unchanged-source six-case retry.
- `java/official-api-summary.json`: API provenance, signatures, discovery controls and invalid-name runtime probe.
- `independent-live.xml`, `independent-support.xml`: 28 supplementary live checks.
- `ivct-subset-final.xml`, `ivct-subset-final-console.txt`, `ivct_binary.py`: failed legacy subset and disclosed binary-selection adapter.
- `evidence-tools.xml`: evidence-tool tests.
- `supported-quality-gate.json`: executed bounded profile.
- `coverage/full-2010-check.json`, `coverage/phase1-summary.json`: full-readiness failure and source/application coverage census.
- `coverage/current-assessment.md`, `coverage/ivct-failure-triage.json`, `coverage/failure-index-proof.json`: source-backed failure triage and retained-failure/link reconciliation.

A compact [machine-readable report](ieee1516-2010-full-validation-20261003.json)
records these scoped results and hashes of the primary evidence artifacts.

## Next Work

1. Isolate the remaining Go save callback after cross-generation restore, including invocation acknowledgements and server/local replay provenance.
2. Reconcile the legacy IVCT raw-generation, pending-acquisition teardown and duplicate-label callback oracles, preserving the failed baseline and positive/error controls.
3. Isolate Java child deadlines and the Linux internal-build deadline; preserve first failures and do not merely extend deadlines.
4. Update the HLA file/node inventory and restore the optional C++ publisher prerequisite if that separate product test is to be required.
5. Link and independently review remaining applicable obligations, callback/statechart/table/schema/API facets and unresolved applicability before making a whole-RTI claim.
