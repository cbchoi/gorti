# Applicable Requirement Progress, 2026-10-03

**Status: incomplete, with reproduced behavioral failures.**

This increment follows the [full validation run](ieee1516-2010-full-validation-20261003.md).
It reviews applicable requirements without test links, adds narrowly described
oracles and executes new boundary checks. No production or SDK implementation
was changed. Existing source statements, applicability and reviewed-unit scopes
are preserved. Linking an assertion does not establish full fulfillment.

## Requirement Accounting

There remain 712 records, 639 applicable and 25 unresolved. Applicable records
without any oracle link decrease **402 to 229**: 173 gain partial evidence links.
Links increase **347 to 597**. These are not 250 new tests or 173 completed
requirements; tests can be reused and contain only some required facets.

| Slice | Unlinked Before | Unlinked After | Newly Linked |
| --- | ---: | ---: | ---: |
| declaration-object.json | 43 | 3 | 40 |
| fm.json | 295 | 212 | 83 |
| framework.json | 1 | 0 | 1 |
| java-annex.json | 37 | 5 | 32 |
| omt.json | 3 | 0 | 3 |
| ownership-ddm.json | 1 | 0 | 1 |
| support-mom.json | 15 | 8 | 7 |
| time-mapping.json | 7 | 1 | 6 |

All applicable semantic assessments remain open. No source unit or full-standard
coverage claim is promoted. The [machine-readable report](ieee1516-2010-applicable-requirements-20261003.json)
contains every newly linked ID, all 229 unlinked applicable IDs, and retained
nonpassing links. Rows with links can still need additional tests or fixes.

## Execution And Evidence

Fresh artifacts are outside Git at
`D:/workspace/gorti/applicable-requirements-20261003-r1/`.
The live checks use the unchanged assessed Windows server, SHA256
`fc2bd04a5e64970e2dd0f6cb37f98741a257a578e273cae6458d4fd9602d75e8`.

| Fresh Scope | Top-Level Pass | Top-Level Fail | Notes |
| --- | ---: | ---: | --- |
| FOM parser/model and new OMT contracts | 110 | 0 | 321 passing nested records; public parsers, not live admission |
| Java selected mapping/callback suites | 204 | 0 | Three new cases; unit/loopback, not a server campaign |
| TM/MOM focused checks | 15 | 0 | 300 passing nested records; production components/in-process RPC |
| Corrected IVCT-inspired subset | 35 | 0 | Actual server; not official IVCT certification |
| Evidence-tool tests | 129 | 0 | 112 passing subtests and one warning; not IEEE obligations |
| Initial new declaration/object/DDM probes | 2 | 5 | Includes a wrong-owner DDM query, corrected in the reviewed selection |
| Declaration/object reviewed selection | 44 | 4 | Four object boundary declarations remain red |
| Real-server object-name boundary probe | 0 | 1 | Four failing parameter subcases |
| Passive-routing supplementary control | 1 | 0 | No skipped or nested cases |

Campaign scopes overlap and must not be summed as distinct tests. No skip or
expected-failure waiver makes the new failing probes green. Original full Java
child-deadline failures and Go cross-generation callback failures remain in the
preceding report; no new full-product regression or race campaign was performed.

The final reconciliation imports original failures as well as fresh results:
**583 passing-record link statuses, 14 nonpassing-variant links, zero missing
execution records**. This is evidence matching, not semantic qualification.
Some nonpassing links retain earlier IVCT failures even though revised helpers
and oracles now pass. Source revision differences and old failures remain
visible; the current successful run does not retroactively pass the old tests.
The original DDM fixture failure is likewise retained beside its corrected pass.

## New Behavioral Findings

The new component tests reproduce four object/declaration boundary gaps:

- Implicit delete-privilege publication is absent at initial publication and
  after a new publication lifetime in the inspected production-manager stack.
- An HLA-prefixed single object-name reservation is accepted and changes state.
- Invalid multiple reservation inputs, including an empty set and a mixed
  valid/HLA-prefixed set, are accepted or produce callbacks instead of the
  expected synchronous rejection without effects.
- Explicit unreserved-name registration succeeds and creates visible state
  instead of rejecting with `ObjectInstanceNameNotReserved`.

The raw live-server probe independently reproduces the three name-admission
families, with the multiple family split into two cases. The implicit-publication
finding remains a component-stack observation, not a completed live binding
assessment. Selected SDK service guards, error precedence, automatic names,
reservation consumption, restore compatibility and existing examples must be
considered when correcting these boundaries.

The initial DDM time-independence probe receives `PermissionDenied` because
its bounds query uses another federate's region. The corrected owner-authenticated
probe preserves the pending/committed bounds and unchanged logical-time assertions
and passes in the reviewed selection. The requirement gains that bounded link;
the original error does not establish time-dependent DDM or full DDM completion.

The declaration probe's four servers and the separate IVCT fixture server are
stopped; no owned campaign process remains.
The per-slice reviews list exact IDs, source and assertion boundaries:
[FM](requirements2010/fm-oracle-review-20261003.md),
[DM/OM](requirements2010/declaration-object-applicable-review-20261003.md),
[Java](requirements2010/java-annex-applicable-review-20261003.md),
[TM](requirements2010/time-mapping-review-20261003-r1.md), and
[Support/MOM](requirements2010/support-mom-review-20261003-r1.md).

## Harness Corrections

The earlier five IVCT failures were reconciled with their current contracts:

- Raw destroy obtains and sends federation generation. Joined-member rejection
  now checks the exact `FederatesCurrentlyJoined` error trailer, not only a
  generic gRPC status that a different guard could satisfy.
- Duplicate synchronization registration checks the failed callback and exact
  label-not-unique reason, original tag/announcement retention and successful
  subsequent achievement of the original point.
- Pending acquisition teardown uses a cancellation-capable resign directive;
  the owner's release-request attribute/tag assertions remain unchanged.
- The fixture accepts an explicitly selected server and checks its supplied hash.

Python result resolution now handles repository tests and nested `pytest.ini`
collection roots rather than assuming every test is below `pysdk`. TDD first
reproduced two out-of-root resolver errors; the correction passes all 15 focused
resolver tests, including nonpassing-variant and wrong-module controls.
Independent review found no actionable issue in the inspected mapping/helper
changes. JUnit collection-root provenance remains a future ambiguity risk; no
collision was found among current linked aliases.

## Plan, Do, Review, Reflect

**Plan:** assess source responsibility and exact assertions, preserving unknown
facets and failed evidence rather than replacing missing links with generic tests.

**Do:** review eight disjoint slices, update bounded mappings, add OMT/Java/TM/MOM/declaration/object probes,
correct stale wire/callback/cleanup helpers, and rerun the affected contracts.

**Review:** inspect literal assertion bodies and fixture boundaries, reconcile
source/test declarations and actual invocation records, retain newly failing
requirements, and separately review the main tooling/OMT/helper changes.

**Reflect:** the largest remaining gap is FM: 212 unlinked applicable records,
mainly service preconditions, exceptions and save/restore callback cuts. Next
implementation work should address reproduced object-name admission and implicit
publication first, then rerun unchanged positive/error controls, SDK examples,
whole Go/Python/Java suites and race checks. Remaining requirements also need
MOM actor/extension isolation, exact evocation timing, selected-binding and
independent normative-resource review. External certified identification cannot
be replaced with a local test.
