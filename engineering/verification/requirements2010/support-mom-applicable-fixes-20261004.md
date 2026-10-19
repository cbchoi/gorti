# Support/MOM Applicable Increment, 2026-10-04

## Plan

Assess the eight previously unlinked applicable rows against S1516.1, then add
behavioral assertions and bounded fixes. Source: local IEEE 1516.1 redline,
SHA256 `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4`;
clauses 10.42 (PDF 283-284), 11.2.1 (287), 11.3.1 (288), 11.4.1
(289-290, 292-294), and 13 (390). Projection remains candidate source data.
No reviewed-unit expansion or whole-source completeness claim is made.

## Do

All eight rows now have narrowly relevant links, adding eight links (53 to 61).
The slice retains 43 applicable records, all linked. Certification-label
entitlement remains unestablished: the conditional obligation stays applicable,
and no recognized authority's certificate/public results were supplied.
A runtime test cannot manufacture that external condition.

All Go tests below are new literal declarations in
`rti/internal/hla/mom/applicable_support_actor_20261004_test.go`:

| Requirement | Literal test | Bounded assertion |
| --- | --- | --- |
| SM-MOM-OBJECT-EXTENSION-ISOLATION | TestApplicableSupportMOMExtensionOwnershipAndIsolation | Separate participant subclass instance; exact extension payload; no RTI subclass update/publication or object subscription |
| SM-MOM-OWNERSHIP-PROHIBITIONS | TestApplicableSupportMOMExtensionOwnershipAndIsolation | Predefined owner survives acquisition attempt; participant subclass attribute remains participant-owned |
| SM-MOM-REQUEST-ATTRIBUTE-REFRESH | TestApplicableSupportMOMRequestRefreshBothForms | Class/instance request services produce exactly three/one fresh switch-value reflections |
| SM-MOM-REPORT-EXTENSIONS | TestApplicableSupportMOMReportExtensionPromotion | Full four-parameter subclass vs three-parameter promoted leaf; RTI report uses only predefined leaf/parameters |
| SM-MOM-LEAF-ADMINISTRATION | TestApplicableSupportMOMPredefinedPublicationSet | Exact 32/12 predefined object attribute sets plus inherited delete privilege |
| SM-MOM-PROHIBITED-MUTATIONS | TestApplicableSupportMOMActorPreservesTransportAndOrder | Actual RTI refresh remains reliable/receive-order, untimed, untagged; overrides unchanged |

`SM-SUPPORT-EVOKE-MULTIPLE` links
`javasdk/client/src/test/java/org/gorti/rti/ApplicableSupportEvoke20261004Test.java`:
`multipleWaitsForMinimumAndStopsBacklogAtMaximum`. It checks empty/minimum wait,
continuous callback backlog, maximum-bound termination and the pending flag.
The connection is mocked and callback bodies are injected into the real SDK
queue; this is not a live-server or arbitrary-federate-model campaign.

`SM-CONFORMANCE-CERTIFIED-LABEL` links the new
`scripts/tests/test_ieee2010_certification_claim_guard.py` literal
`test_green_mapping_and_records_cannot_claim_hla_conforming`. The actual report
builder receives a completely linked applicable synthetic inventory, a reviewed
unit and a passing execution record. Both report and printed summary retain
`full_conformance=false`; coverage remains incomplete, the semantic verdict
stays open, the external-certificate gap is retained, and no HLA Conforming
label is emitted. This is a conservative product-claim guard, not certification.

Production fixes publish all predefined object attributes before initial
ownership, route requests for RTI-owned attributes through fresh source-backed
MOM publication rather than a nonexistent RTI callback queue, and select/project
immediate interaction ancestor recipients. The inheritance fix preserves
existing callback/time behavior and the current-FOM-reference cache contract.

## Review

Raw evidence root: `D:/workspace/gorti/applicable-fixes-20261004/support-object/`.
`run-counts.json` inventories every attempt, including compilation mistakes,
fixture corrections and first-red product assertions. Nothing is silently
replaced by a later pass.

Final evidence, after the rate correction:

- `corrected-final-focused.jsonl`: 15 top-level and 425 nested passes, zero
  failures/skips. Of these, the new Go declarations contribute 10 top-level
  and 6 nested passes. The remaining cases include the two server catalogs
  and parent-owned privilege/reservation lifetime tests.
- `affected-regression-complete.jsonl`: all five affected packages, 818
  top-level and 1,443 nested passes, zero failures/skips. This run precedes
  only the addition of the extra injected commit-failure assertion; the final
  focused run above executes that assertion with unchanged production code.
- `claim-guard-final-linked-results.xml`: 19 passes, zero failures/errors/skips
  (one new claim guard and 18 existing evidence-index safeguards). Its new
  class name is
  `scripts.tests.test_ieee2010_certification_claim_guard.TestCertificationClaimGuard`.
  Actual slice validation and execution resolution find the guard as
  `passing-records-only-completeness-unassessed`.
- Both owned slices validate: support/MOM 43/43 applicable linked;
  declaration/object 88/88 applicable linked. The original eight-slice
  applicable denominator remains 639. A separate concurrently added FM
  83-row slice makes the current directory-wide count 722; it is not this
  worker's scope or an applicability reclassification.

Historical attempts retained below are not the final verdict:

- New Go assertions in `final-focused.jsonl`: 8 top-level cases, 7 pass and
  1 fail (the declaration/object rate case), plus 4 passing nested cases.
- The larger selected run in that same file: 13 top-level cases, 12 pass,
  1 fail; 423 nested cases pass; zero skips. This includes both corrected
  server catalog tests and parent-owned privilege/reservation tests.
- Java first run: 1 error/timeout, zero passes/skips, including cold connection
  setup in the five-second test deadline. Setup was moved to BeforeEach without
  weakening callback timing assertions or the five-second test-body deadline.
  Final isolated XML: 1 pass, zero failures/errors/skips, at
  `java/final-reports/TEST-org.gorti.rti.ApplicableSupportEvoke20261004Test.xml`.
  Original XML remains under `java/first-reports/`.
- `package-regression-final.jsonl` records the complete four-package run before
  the cache correction. `cache-correction.jsonl` passes both cache assertions.
  `object-regression-final.jsonl` is the subsequent complete object-package run:
  209 top-level passes, 2 failures; 405 nested passes, 4 failures; zero skips.
  At that checkpoint, failures were the assigned rate case and parent's independently
  added reservation callback-admission atomicity case, not suppressed variants.

Exact focused command, with configured offline Go caches and Python schema
runtime, from repository root:

```text
go test -count=1 -p=2 -json ./rti/internal/hla/object ./rti/internal/hla/mom ./rti/internal/hla/declaration ./rti/internal/application/server -run '^(TestApplicableSupport|TestMOMFullCatalogActualServer$|TestMOMServiceExecutionRealManagersAllLeaves$|TestPrivilegeLifetimePersistenceAndReset$|TestPrivilegeInitialOwnershipFollowsPublicationLifetime$|TestApplicableR2ReservationSurvivesRegistrationDeletionAndRestore$)'
```

Earlier package regression command: `go test -count=1 -p=2 -json
./rti/internal/hla/object ./rti/internal/hla/mom ./rti/internal/adapters/grpc
./rti/internal/hla/core`; final object rerun: `go test -count=1 -json
./rti/internal/hla/object`. Go executable: `C:/Program Files/Go/bin/go.exe`.

The final complete affected-package command adds
`./rti/internal/hla/declaration` to that four-package command. Final claim-guard
command, from repository root with plugin autoload disabled:

```text
D:/workspace/gorti/n04-venv/Scripts/python.exe -m pytest --rootdir=D:/workspace/gorti/gorti --import-mode=importlib -c pysdk/pyproject.toml -p no:cacheprovider -p pytest_asyncio.plugin -o addopts= scripts/tests/test_ieee2010_certification_claim_guard.py scripts/tests/test_ieee2010_requirements_review.py --junitxml=D:/workspace/gorti/applicable-fixes-20261004/support-object/claim-guard-final-linked-results.xml
```

The first guard attempt has one fixture/census failure and 18 passes in
`claim-guard-results.xml`. The fixture was corrected to an accepted heading on
body page 17. Intermediate green XML files used an unsuitable root/class name
for declaration/evidence linking. Only `claim-guard-final-linked-results.xml`
is the final linkable script evidence; previous files are retained.

Java used an external workspace copy under `support-object/java/work`, Java
11.0.31 and Maven 3.9.16 with offline `D:/workspace/gorti/java-m2`:

```text
mvn.cmd -o -B -ntp -f D:/workspace/gorti/applicable-fixes-20261004/support-object/java/work/javasdk/pom.xml -Dmaven.repo.local=D:/workspace/gorti/java-m2 -Dtest=ApplicableSupportEvoke20261004Test -Dsurefire.failIfNoSpecifiedTests=false -Djava.io.tmpdir=D:/workspace/gorti/applicable-fixes-20261004/support-object/java/tmp test
```

Only the named fresh Java XML is evidence; historical reports copied with the
external build tree are not part of this campaign. No standalone server binary
was launched. Go servers/queues and the Java mock server close in test cleanup;
all owned command sessions completed.

## Reflect

All applicable rows in this slice now have links, but links are not verification.
Still open: same-instance added MOM attributes, exact Table 5 interaction
declarations, all Table 6 source/encoding/trigger variants, DDM exceptions,
all report extension combinations, Java live callbacks, and certification.
`coverage_complete` remains false and source-scope review remains incomplete.

Exact owned changed paths and final SHA256 identities are recorded in the
external `owned-changed-files.json`. No Git operations were performed.
