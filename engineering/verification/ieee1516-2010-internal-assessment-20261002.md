# Internal IEEE 1516 2010 assessment

Assessment date: 2026-10-02. Scope: the current local RTI implementation,
Go/Python product interfaces, and the selected standard Java binding.
This is an internal assessment, not certification or an external report.

Subsequent [defect corrections and reruns](ieee1516-2010-defect-corrections-20261002.md)
record repairs to these assessed inputs and reviewed observer contracts. The
failed executions below remain the outcomes of this earlier snapshot; they
are not retroactively changed to passes.

**Full internal conformance assessment: NOT PASSED.** The broad Go regression
and core-service race campaigns passed, but broader Python and standard-Java
execution reproduced current behavioral defects. The complete applicable
normative obligation inventory and whole-binding semantic evidence also remain
open. Runtime failure, obsolete/ambiguous test expectations, evidence gaps and
external certification are separate dispositions below.

## Fresh execution

| Campaign | Actual result | Boundary |
| --- | --- | --- |
| Windows Go RTI, SDK, tests and examples | 3,240 top-level tests passed; 9,669 passing test/subtest events | 86 packages: 76 passed, 10 have no test files; zero failing or skipped behavioral tests |
| Linux HLA services and delivery, race detector enabled | 1,030 top-level tests passed; 2,551 passing test/subtest events | 11 packages passed, 1 has no test files; zero failing/skipped tests or reported races |
| Current regression-manifest links | All 985 literal test references in 226 groups found passing | Link coverage in the broad Go run, not execution of the gate's separate per-group commands |
| Independently authored public Python TCP contracts | 6 passed | Named positive/negative contracts; not an exhaustive service matrix |
| Verification-tool unit tests | 64 passed, 85 subtests passed | Checks fail-closed handling of invalid/missing/failing evidence |
| Python HLA campaign, distinct cases | 1,921 passed, 30 failed, 1 skipped, zero errors | 1,952 cases in 130 HLA files; broad rerun and explicit live run merged by case identity |
| Python explicit live selection | 53 actual-TCP cases passed, 7 failed; 23 selector unit cases passed | Repeated cases are not added to the distinct total |
| Java full live Maven campaign | 499 passed, 3 failed, 3 skipped | 447 unit/in-process passes plus 52/55 live passes; three fixture methods skipped |
| Java external-original-API selected suites | 205 passed, 1 failed, zero skips | Independent compilation/class origins; overlapping cases, not additional unique coverage |
| Java independent callback-state probes | 0 passed, 4 failed | Enable/disable callbacks during saving and restoring |
| Java empty-FOM/default-MIM probe and valid control | 1 passed, 1 failed | Invalid empty admission and absent MIM reproduced; valid-FOM/default-MIM control passed |
| Full-2010 evidence prerequisites | FAIL, 43 metadata messages, zero commands started by this check | Inventory/applicability/evidence completeness, not 43 reproduced product defects |
| Source and test links | RECORDS_VALID; 20 seed obligations, 12 with implementation/test links | All 20 remain open for assessment; the seed is not the full standards census |

The broad Go command executed directly despite the incomplete full-claim
metadata. The metadata check did not prevent the service experiments.
No skip, missing prerequisite, or previous checkpoint is promoted to a pass.

## Commands and candidate

Windows uses Go 1.26.5 with the existing offline module cache, Python 3.14
at `D:/workspace/gorti/n04-venv/Scripts/python.exe`, and the registered official
FDD schema. The broad command was:

```text
go test -json -count=1 -timeout=180s ./rti/... ./tests/... ./examples/...
```

Linux uses WSL Ubuntu 24.04, Go 1.22.2 and CGO:

```text
go test -json -race -count=1 -timeout=180s ./rti/internal/hla/... ./rti/internal/adapters/delivery
```

This is a Linux core/race campaign, not the complete Linux server/SDK release
campaign. `/usr/bin/python3` lacks `lxml`; no whole-Linux FDD-validation campaign
is inferred from the race run.

The current server was freshly built as
`D:/workspace/gorti/rtid-internal-assessment-20261002.exe` from
`./rti/cmd/rtid`. Its SHA-256 is
`6c9cf2c18c06236a909e900d9d70e58db6fa31e1d6708e81400e44f5ab17d979`,
identical to the October 1 SDK-fix executable. The six public TCP scenarios
used this fresh path, checked the pin before/after, and closed all six owned
server processes. The new-server exception trailer assertion was enabled.

## Evidence integrity

The source/test input record covers 2,348 files in selected roots: `rti`,
`proto`, `pysdk`, `javasdk`, `scripts`, `tests`, `examples`, plus Go module files.
Hashes were unchanged when rechecked after the core campaigns. Actual contents
are preserved in an archive outside Git. This selected-root record is not a
claim that the entire repository, every toolchain dependency, or a settled
release profile is frozen.

| Artifact outside Git | SHA-256 |
| --- | --- |
| `D:/workspace/gorti/internal-source-inputs-20261002.json` | `6cf030f3a4143f7c3b309977273a967af8377ae4695767a6b2e37d56c274c116` |
| `D:/workspace/gorti/internal-source-inputs-20261002.tar` | `a2d1cc4964c131ca2641a8b9896b6dfd7262dda25a30768d799e88cf68a24aed` |
| `D:/workspace/gorti/internal-go-all-20261002.jsonl` | `eb583b143100c0166a6c2ce391b3d883aef554ccb6a9bf270f92421911472545` |
| `D:/workspace/gorti/internal-go-race-20261002.jsonl` | `598602cc79d0a1068c1f15ddd9f9796772fea6089f236f186179f066b14f7e8b` |

Additional records are `internal-go-summary-20261002.json`,
`internal-go-link-coverage-20261002.json`, `internal-full-readiness-20261002.json`,
`internal-traceability-20261002.json`, `internal-verification-tools-20261002.xml`
and `internal-blackbox-assessment-20261002.xml`, all under
`D:/workspace/gorti/`. Live FOM inputs, server logs, process records and callback
observations are under `internal-blackbox-assessment-20261002/`.

## Internal exit criteria

Use the existing [quality gates](ieee1516-2010-quality-gates.md), rather than
test-count percentages, for the internal L2 assessment. The RTI must be assessed
against its own responsibilities. User-federate and federation-model obligations
need their own applicability decisions; they are not automatically missing RTI
functionality. Go/Python product interfaces do not replace verification of the
selected standard Java API.

| Gate | Current disposition |
| --- | --- |
| G0 normative applicability and inventory | Open: the source-reviewed seed and historical defect ledger are not exhaustive |
| G1 reproducible evidence | Partial: fresh raw results, pinned executable and selected source contents retained; settled full release scope and dependency/platform evidence remain |
| G2 binding/model contract | Partial/failed: official Java provenance and compilation verified, but public binding contracts have reproduced failures; whole-binding closure absent |
| G3 observable semantics | Failed/open: current Python/Java behavioral failures and unreviewed obligation coverage remain despite passing Go and selected black-box campaigns |
| G4 release assessment | Not ready: known failures and no complete applicable campaign/signed internal release assessment |
| G5 external assessment | Not performed; outside this internal task |

The current full-claim checker reuses a bounded engineering-regression manifest.
Missing IEEE clauses for engineering IDs, inventory mismatch and missing review
records are metadata/model issues to resolve; they must not be misreported as
the same number of runtime defects. No `coverage_complete` flag is changed to
make this campaign pass.

## Python failures and normative findings

The [Python assessment](ieee1516-2010-internal-python-audit-20261002.md)
preserves all failing outcomes, including those with obsolete or ambiguous
oracles. The fresh-cache rerun removed initial build/cache access failures,
but the final distinct-case campaign still fails. Its 46 excluded files are
RL/application/static-quality scope exclusions, not passing HLA tests. One
remaining skip requires an unavailable C++ publisher; no C++ behavior is credited.

| Failed cases | Classification | Disposition |
| ---: | --- | --- |
| 5 | DDM bounds queries omit the required authenticated member/session | Confirmed product SDK integration defect |
| 2 | Interaction advisory dispatcher uses a branch-local `InteractionClassHandle` before binding it | Confirmed Python defect, including a real-server manifestation |
| 4 | Obsolete Integer64-rejection and pending-metric expectations | Failing oracles retained; later assertions not credited |
| 3 | Historical synchronization/restore participation | Callback invocation/restore-stage compatibility requires review |
| 2 | Same-time grant ordering selected by equality | Ambiguous event-identity oracle, not a proven ordering defect |
| 10 | RO queue/raw-consumer receipt boundary | Accounting/observer mismatch; nine timestamp checks never reach their invalid request |
| 1 | DDM no-shared-dimension delivery | Unresolved observable routing mismatch |
| 2 | Ownership restore callbacks/outbox | Unresolved receipt/recovery integration; not dismissed as fixture-only |
| 1 | In-process event pump crosses threads without a loop wakeup | Source-supported thread-boundary diagnosis; not a demonstrated TCP failure |

The [normative audit](ieee1516-2010-internal-normative-audit-20261002.md)
confirms that the current seed and signature census are not an exhaustive
obligation inventory. It separates RTI responsibilities from arbitrary user
federate/SOM state and unclaimed OMT-authoring-tool responsibilities. It also
identifies an empty-FOM shortcut that bypasses standard-MIM merge. The Java
campaign subsequently reproduced that path through the official public API.

## Java findings and official API evidence

The [Java assessment](ieee1516-2010-internal-java-audit-20261002.md) compiled
the provider and clients against the external original IEEE API and verified
runtime class origins. Of 204 API sources, 202 match the official artifact
byte-for-byte; the two documented Java 11 factory-discovery adaptations are
kept explicit. Public/protected signatures match. Direct original-API execution
and adapted discovery both have successful runtime probes, but successful
compilation/factory discovery does not establish complete service semantics.

| Finding | Reproduction and disposition |
| --- | --- |
| Callback switches during save/restore | Four independent tests fail: both enable and disable return success instead of the source-required SaveInProgress/RestoreInProgress. IEEE 1516.1-2010 10.43/10.44, physical PDF 284-285, visually reviewed. |
| Idle restore completion | federateRestoreComplete returns RTIinternalError from an SDK readiness guard instead of RestoreNotRequested. Reproduced in ordinary and external-API runs. |
| Empty-FOM admission/default MIM | Creation and join with new URL[0] succeed, but the standard HLAfederate MOM class is absent. Valid FOM with omitted MIM provides that class. No unreviewed empty-array exception identity is invented. |
| Float-only selector expectation | One existing live test incorrectly expects supported Integer64 selection to fail. Its failure remains recorded; seven Integer64 production cases and exact independent grants pass. |
| Restart/discovery connection timeout | Original runs fail at loopback connection deadlines; fresh reruns pass. Cause is not isolated, so earlier failures are retained rather than declared environmental or erased. |

The Java campaign records 447 offline passes with 44 live/fixture skips;
enabling live properties produces the 499/3/3 row above. Parameterization changes
the denominator. External selected-suite and focused rerun results overlap the
ordinary suite and are not added together. The six-case restart rerun passes,
while the idle-restore exception reproduces. Implementing EVOKED only is a
reviewed valid callback-model choice; IMMEDIATE is not counted as a missing
mandatory implementation in this assessment.

Java raw logs, command/classpath/exit records, Surefire/JUnit reports, external
API hashes, independently authored probe sources and input models remain under
`D:/workspace/gorti/internal-java-assessment-20261002/`. Python raw records and
selection/exclusion/case-identity accounting remain under
`D:/workspace/gorti/internal-python-assessment-20261002/`. All assessment-owned
server processes and execution sessions have ended.

## Correction and verification priorities

1. Repair the confirmed Python DDM query session/identity propagation and
   interaction-advisory dispatch errors; rerun the failing cases with valid
   authenticated controls and unchanged unknown/stale-member rejection.
2. Repair Java callback-switch save/restore guards and idle restore-completion
   error precedence; promote the independent public-API probes into regression
   coverage without changing their source-derived expectations.
3. Correct missing-FOM admission and default-MIM construction at the appropriate
   standard/public creation boundary. Preserve explicitly declared product
   extensions and replace lifecycle fixtures that relied on a production shortcut
   with lawful minimal FOM inputs or proper test repositories.
4. Diagnose unresolved DDM delivery, ownership/outbox recovery and callback queue
   boundaries; distinguish raw delivery from actual invocation without ignoring
   observed failures. Isolate the Java connection timeout observation.
5. Independently review obsolete/ambiguous oracles, then rerun their entire
   scenarios so later assertions are actually exercised. Do not merely delete
   failing expectations or count unreachable checks as passes.
6. Complete the RTI-only source obligation/applicability map, attach exact current
   API/state/callback evidence, and run the complete permitted release profiles.
   Keep the engineering regression ledger separate from normative requirements.

No production fix or test-assertion relaxation is part of this verification
increment. These findings invalidate an all-passing internal assessment even
before the remaining obligation-coverage work is considered.
