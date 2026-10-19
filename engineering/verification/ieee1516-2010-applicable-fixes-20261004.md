# Applicable Requirements: Implementation and Evidence, 2026-10-04

## Scope

This increment corrects reproduced implementation defects and links the
previously unlinked records in the reviewed IEEE 1516-2010 inventory. The
assessment covers the RTI, the selected standard Java binding, and the Go and
Python product interfaces. It does not extend a conformance claim to C++.

The inventory contains 712 records: 639 applicable, 48 not applicable, and 25
whose applicability remains unresolved. The applicable denominator was not
reduced to obtain complete linkage. All 639 applicable records now have at
least one declared test and an explicit description of its assertions, compared
with 229 unlinked applicable records at the start of this increment.

**Complete linkage is not complete behavioral coverage or IEEE conformance.**
Some links are source-conflict diagnostics, a product-claim safeguard, or a
test of the supported architecture boundary. These do not establish the
associated conditional obligation. Requirement semantic verdicts remain open;
`coverage_complete` and `full_conformance` remain false.

## Implementation Changes

| Area | Correction | Verification boundary |
|---|---|---|
| Object names | Explicit names require a reservation held by the caller. Registration retains that reservation; releasing a reservation does not delete a live object. Reservation outcome admission and reservation mutation are atomic. | Single and multiple names, invalid/empty input, another holder, occupied names, exhausted callback delivery, and retry. |
| Registration replay | An internal recovery interface restores recorded registration identity without inventing a public reservation or weakening public registration. | Real event writer/reader and replay, stable object handle/name/class/registrant/time, legacy snapshot compatibility. |
| Delete privilege | Implicit publication follows publication history and explicit unpublication. Snapshot version 4 retains this history while versions 1-3 remain readable. | Export/import, old formats, malformed history, federation generation rebinding, ownership and object regressions. |
| Synchronization callbacks | Checkpoint admission starts at the participant's actual initiation callback, not the request or queue insertion. A queued registration confirmation remains retryable and is deferred until actual terminal invocation. | Real authenticated TCP and Go SDK; positive/negative confirmation, Float64/Integer64, admission retry and unchanged synchronization state. SDK queue tests distinguish entry failure from user-handler failure. |
| Detected federate loss | Authenticated callback-session loss after grace emits a MOM lost-member report; actual loss during an instructed save produces the fault-specific unsuccessful-save outcome. Both fanouts share one delivery reservation. | Surviving subscribed recipients, exact granted logical-time bytes including Integer64 above 2^53, voluntary resignation and reconnection controls, no failed-save bundle. Capacity rejection publishes neither part; retry after freeing capacity emits one of each. |
| Update rates and MOM | Best-effort immediate update reduction obeys rate boundaries without splitting a passel or dropping reliable updates. Admission failure does not consume a successful-send interval. MOM subscription reporting uses delivery admission. | Relevant object, MOM, reset, routing and retry tests. |
| FDD admission | Required enumeration values are checked; foreign-namespace information is distinguished from normative elements. Invalid modules map to the appropriate FDD exception. | Independent schema input, parser, production composition and Java public API. |
| Java lifecycle | Supplied FOM URL designators are preserved; connect, list and interrupted disconnect faults have typed outcomes and cleanup. | Controlled transport failure through the public Java ambassador, exact canonical API comparison and live federation tests. |

Older tests and examples that supplied an unreserved name now reserve it
explicitly or request an automatically generated name when its spelling is
irrelevant. Recovery uses its own internal contract rather than a public
test-only bypass. Tests that previously only read a terminal callback now
report actual invocation before attempting newly admissible callbacks.

## Requirement Traceability

The eight files in [requirements2010](requirements2010/README.md) retain the
source location, applicability, implementation paths, literal test declarations,
assertion-level oracle and remaining conditions. Earlier dated observations
are retained as history, with superseding bounded evidence identified.

`scripts/ieee2010_requirements_review.py --require-applicable-oracles` now rejects
an unlinked applicable record or an empty applicable inventory. It validates
declared tests and source hashes, and reconciles execution records separately.
A passing source-link check cannot manufacture a passing execution or a closed
semantic verdict.

The source census spans 793 supplied PDF pages and contains 1,856 candidate
review units with 210 direct-body decomposition declarations. These are
navigation and review counts, not an exhaustive count of atomic normative
requirements. Tables, state charts, cross-references, corrections and remaining
conditional facets still require semantic review.

## Evidence Campaigns

The [machine-readable summary](ieee1516-2010-applicable-fixes-20261004.json)
records the selected campaigns and artifact hashes. Final reconciliation finds
873 requirement-to-test references: 872 with passing imported invocations,
one with the retained MIM source-diagnostic error, and none without an execution
record. A test can serve multiple records, so these are not 873 unique tests.
Passing invocation references are not requirement-level semantic verdicts.

| Campaign | Passed | Failed/error | Skipped | Scope |
|---|---:|---:|---:|---|
| Go RTI and conformance | 3,344 | 0 | 0 | Top-level tests in 72 packages; 6,665 nested subtests also passed. Real DDM endpoint and independent FDD XSD enabled. |
| Go SDK actual-server recovery | 2 | 0 | 0 | Four Float64/Integer64 subcases; explicit live build tag. |
| Linux focused race | 26 | 0 | 0 | Two repetitions, 126 nested subcases, no race warning. This is not a whole-repository race run. |
| Java broad SDK | 581 | 0 | 0 | Fresh current-server campaign; source-conditional MIM diagnostic is separate. |
| Python core | 2,160 | 4 | 4 | One non-RL core invocation, not a full Python-suite pass. |
| Python actual save callback | 4 | 0 | 0 | Separate latest-server run, Float64/Integer64, successful/unsuccessful save. |
| IVCT subset | 35 | 0 | 0 | Internal executable subset, not independent certification. |
| Requirement tooling | 133 | 0 | 0 | Also 112 subtests; one unused asyncio configuration warning. |
| MIM source diagnostic | 0 | 1 | 0 | Explicit source/resource discrepancy retained, not an acceptance pass. |

The four Python failures are two joined-reference tests requiring unavailable
`numpy`, unavailable `mypy`, and existing whole-SDK Ruff findings. Scoped lint
passes for the changed Python callback files. Optional RL collection was
excluded explicitly; the joined-reference and lint failures were retained.

Final Go SDK, Java broad, MIM diagnostic and Python four-case live campaigns used the same RTID
binary, SHA-256 `c4272e212e51dcd44fb4d4d1dad44520b0f8ada55c1f5d3ada64553d1dfdda10`.
The earlier Python core binary has its own recorded identity. This is not one
frozen whole-tree release qualification.

Raw output, private standards, copied Java build trees and server executables
stay outside the repository under
`D:/workspace/gorti/applicable-fixes-20261004`. The full reconciled record is
`final-reconciliation.json` there; the repository contains only the source
inventory and compact derived summary.

Red runs are preserved. They include reproduced behavior defects, stale test
assumptions, development-time test errors and environment failures; these
categories must not be reported as the same kind of product defect. Corrected
campaigns have separate filenames. A historical pass is not evidence that a
later source revision was tested.

Go counts distinguish top-level test functions from nested subtests. Java and
Python counts are test-case invocations. Counts from different campaigns are
not added into a claim of one fully passing suite. A skipped, failed or missing
invocation remains visible in reconciliation.

## Explicit Open Boundaries

- `FM2010-FAULT-ONE-SEGMENT` has an architecture test, not a passing partition
  selection oracle. The supported RTID has one authority; an advertised peer
  assignment is not a replicated viable execution. Selection among multiple
  independently viable segments remains unproved, and applicability is unchanged.
- `FM2010-S3-PRE-A` and `FM2010-S3-EXCEPTION-B` have a source/binding diagnostic.
  The Disconnect prose and the canonical Java checked-exception list differ.
  Observed idempotence is recorded without declaring it normatively correct.
- `JAVA2010-MIM-TABLE-PRECEDENCE` retains the failing source-conditional lookup
  for `HLAAnnexSaveName`; the supplied official MIM uses `HLAnextSaveName`.
  No normative MIM spelling was changed to make the diagnostic pass.
- `FM2010-INITIAL-EMPTY-EXECUTION` checks that a newly created execution has
  only the required MOM instance. The interaction between the empty-execution
  wording and MOM creation timing remains open; this is not an assertion that
  the execution contains literally no object at the observed boundary.
- The certification-label guard verifies that internal green records do not
  produce a certified claim. It does not replace a recognized certificate or
  independently certified public results.
- Loss tests establish session-loss reporting and immediate-save failure, not
  every fault detector, simultaneous loss, partition scenario or reliable-TSO
  last-known-good guarantee.
- A callback capacity of one cannot atomically hold both fault notifications
  for a survivor. It now preserves state without partial publication, but the
  loss/save transition stays pending. Later resign preparation or journal
  failure after successful joint notification admission is also outside the
  corrected atomic boundary.
- Callback tests establish the stated synchronization-confirmation boundary,
  not every control callback and every successful cross-generation restore
  interleaving.

## Next Review

The later [Python dependency/lint repair](python-dependency-lint-fixes-20261004.md)
records dependency installation, local binding regeneration and scoped
regressions. It preserves this campaign's original counts and distinguishes
the remaining PPO baseline mismatch and concurrent continuation-code errors.

Close the remaining conditional facets with independent oracles, reconcile
source-versus-normative-resource discrepancies, and complete the normative
inventory before assigning requirement-level conformance verdicts. External
qualification remains separate from these implementation and local evidence
results.
