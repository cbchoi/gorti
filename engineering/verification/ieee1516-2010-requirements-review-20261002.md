# IEEE 1516-2010 Whole-Source Requirement Review

Date: 2026-10-02. Assessed product: gorti RTI, its selected standard Java
mapping, and its Go/Python product interfaces.

## Verdict

**The whole-source traceability review has advanced, but exhaustive normative
coverage and full internal conformance are not established.** This increment
builds a reproducible source census, decomposes requirements across all service
families, checks implementation/test links, reconciles selected execution
records, and corrects one source-confirmed Java behavior mismatch.

The [historical twenty-row seed](ieee1516-2010-normative-obligations.json)
is preserved. The current review is in
[eight service/source slices](requirements2010/README.md) and the
[reconciled machine-readable record](ieee1516-2010-requirements-review-20261002.json).
The latter is not a certificate, a complete atomic obligation inventory, or a
claim that every linked test selected all parameter variants.

## Accounting

| Artifact | Current count | Meaning |
|---|---:|---|
| Supplied PDF pages processed | 793 | All extracted page identities and PDF hashes checked |
| Candidate review units | 1,856 | Recognized clauses, captions and normative-annex boundaries |
| Ambiguous/repeated candidate units | 36 | Includes continued tables and repeated headings; requires review |
| Direct-body decomposition declarations | 210 | Does not include descendants or referenced contracts automatically |
| Requirement review records | 712 | Mix of atomic and composite statements, not a complete denominator |
| Source-reviewed records | 706 | Source statement inspected; not a fulfillment verdict |
| Candidate records | 6 | Interpretation still unresolved |
| Applicable / excluded / unresolved | 639 / 48 / 25 | RTI-only assessment scope, not arbitrary model conformance |
| Requirement-to-oracle links | 318 | Reuse across requirements is intentional |
| Distinct literal test declarations linked | 206 | Tests are not independent normative obligations |
| Records without linked oracles | 489 | Verification gaps, not 489 reproduced implementation defects |

All 318 links resolve to existing declarations and matching passing local
execution records. Invocation identities are retained. Their status is
`passing-records-only-completeness-unassessed`: only the imported invocations
are credited. No whole-method, source-stability or complete semantic verdict
is inferred. All applicable semantic verdicts remain open.

All recorded clause/bullet/range/caption locators reconcile with the candidate
index. This is a locator check, not independent proof of source interpretation.
The 844 open-item notes can overlap; they are not a count of missing features.

| Review slice | Records | Oracle links | Direct-body declarations |
|---|---:|---:|---:|
| Framework | 16 | 6 | 9 |
| FM, synchronization, save/restore | 381 | 60 | 90 |
| DM and OM | 92 | 58 | 35 |
| Ownership and DDM | 29 | 48 | 5 |
| TM and mapping | 33 | 45 | 7 |
| Support, MOM and claim scope | 47 | 39 | 9 |
| Java and normative resources | 69 | 33 | 44 |
| OMT and FDD | 45 | 29 | 11 |

## Source Handling

One-based physical PDF pages identify the supplied redlines. The registered
SHA-256 values were rechecked against all three PDFs. Extraction hashes are
recorded separately; correct hashes do not make an extraction authoritative.

| Source | Pages | SHA-256 |
|---|---:|---|
| S1516 | 41 | `6ac0b36add984f2f2e772b8c070f75e8172fd1cd3ab55dd65defe3066e46b086` |
| S1516.1 | 588 | `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4` |
| S1516.2 | 164 | `e0ec65cb99b9a9e47cb21cd1d003b78df76e18cddd5ab2f0fd1e3a60e5223f09` |

The private projection excludes exact RGB-red characters. It is a candidate
locator aid: inserted/retained text, figures, malformed layout and apparent
contradictions need visual resolution. Candidate heading recognition may miss
content. Normative API declarations and XSD elements are not fully decomposed
by recognizing a single annex boundary. No full text, PDF or raw execution log
was copied into the repository.

A bounded official-domain search checked the
[IEEE 1516](https://standards.ieee.org/ieee/1516/3744/),
[1516.1](https://standards.ieee.org/ieee/1516.1/3745/) and
[1516.2](https://standards.ieee.org/ieee/1516.2/3746/) records and searched for
2010 errata/corrigenda. It did not locate a specific applicable correction.
This does **not** establish that no correction exists. The located
[1516.1-2000 correction sheet](https://standards.ieee.org/wp-content/uploads/import/documents/erratas/1516.1-2000_errata.pdf)
is a different edition and was not applied as a 2010 correction. Reconciliation
of the source ambiguities remains open; the target has not changed to 2025.

## Plan, Do, Review, Reflect

### Plan

Use the full supplied source set rather than API names or the historical seed
as the coverage denominator. Separate RTI, federate, federation and model-tool
responsibilities. Decompose direct service bodies, preconditions, effects,
exceptions, callback triggers, tables, mappings and encoding rules. Record a
specific inspected oracle and its limitations for each linked test.

### Do

Six concurrent service reviews produced eight disjoint slices. The
[reconciliation tool](../../scripts/ieee2010_requirements_review.py) checks
page continuity, hashes, safe paths, unique IDs, declarations and local results.
It reads the previous final Go/Python/Java campaigns described in the
[defect-correction report](ieee1516-2010-defect-corrections-20261002.md), plus
the focused new Java evidence below. Those broad campaigns were not rerun in
this increment and are not added again to a cumulative pass total.

### Review

Separate reviewers checked source responsibility, clause scopes and the
reconciliation logic. Corrections included:

- Restored application memory and undefined failed-restore state belong to the
  federate, not the RTI. Ten application-only FM records are excluded from this
  RTI claim; thirteen callback-error records remain unresolved until the
  selected SDK propagation responsibility is derived.
- A reviewed parent means its direct body only. Unassessed descendants and
  external SISO contracts remain separate.
- Same-page sibling headings now stop candidate spans at their actual line.
  Sibling ranges include interior clauses; unsupported ranges cannot receive a
  complete locator match.
- Qualified Java annotations, annex aliases and mixed `7/Table 50` locators
  are handled. Table 50 now has seven linked review records.
- Executed parameter identities are retained. Missing variant selection cannot
  be interpreted as a whole-method pass; failing/skipped variants are not
  credited. An empty/candidate-only/unrelated decomposition cannot mark a unit
  reviewed.
- Existing Float64 overflow/negative-result assertions are now traced separately
  from endpoint construction and ordinary representable epsilon arithmetic.

The new reconciliation safeguards pass twelve tool tests; the existing seed
traceability tests pass eighteen. These thirty tests validate evidence handling,
not thirty HLA requirements.

### Reflect

A source review exposed a real mismatch hidden by an existing contrary test
oracle. This shows why implementation tests cannot define the standard.
Recorded passes are useful evidence, but closure still requires complete source
decomposition, applicability review and condition-specific assertions. No
automatic conformance percentage is computed from the counts above.

## Corrected Java Synchronization Behavior

S1516.1 Clause 4.11, physical PDF page 59, was visually rechecked. An omitted
or explicitly empty optional participant set informs all joined members. Java
previously rejected the explicit-empty case, and its old test expected that
rejection. Only explicit-null input remains rejected by the product guard.

The correction is in
`javasdk/client/src/main/java/org/gorti/rti/SynchronizationServices.java`.
The focused `SynchronizationServicesTest` first reproduced the contradiction,
then passed four cases with the corrected oracle. Tag copy, unauthorized
achievement and explicit-null controls are retained.

A new `LiveExchangeTest` case,
`explicitEmptyParticipantsSynchronizeBothEvokedMembersWithImplicitControl`,
passes against the unchanged production binary. Two real joined Java members
exercise explicit-empty and omitted-set registration. Four actual announcement
callbacks preserve exact tags; four actual synchronized callbacks carry empty
failure sets. Callback invocation is EVOKED, non-inline and on the caller
thread. Three evocation rounds before the second achievement do not complete
either point. Cleanup resigns both members and destroys the federation.

| Fresh selected execution | Result | Boundary |
|---|---|---|
| Focused Java unit/in-process suite | 4 pass, 0 fail/error/skip | Requests and local guards |
| Production Java synchronization case | 1 pass, 0 fail/error/skip | Two members, two labels, actual callbacks |

Evidence remains outside Git:
`D:/workspace/gorti/java-sync-empty-tdd-20261002-1415/` and
`D:/workspace/gorti/java-sync-empty-live-20261002-1335/`.
The owned server was stopped. Its SHA-256 stayed
`24b9c82590543159e6234c8dc7d473b6ee1699329c34677d1779035aad115b05`.
Newly edited Java source is not covered by the historical 535-case total merely
because that earlier campaign passed. Late joins, save/restore and all error
conditions for this synchronization service remain separate evidence facets.

## Remaining Internal Work

| Priority | Scope | Required next evidence |
|---|---|---|
| 1 | Complete source inventory | Remaining operative paragraphs, all tables/charts and API/schema contracts; resolve six candidate statements and ambiguous locators |
| 1 | Service conditions and exceptions | Positive/negative, unchanged-state and callback-window controls for unlinked FM/DM/OM/Ownership/DDM/Support facets, through selected Java and product interfaces |
| 1 | Time management | Complete sent/received order matrices, all request modes, finite endpoint service behavior and callback-entry transitions |
| 1 | Save/restore and teardown | Persistent queued-message recovery, fault/race outcomes and final-resigner object cleanup under public service contracts |
| 2 | MOM | Exact Table 6/7 payloads, NULL/empty responses, DDM regions, extensions, prohibited mutations and independently decoded service reports |
| 2 | FDD/OMT | Naming rejection, datatype categories, dimension semantics and independent schema/model admission controls |
| 2 | Selected Java mapping | Complete declared exceptions, overloads, callbacks, copy/reference behavior, URL restrictions and referenced SISO contracts |
| 2 | Source ambiguities | Custom-factory admission/support scope, Float64 factory wording and MOM source/MIM discrepancies with applicable corrections |

These are verification scopes, not a declaration that every corresponding
feature is missing. In particular, unknown custom-selector rejection does not
prove that a valid installed custom factory must be accepted, nor establish an
implementation defect. Full-model descriptive OMT merging rules are not
blindly imposed on the RTI's narrower FDD combination rules.

Internal inventory and semantic qualification remain open independently of
external assessment. External certification cannot replace completing this
internal evidence, and this increment does not use the certified conformance
label.
