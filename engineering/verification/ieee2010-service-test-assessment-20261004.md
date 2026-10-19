# IEEE 2010 Service Test Assessment, 2026-10-04

Follow-up: the reproduced Support exception defects were subsequently repaired
and regression-tested. See [Support lookup repair](ieee2010-support-lookup-fix-20261004.md).
The MIM lookup was subsequently corrected by applying Clause 11 table precedence;
see [MIM table-precedence repair](ieee2010-mim-table-precedence-fix-20261004.md).
The observations and counts below retain the pre-repair assessment.

## Scope and Verdict

This review assesses the prepared IEEE service, SDK and FDD/MIM tests and
their recorded outcomes. PPO reference data, RL extensions, lint and
certification-label checks are outside the decision. Production code was not
changed. Existing execution records were inspected; only the six focused
checks below were newly executed. This was not another full-suite run.

**Verdict:** the ordinary linked tests have passing execution records, but
the suite is not yet sufficient for a whole-service behavioral verdict.
One source-conditional MIM diagnostic remains in error. A new real-server
probe independently reproduces incorrect standard Java exceptions for three
Support services. These are implementation defects, not MIM interpretation
questions.

## MIM Name Discrepancy

The supplied `15161_2010-2004_redline.pdf`, PDF page 298, Table 6, visibly
contains `HLAAnnexSaveName`. This was checked in a newly rendered page, not
in extracted text alone. The MIM from the IEEE supplementary archive defines
`HLAnextSaveName`. The archive member, the previously extracted official MIM
and the current embedded MIM have identical SHA-256:

`839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023`

The archive is listed on the [IEEE 1516.1-2010 resource page](https://standards.ieee.org/ieee/1516.1/3745/).
The implementation therefore follows the distributed MIM, rather than
introducing its own spelling.

**Classification:** an actual discrepancy between supplied standards
artifacts. It is not an extraction error, and the name mismatch alone does
not establish a save-service algorithm defect. However, Clause 11.6 (PDF
page 292) and Annex G (PDF page 583) explicitly give the Clause 11 tables
precedence. No applicable correction establishing a different precedence
or spelling was established in this review. Under a literal reading of the
supplied table, the lookup does not satisfy that table. The diagnostic must
not be discarded or marked passed merely because the MIM uses the other name.
Changing the predefined MIM name without resolving the source discrepancy
is not recommended.

## Independent Implementation Defect

For unknown names, the standard Java API declares `NameNotFound` for
`getAttributeHandle` (10.11), `getInteractionClassHandle` (10.15) and
`getParameterHandle` (10.17). These declarations are present in the incorporated
IEEE Java API. The diagnostic uses unrelated, deliberately nonexistent names
to avoid relying on the disputed MIM spelling.

| Actual Java API check | Expected | Observed | Result |
|---|---|---|---|
| Existing `HLAnextSaveName` handle/name round trip | Same name | Same name | Pass |
| Existing interaction parameter handle/name round trip | Same name | Same name | Pass |
| Missing object class | `NameNotFound` | `NameNotFound` | Pass |
| Missing attribute on valid object class | `NameNotFound` | `RTIinternalError` | Fail |
| Missing interaction class | `NameNotFound` | `RTIinternalError` | Fail |
| Missing parameter on valid interaction class | `NameNotFound` | `RTIinternalError` | Fail |

The cause is in `rti/internal/adapters/grpc/support.go`: the object-class
lookup adds the `rti-spec-exception=NameNotFound` trailer, but the three other
handlers return bare `NOT_FOUND`. `javasdk/client/src/main/java/org/gorti/rti/WireErrors.java`
then falls back to `RTIinternalError`. The current source still has this path.
This finding concerns exception semantics; the invalid requests are rejected.

The probe used the same server binary as the latest broad Java campaign:
SHA-256 `c4272e212e51dcd44fb4d4d1dad44520b0f8ada55c1f5d3ada64553d1dfdda10`.
It used the recorded, compiled Java SDK and actual localhost TCP service,
created and joined its own federation, resigned and destroyed it, and stopped
its owned server. Result: **3 passed, 3 failed**, exit code 1.

## Prepared Test Outcomes

Recounting `final-reconciliation.json` for applicable requirements, excluding
the certification-label guard, gives **869 requirement-to-test references**
and **458 unique `(path, test declaration)` pairs**. Of these, **457 have
passing imported invocations** and **one is the MIM source diagnostic error**.
All eight requirement-input file hashes still match that reconciliation.
These are linked declarations, not 458 independently exhaustive requirements
or a count of every parameterized invocation.

Fourteen linked declarations in seven Python files changed after the original
reconciliation. Each has matching passing invocations in the later
`python-lint-fixes-20261004/sdk-local-r2.xml`; that scoped campaign contains
790 passes and no failures or skips. The old source hashes are not silently
treated as current execution evidence.

The larger execution records were also recounted directly:

| Existing campaign | Passed | Failed/error | Skipped |
|---|---:|---:|---:|
| Go RTI/conformance top-level tests | 3,344 | 0 | 0 |
| Go SDK actual-server recovery | 2 | 0 | 0 |
| Focused Linux race top-level invocations | 26 | 0 | 0 |
| Java broad SDK | 581 | 0 | 0 |
| Python scoped follow-up | 790 | 0 | 0 |
| Executable IVCT subset | 35 | 0 | 0 |
| Separately enabled MIM source diagnostic | 0 | 1 | 0 |

Go additionally records 6,665 nested passes; these are not added to the
top-level count. The campaigns overlap and are not summed into one suite.
The newly executed six-check probe is separate from these historical totals.
The earlier 873-reference summary included non-applicable/unresolved records
as well as the label guard, so it is not this review's service-test denominator.

## Test Sufficiency

The suite has substantial regression coverage, but successful declarations
or one linked test per requirement do not establish every required outcome.
Concrete deficiencies are:

1. **Support errors over the real SDK/server boundary.**
   `ExchangeServicesTest.allBasicFomOrderAndTransportationLookupsUseRpc`
   checks successful stub responses. Its exception test injects a canonical
   trailer for the object-class query. `TestSupportObjectClassNameErrorRealWire`
   checks that query only. These checks do not establish that the attribute,
   interaction and parameter handlers actually produce the required exceptions.
   The new probe exposes exactly that coverage gap.
2. **MOM exact values and conditions.**
   `TestMOMFullStandardReportsActualServer` checks fifteen request/report
   scenarios, subjects and selected fields, but some count arrays are checked
   only for a nonzero array length and expected framing, not exact per-class
   counts. The linked Table 6/7/15/17 review still needs independent expected
   values, NULL-report conditions and conditional update triggers. This is a
   test gap, not a reproduced failure of all those MOM functions.
3. **Callback/state-condition completeness.**
   Linked callback and save/restore cases cover specific sequences. The
   requirement inventory does not yet establish a complete matrix of required
   preconditions, exceptions, unchanged state after rejection and callback
   order across the applicable services. Complete linkage cannot substitute
   for those assertions. Add bounded, source-derived cases for the missing
   conditions rather than requiring arbitrary combinations of all features.

The immediate acceptance work is to retain the three new failing cases as
real-server SDK regressions, repair the shared exception mapping, and extend
the same negative-case check to the other Support lookup services. Resolve
the MIM oracle separately; do not use it to hide the unrelated mapping defect.

## Evidence Locations

Private standards and raw execution artifacts remain outside Git:

- `D:/workspace/gorti/ieee2010-source-intake-20260921/table6-298-review.png`
- `D:/workspace/gorti/applicable-fixes-20261004/final-reconciliation.json`
- `D:/workspace/gorti/applicable-fixes-20261004/java/source-review/mim-discrepancy.json`
- `D:/workspace/gorti/python-lint-fixes-20261004/sdk-local-r2.xml`
- `D:/workspace/gorti/ieee-service-assessment-20261004/SupportExceptionProbe.java`
- `D:/workspace/gorti/ieee-service-assessment-20261004/run-probe.ps1`
- `D:/workspace/gorti/ieee-service-assessment-20261004/probe.log`
- `D:/workspace/gorti/ieee-service-assessment-20261004/probe-completion.json`
