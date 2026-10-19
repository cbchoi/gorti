# IEEE 1516-2010 Internal Normative Coverage Audit

Date: 2026-10-02. Repository: `D:/workspace/gorti/gorti`.
Observed branch: `codex/ieee2010-feature-completion`.
Observed HEAD: `518cb67f00858fcbec9ed86145d5b2360cfb2241`.

## Verdict And Boundary

**Full internal RTI conformance is not established. G0 applicability/inventory and G3 obligation-level semantic coverage remain open.** This is an independent internal, read-only assessment of the coverage argument, not external assessment, certification, or a new runtime campaign. The extensive pre-existing dirty tree was preserved. This report is the auditor's only repository write.

The intended assessed subject is the **RTI**, including its selected standard Java mapping, not arbitrary user federates, federations, simulation models, or an unclaimed full OMT-authoring tool. An RTI-only assessment must not require proof that arbitrary federates save application memory correctly, maintain correct simulation state, have complete SOMs, or obey a particular federation's application time policy. It must still assess the RTI's service behavior, required model admission/combination, ownership enforcement, saved RTI state, and callbacks.

Here, **seed20** means the 20 rows in `ieee1516-2010-normative-obligations.json`. No separately named seed20 test campaign or manifest was identified in the targeted searches. Passing tests selected from this seed cannot establish full coverage: the seed expressly excludes completeness, includes non-RTI responsibilities, and contains no atomic service coverage for entire normative families. This report identifies bounded missing scopes; it does **not** invent a complete inventory or an exact total of all missing IEEE obligations.

The main executor supplied these latest results during this audit:

| Reported execution | Result | Coverage interpretation |
|---|---|---|
| Windows Go | 86 packages; 3,240 top-level passes; 9,669 test/subtest events; no behavioral failures/skips | Main's reported execution, not independently rerun here; test counts are not obligation counts |
| Linux HLA and delivery race selection | 1,030 top-level passes; 2,551 events; no races | Useful selected concurrency evidence, not complete normative or Java coverage |
| Current bounded manifest | 226 groups and 985 literal references all found passing in the Go-all run | Required regression references executed; not 985 distinct IEEE obligations |
| Selected source roots | 2,348 files stable | Main's reported selected-root stability, not a complete release identity or a completeness proof |

The existing `D:/workspace/gorti/internal-full-readiness-20261002.json` was read, not rerun. It records `FAIL`, `commands_started: 0`, `conformance_established: false`, and **43 metadata messages**. They are not 43 defects or 43 missing standard services. In particular, that check compares engineering regression IDs with an empty normative-ID list and consequently reports bookkeeping mismatches and absent evidence.

## Findings

| ID | Finding | Classification |
|---|---|---|
| A1 | The seed explicitly has `coverage_complete: false`; all 20 rows retain open items. The main evidence ledger also has incomplete coverage, an empty normative requirement list, and no normative review reference. | Confirmed G0 evidence gap, not proof that 20 implementations fail |
| A2 | The selected standard Java mapping has no complete source-obligation-to-oracle-to-execution assessment. The two linked Java time tests check discovery/enumeration, not complete runtime time semantics. Product Go/Python evidence cannot establish all Java exception, overload, callback, and mapping behavior. | Confirmed G3 coverage gap; not a finding that all Java services are defective |
| A3 | The seed mixes RTI, federate, federation, and OMT-tool responsibilities. Treating every row's current `applicable` flag as an RTI requirement would improperly demand arbitrary user SOM/state/policy conformance. Treating every federation-labelled row as irrelevant would improperly discard ownership enforcement and relevant model admission. | Applicability/disposition gap requiring decomposition, not wholesale inclusion or exclusion |
| A4 | The at-least-one-FOM creation requirement is not a separate seed obligation. A current repository shortcut returns an empty model when no FOM and no explicit MIM are supplied; the inspected Java entry point rejects a null array, but not a zero-length array. | Source-confirmed contradictory ingestion path requiring focused standard-interface assessment; no new live reproduction here |
| A5 | The regression ledger's 79 rows, including engineering guards, are not the normative inventory. Its empty `independent_evidence` array does not erase separately reported black-box work, but that work has not been integrated as complete obligation-level evidence. | Record-model/traceability gap; no metadata-only promotion is justified |

No new end-to-end behavioral defect was reproduced in this audit. Historical matrix defects are not automatically current defects. Later Integer64, DDM, callback recovery, MOM, support-name, and SDK lifecycle corrections were considered when interpreting earlier reports.

## Normative Baseline And Scope Decisions

Source IDs below use **one-based physical PDF pages**, not printed page numbers. The supplied redlines were hash-checked. Retained/added 2010 text governs; deleted text is not an obligation. Raw extraction is only a locator aid, and redline ambiguities and applicable corrections still require targeted resolution.

| Source | Local supplied file | Pages | SHA-256 rechecked |
|---|---|---:|---|
| S1516 | `C:/Users/cbchoi/Downloads/1516_2010-2000_redline.pdf` | 41, registered source count | `6ac0b36add984f2f2e772b8c070f75e8172fd1cd3ab55dd65defe3066e46b086` |
| S1516.1 | `C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf` | 588, also checked by PDF reader | `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4` |
| S1516.2 | `C:/Users/cbchoi/Downloads/15162_2010-2000_redline.pdf` | 164, registered source count | `e0ec65cb99b9a9e47cb21cd1d003b78df76e18cddd5ab2f0fd1e3a60e5223f09` |

The following pages were visually inspected using retained rendered pages in `D:/workspace/gorti/ieee2010-source-intake-20260921`: S1516.1 pp.38, 48, 52, 339, 390 and S1516.2 p.82. S1516.1 pp.325, 338, 339, 390 were also extracted in memory. Other clause/page locators below are inherited from the existing source-reviewed inventory, normative review, or readiness audit and are explicitly **not newly reviewed in full here**.

| Decision | Basis | RTI-only consequence |
|---|---|---|
| At least one standard language API; complete services and RTI-initiated callbacks | S1516.1 13.2, PDF 390, visually rechecked | Java can be the assessed mapping. Go/Python do not replace it. C++ and Web Services are not automatic prerequisites merely because their APIs exist. Assess separately advertised conformance claims separately. |
| EVOKED-only is a valid callback-model choice | S1516.1 4.1, PDF 38, visually rechecked; Connect details 4.2, pp.50-51 inherited | Do not add IMMEDIATE as a missing mandatory implementation. Verify EVOKED behavior and the rejection of an unsupported requested model. |
| Both standardized time representations remain required | S1516.1 12.3, PDF 325, extracted; 12.4/Table 18, pp.338-340, with p.339 visually rechecked | Do not accept a float-only full-RTI exclusion. Factory availability is narrower than runtime composition, encodings, endpoints, epsilon, and arithmetic. Do not reopen historical Integer64 absence as a current defect. |
| Creation requires FOM input and default or selected MIM handling | S1516.1 4.1.4, PDF 48, and 4.5, PDF 52, visually rechecked | Required ingestion behavior is an RTI obligation even though authors own model-content correctness. Missing-FOM admission is separate from default-MIM availability. |
| Model, OMT-tool, and XML-product conformance are distinct subjects | S1516.2 6.1.1-6.1.3, PDF 82, visually rechecked | No full OMT-tool claim means tool-only rows can have reviewed exclusions. RTI FDD/MIM/model obligations remain. Relaxed FDD schema validation is not full OMT conformance. |
| Internal assessment is not the standard's public conformance label | S1516.1 13, PDF 390, visually rechecked | Lack of external certification is not a reason to stop this internal audit, but neither seed20 nor this report authorizes the label "HLA Conforming". |

The Java API was independently compared against the restored, hash-pinned supplemental archive at `D:/workspace/gorti/ieee2010-source-intake-20260921/1516.1-2010_downloads-restored.zip`. Its SHA-256 is `e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1`. The nested Java ZIP hash is `66485e053b6183df2df94e35de7c609677f2ccaef0830edde014aad066e41890`. All 204 Java source files were found. Only `RtiFactoryFactory.java` and `LogicalTimeFactoryFactory.java` differ, matching the documented service-loader adaptations. This is artifact identity/provenance evidence, not loader acceptance, execution, or semantic conformance.

The current `service-coverage.tsv` contains 172 `MAPPED` signatures. The actual `RTIambassador`, `FederateAmbassador`, and logical-time interfaces were inspected for service/callback/helper surfaces, including declared exceptions and parameter-copy conventions. A signature census cannot enumerate service preconditions, effects, invalid-state behavior, callback triggers, allowed partial orders, tables, encodings, or inspection-only obligations. No conformance percentage is derived from 172, 204, 20, or the runtime test totals.

## Exact Current Record Accounting

| Record | Observed accounting |
|---|---|
| Normative seed | 20 rows: 5 S1516, 12 S1516.1, 3 S1516.2 |
| Seed subjects | 12 RTI, 3 federation, 3 federate, 2 OMT-tool |
| Seed applicability | 17 marked applicable; 3 unresolved; no reviewed not-applicable rows |
| Seed links | 12 rows with test links; 8 with none; 21 literal path/name links in total |
| Seed closure | All 20 have nonempty open items. These are source links/statuses, not 20 closure verdicts. |
| Main regression evidence | 79 requirement rows; 78 profile-required rows; 226 test groups; 37 rows have no clause value |
| Main normative record | `coverage_complete: false`, `requirement_ids: []`, `review_evidence: null`, `independent_evidence: []` |
| Main unresolved example | `Q01-normative-claim-scope`, with unresolved applicability and no tests |

The three unresolved seed rows are the two OMT-tool rows and `RTI-FULL-CLAIM-SERVICE-SCOPE`. The internal RTI intent is now explicit, but the record still needs a reviewed profile and responsibility disposition. Do not count the 17 applicable flags as 17 applicable **RTI** obligations.

The main ledger's 37 empty clause values include engineering descriptors, architecture checks, persistence guards, and broader regression IDs. They are not automatically 37 standard violations. Likewise, a `resolved` regression status means the selected engineering issue has evidence; it is not a complete containing-clause verdict.

## RTI-Only Applicability And Seed20 Remainders

Every row below remains open in the current record. "Not an RTI author obligation" removes inappropriate user-product assessment, not the RTI's separately derived service responsibilities. Source locators are the seed's already source-reviewed locators unless specifically rechecked above.

| Seed ID | Source locator | RTI-only disposition and concrete remaining evidence |
|---|---|---|
| `FW-FEDERATION-FOM` | S1516 5.1, PDF 33 | Federation authors own complete exchange-agreement/FOM documentation. Do **not** require arbitrary author-model correctness for RTI closure. Separate the RTI's required model admission, FDD construction, and MIM/module handling under S1516.1/S1516.2. No seed test is linked. |
| `FW-FEDERATE-ATTRIBUTE-VALUES` | S1516 5.2, PDF 33 | Federate simulation state is outside RTI-only assessment. RTI routing, queued data, MOM attributes, and saved RTI state are not automatically forbidden simulation-state maintenance. No application-state oracle is required for this RTI claim. |
| `FW-ATTRIBUTE-AT-MOST-ONE-OWNER` | S1516 5.5, PDF 34 | Federation-labelled invariant with an applicable RTI enforcement surface. One local queued-transfer case does not cover competing acquisitions, divestiture/cancellation, resign, delete, or restore. Need independently reviewed state/callback oracles through the standard binding for applicable transitions. |
| `FW-FEDERATE-SOM` | S1516 6.1, PDF 34 | User SOM completeness is outside RTI-only assessment. Do not turn an absent user-SOM test into an RTI defect. Assess any specifically claimed example federate separately. |
| `FW-FEDERATE-TIME-POLICY` | S1516 6.5, PDF 35 | Federate adherence to its federation's application time agreement is outside RTI-only assessment. Keep RTI time-service requirements under S1516.1 Clause 8 and mapping/time-type requirements under Clause 12. |
| `IND-FM-DUPLICATE` | S1516.1 4.5.3(b), 4.5.5(g), PDF 53 | RTI obligation. Existing independent Python scenario exercises rejection and original-class usability. It does not establish exclusion of every competing module's contents or standard-Java exception/unchanged-state behavior. |
| `IND-FM-DESTROY-JOINED` | S1516.1 4.6.3(c), 4.6.5(a), PDF 54 | RTI obligation. Python rejection, continuing membership, empty destruction, and recreation are bounded evidence. Standard-Java translation and the remaining service conditions still need mapping/evidence. |
| `FM-DEFAULT-MIM` | S1516.1 4.1.4, 4.5, PDFs 48, 52 | RTI obligation. No test linked. Check actual standard MOM declarations through valid public creation, not a label/digest alone. Add missing-FOM admission as a separate obligation; the current shortcut is discussed below. |
| `FM-RESERVED-EXPLICIT-MIM` | S1516.1 4.5, 4.5.5(d), PDFs 52-53 | RTI obligation. Local pre-mutation regression is linked. Need standard-Java exception identity and public post-state control, without treating a generic wire rejection as complete Java evidence. |
| `IND-OM-DELETE-UNKNOWN` | S1516.1 6.14.3(e), 6.14.5(c), PDF 132 | RTI obligation. Python distinguishes undiscovered/nonowner/deleted instances and verifies later updates/removal. Need the selected Java mapping and other applicable deletion conditions. No universal exception precedence is inferred. |
| `SUPPORT-OBJECT-CLASS-NAME-ERROR` | S1516.1 10.6.3(d), 10.6.5(a), Annex B, PDFs 252, 439-440 | RTI obligation. Later Python/server NameNotFound repair and passing public controls are reported; the old SyncPointNotFound failure is historical, not a new current defect. Need standard-Java execution and the service-specific connected/joined/error-state assessment. |
| `IND-FOM-CONTRIBUTOR-PERSISTENCE` | S1516.1 4.1.4.1, PDF 49 | RTI obligation. Python fresh-member usability after all contributors resign is bounded evidence. Full combination rules, destruction/recreation isolation, and Java service behavior remain separate. |
| `FDD-TRANSPORT-DESCRIPTION-EXCLUDED` | S1516.1 4.1.4-4.1.4.1, PDF 48 | RTI obligation. Linked parser cases cover non-operational description differences. Need independent reliability-conflict and public create/join whole-call atomicity oracles. First-description retention is product policy, not the normative requirement. |
| `FDD-RATE-DESCRIPTION-EXCLUDED` | S1516.1 4.1.4-4.1.4.1, PDF 48 | RTI obligation. Need independently reviewed unequal exact-rate negatives and public admission atomicity. Rounded numeric equality and descriptive retention policy are not substitute oracles. |
| `TIME-INTEGER64-REPRESENTATION` | S1516.1 12.3, PDF 325 | RTI/selected-binding obligation. The linked test establishes factory discovery only. Add runtime composition, independent BE vectors, endpoints/arithmetic, exact values beyond 2^53, service/callback/retraction/restore paths. Separate Clause 12.4/Table 18 facets rather than assume they are already inventoried. C++ links are not prerequisites for a Java-only assessment. |
| `TIME-FLOAT64-REPRESENTATION` | S1516.1 12.3, PDF 325 | RTI/selected-binding obligation. The linked test enumerates factory names. It does not verify Float64 runtime behavior, finite final, epsilon/distinguishable arithmetic, encoding, or source-defined invalid-value behavior. These facets need separate locators/oracles; historical corrected endpoints are not declared broken. |
| `OMT-RATE-STRICTLY-POSITIVE` | S1516.2 4.11.2, 6.2.11(2), PDFs 60, 84 | Federation model-content requirement with an RTI ingestion consequence through S1516.1 4.1.4. Do not require correctness of every user model. Assess relevant accepted/rejected public inputs and failure atomicity; linked model/composition tests are not a complete independent Java admission oracle. Do not invent stricter numeric limits from positivity alone. |
| `OMT-TOOL-ACCEPT-CONFORMING-MODEL` | S1516.2 6.1.2, PDF 82 | Conditional on an OMT-tool conformance claim, not automatically an RTI requirement. Record a reviewed exclusion for the RTI-only claim if no such tool claim is made. FDD ingestion remains in scope separately. No tool corpus/test linked. |
| `OMT-TOOL-VALID-XML-OUTPUT` | S1516.2 6.1.2, PDF 82 | Same tool-claim decision. Relaxed FDD export checks do not prove full OMT-tool/XML-product conformance, but full OMT-tool verification should not be demanded for an unclaimed tool. Required RTI FDD exposure/export behavior needs its own rows. |
| `RTI-FULL-CLAIM-SERVICE-SCOPE` | S1516.1 13.2, PDF 390 | Applicable scope constraint for this internal RTI assessment; record is still unresolved. Split joined-federate services, RTI-initiated services, selected language mapping, and internal/public-label responsibilities. Do not require arbitrary user-federate correctness or external certification to perform internal coverage work. No executable case can alone prove completeness. |

The three federate-labelled seed rows can therefore be dispositioned outside the RTI-only claim with reviewed normative responsibility rationale. The two OMT-tool rows need a documented claim decision, not invented tool failures. The three federation-labelled rows need **decomposition**, not a blanket exemption. This audit does not edit those records or count these proposed dispositions as already reviewed closures.

## Outstanding G0 Coverage, Bounded By Known Sources

The required next work is a responsibility-aware extension of source review, not another method counter. The following absent or partial scopes are demonstrable from the seed, actual API surfaces, and existing verified locators. They are **review scopes**, not a completed inventory or a count of defects.

| Scope | Concrete gap in seed20 | Locator/review status |
|---|---|---|
| Edition, incorporated requirements, corrections, interpretation | Source hashes exist; no complete corrections applicability register, clause-by-clause disposition, or per-obligation reviewer argument is attached. The seed schema has `review_status` but no row reviewer/date. External reports could supply these, but the main review reference is null. | Quality-gates G0; existing normative review. Unreviewed remainder, not a finding that a particular unexamined correction applies. |
| Framework responsibility completion | Seed omits the reviewed framework exchange/interface and update/reflect, ownership-capability, and update-condition rules: 5.3, 5.4, 6.2-6.4. Separate their RTI-facing implications from federate duties. | S1516 PDFs 33-35, inherited from the framework responsibility map; not newly reviewed here. |
| FM, synchronization, save/restore | A few create/destroy/model-lifetime effects are seeded. Connection/disconnection/faults, list/join, six resign directives, synchronization state, save/restore status/failure/identity/persistent message obligations are not exhaustively represented. | S1516.1 Clause 4/API service locators; 4.1.2 pp.43-44, 4.10 pp.57-58, 4.24-4.27 pp.73-76 inherited. Full remainder unreviewed. |
| Declaration and advisories | No atomic Clause 5 rows: inherited declarations, active/passive effects, targeted removal including empty input, update-rate selection, and trigger/default/switch conditions. | S1516.1 Clause 5 and relevant Clause 10 services, actual Java API plus Q06/T17. Full text remainder unreviewed. |
| Object/transport/order/callbacks | Unknown-delete is one facet, not Clause 6. Reservations, registration, update/send/request/local-delete, knowledge visibility, transport confirmations, sent/received order, supplemental regions/producer, and retraction overload triggers need rows. | Actual Java RTI/Federate interfaces; 6.14 pp.131-132 inherited, Q05/T01-T04/T11. Full remainder unreviewed. |
| Ownership | A general one-owner rule is not a Clause 7 state/exception/callback inventory. All transitions, caller knowledge/publication, requested sets, tag provenance, cancellation, solicitation, atomic return sets, resign/delete/restore interactions need applicability/oracles. | S1516.1 Clause 7 and Java callbacks, Q02/T05-T08; 7.8-7.9 pp.167-170 inherited. Full remainder unreviewed. |
| Time | Representation availability is not Clause 8. Pending enables, each of five advance primitives, GALT/LITS, lookahead, asynchronous RO delivery, zero-lookahead constraints, message-before-grant, retraction, and delayed subscription evaluation need separate source facets. | 8.1 pp.182-191 and services 8.16-8.20 pp.211-214 inherited; 12.3/12.4 partly rechecked above. Full remainder unreviewed. |
| DDM/support/callback scheduling | No atomic Clause 9 coverage; support seed covers only one by-name error. Region template/specification lifecycle, defaults/exclusions, first commit, pairs/empty inputs, conveyed copies, callback-lifetime queries, normalization, switches, evoke/reentry and service-specific states remain. | 9.1 pp.220-229 and 9.3 p.230 inherited; actual Java Clause 9/10 services; Q09/T18/T20/T21. Full remainder unreviewed. |
| MOM | No atomic Clause 11/MIM behavioral rows. The later 32-implemented-leaf campaign is useful, but catalog count does not prove every applicable attribute, request/report, service, exception, target, partition, periodic update, region and encoding condition. | Clause 11, Annex G; 11.6/Table 6 p.297 and Annex G p.583 inherited. Full remainder unreviewed. |
| Shared/Java mapping and normative annexes | Handles/types, caller-data retention/returned values, factories, collections, encoder alignment/aggregate semantics, exceptions, callback serialization, utility contracts and Java mapping need source-derived review beyond service TSV. | Clause 12 and Annex B; 12.4 pp.338-340 partly rechecked; 12.6 p.340 and Java-specific 12.11 inherited. Normative annex identities checked in PDF contents, not complete annex texts. |
| Model rules consumed by RTI | Rates are one rule. FDD structure/context, namespaces/cardinality, inheritance, dimensions/defaults, exact operational merge fields, scaffolding completion, switches and custom MIM rules need responsibility-aware dispositions. Do not impose full author/tool content on minimum RTI FDD solely because it appears in OMT. | 4.1.4 pp.47-49, Annex F/G pp.582-583 inherited; S1516.2 6.1-6.2 pp.82-84 and Clause 7/Annexes B-E. Full remainder unreviewed. |

S1516.1's contents identify Annex B (Java), C (C++), D (Web Services), F (FDD schema), and G (MIM) as normative. Java selection makes B relevant, while C/D require an assessed-binding applicability decision, not automatic inclusion. S1516.2's contents identify B (normalization), C (module merging), D (interchange), and E (OMT schema) as normative; applicability depends on the RTI/model/tool responsibility and incorporated RTI requirements. Informative annexes, examples, and deleted redline passages must not be promoted to mandatory obligations. These contents checks do not establish complete annex coverage.

Q01-Q10 and matrix T01-T22 remain starting review groups/fixtures. In particular, the matrix's old "integer if offered" wording must be read with the later verified both-types requirement, and its IMMEDIATE fixture is relevant only if that model is assessed. The completion-audit IDs `G01`-`G23` are different local identifiers from quality gates **G0** and **G3**.

## Outstanding G3 Evidence

G3 closure requires an independently reviewed oracle for each applicable RTI facet, observed through the actual server and assessed binding. Existing local/domain/wire tests can support that argument but do not automatically substitute for the advertised standard interface.

The exact seed-link gaps are the eight rows without any linked tests: `FW-FEDERATION-FOM`, `FW-FEDERATE-ATTRIBUTE-VALUES`, `FW-FEDERATE-SOM`, `FW-FEDERATE-TIME-POLICY`, `FM-DEFAULT-MIM`, both `OMT-TOOL-*` rows, and `RTI-FULL-CLAIM-SERVICE-SCOPE`. **This is not an instruction to add eight runtime tests.** Several need RTI-only exclusion/decomposition or non-executable completeness review. Default-MIM behavior needs a public service oracle. The 12 linked rows still retain the narrower gaps listed above.

For the applicable RTI rows and newly reviewed scopes, retain positive controls, invalid-input/state cases, boundary cases, and relevant concurrent/failure cases. Verify resulting state, atomic rejected batches, exact source-required exceptions, callback identity/payload/tag, supplemental-data presence, selected overloads, and permitted partial orders. Use source-defined service states rather than a universal joined-only assumption. Do not infer universal exception precedence from a single negative case, demand one scheduling trace where multiple are legal, or turn test deadlines into IEEE response-time rules.

The original four independently specified Python scenarios are bounded black-box evidence, not independent standard-Java evidence. The later support-name repair adds useful negative-service coverage; the rejoin-cache fix is an engineering lifecycle repair, not a standard-mandated cache algorithm. The recent Java/Go/Python restore and MOM campaigns also strengthen their named scenarios. Their reports expressly do not claim all normative callbacks, all restore/cache conditions, or complete MOM permutations. This audit did not replay their raw runtime campaigns or issue new PASS verdicts.

An RTI-only fixture still needs lawful participating federates, a suitable model, and controlled application data to exercise services. Those are test preconditions and oracle inputs, not a claim that every possible user federate/federation conforms. Save/restore tests should independently preserve fixture application state as needed, while judging RTI persistence, identities, knowledge, queues and service outcomes separately.

## Source-Confirmed Path Versus Evidence Gaps

The current empty-FOM path deserves a bounded follow-up even while G0 is incomplete:

- S1516.1 4.5, PDF 52, visibly requires FOM input and default/selected MIM handling. The seed's `FM-DEFAULT-MIM.open_items` acknowledges that the at-least-one-FOM requirement is not inventoried.
- [AmbassadorServices.java](D:/workspace/gorti/gorti/javasdk/client/src/main/java/org/gorti/rti/AmbassadorServices.java:171) rejects `modules == null`; an empty array proceeds through the empty loop into creation.
- [federation.go](D:/workspace/gorti/gorti/rti/internal/adapters/grpc/federation.go:106) forwards converted modules; [manager.go](D:/workspace/gorti/gorti/rti/internal/hla/federation/manager.go:272) loads them without a visible nonempty-module guard in the inspected creation routine.
- [fom_repository.go](D:/workspace/gorti/gorti/rti/internal/application/composition/fom_repository.go:67) returns `model.NewFOM(nil, nil, nil)` before standard-MIM parsing/merging when both module inputs are absent.

This confirms the current local shortcut and the missing source-obligation/oracle linkage. It does not newly demonstrate a successful Java federation creation through every later setup hook. Classify the shortcut as a source-confirmed incompatibility in the inspected ingestion path, with a public Java negative case and valid-FOM/default-MIM control still needed. Do not invent a specific exception identity for empty arrays without reviewing the complete applicable exception/mapping conditions.

All other missing scopes listed in this report are **coverage/evidence gaps unless separately reproduced or source-confirmed**, not freshly established production failures. In particular, current passing Integer64/restore/DDM/MOM work must not be mislabeled absent because an earlier matrix or stale README paragraph described an older increment.

## Checked Files And Commands

All repository paths here are relative to the absolute repository root stated above. Checks were read-only until report creation.

| Checked material | Use/boundary |
|---|---|
| `engineering/verification/ieee1516-2010-quality-gates.md` | G0/G3 and claim policy |
| `engineering/verification/ieee1516-2010-normative-obligations.json` | All 20 statements/conditions/subjects/test links/open items and structured counts |
| `engineering/verification/ieee1516-2010-requirements-matrix.md` | Historical C/N corrections, T01-T22, Q01-Q10, evidence/responsibility distinctions; missing middle read separately |
| `engineering/verification/ieee1516-2010-evidence.json` | Structured profile/requirement/test/independent-evidence and normative fields |
| `engineering/verification/ieee1516-2010-normative-review-20260921.md`; `ieee1516-2010-readiness-20260929.md` | Existing verified locators, scope decisions, historical observations |
| `ieee1516-2010-traceability-20261001.md`; `ieee1516-2010-sdk-gap-fixes-20261001.md` | Independent Python boundaries and later corrections, not new execution |
| `ieee1516-2010-service-completion-20260929.md`; `ieee1516-2010-completion-progress-20261001.md`; `ieee1516-2010-callback-sdk-mom-20261001.md`; `ieee1516-2010-control-outcomes-20261001.md` | Later Java/time/MOM/callback/restore evidence and expressly retained limitations |
| `scripts/ieee2010_traceability.py`; `scripts/ieee2010_quality_gate.py` | Seed-completeness prohibition and metadata policy/record boundaries; not executed |
| `javasdk/README.md`; `javasdk/ieee1516-api/NOTICE.md`; `javasdk/service-coverage.tsv` | Binding claims, adaptations, mapping versus semantics |
| `javasdk/ieee1516-api/src/main/java/hla/rti1516e/{RTIambassador,FederateAmbassador,LogicalTime,LogicalTimeInterval,LogicalTimeFactory}.java` | Full lexical service/helper surface pass plus selected contracts; archive comparison of all 204 API sources; not full semantic review of each source |
| `javasdk/client/src/test/java/org/gorti/rti/types/Integer64TimeFactoryTest.java`; `pysdk/tests/test_ieee2010_independent_contract_live.py` | Direct oracle inspection: factory-only seed links and four actual public-client scenarios |
| `javasdk/client/src/main/java/org/gorti/rti/AmbassadorServices.java`; `rti/internal/{application/composition/fom_repository,hla/federation/manager,adapters/grpc/federation}.go` | Selected create-path inspection only |
| Three supplied PDFs; `D:/workspace/gorti/ieee2010-source-intake-20260921/{sources.json,1516.1-2010_downloads-restored.zip}` and selected page PNGs | Identity, selected current-text visual checks, annex contents; no exhaustive publication review |
| `D:/workspace/gorti/internal-full-readiness-20261002.json`; beginning of `internal-source-inputs-20261002.json` | Existing 43-message metadata result and selected-source scope; no new validation gate |

Commands actually used included `Get-Location`, `git status --short`, `git rev-parse HEAD`, `git branch --show-current`, targeted `rg --files`/`rg -n`, `Get-Content` with bounded `Select-Object` where needed, `Get-ChildItem`, `Test-Path`, `Get-FileHash`, and `ConvertFrom-Json`/`Group-Object`/`ConvertTo-Json` for structured counts. Read-only Python `-B -c` calls used `pypdf.PdfReader` for selected pages/contents and `zipfile`, `hashlib`, `io`, and `pathlib` for in-memory nested-archive comparisons and API-surface reads. The bundled interpreter was `C:/Users/cbchoi/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/python.exe`. The PDF skill was read before extraction. `view_image` inspected the selected existing rendered pages. No external web lookup was needed.

Representative exact read commands:

```powershell
git rev-parse HEAD
git branch --show-current
Get-Content engineering/verification/ieee1516-2010-quality-gates.md
Get-Content engineering/verification/ieee1516-2010-normative-obligations.json -TotalCount 180
Get-Content engineering/verification/ieee1516-2010-requirements-matrix.md
Get-Content engineering/verification/ieee1516-2010-requirements-matrix.md | Select-Object -Skip 198 -First 147
Get-Content scripts/ieee2010_traceability.py | Select-Object -Skip 285 -First 65
Get-Content D:/workspace/gorti/internal-full-readiness-20261002.json -Raw | ConvertFrom-Json
Get-FileHash C:/Users/cbchoi/Downloads/1516_2010-2000_redline.pdf,C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf,C:/Users/cbchoi/Downloads/15162_2010-2000_redline.pdf
```

Operational limitations: broad file searches encountered pre-existing inaccessible legacy directories, including `engineering/task-rl-106-vxunp6cq`; targeted authoritative-file reads succeeded. No completeness claim is based on the broad search. One initial PDF extraction could not print a Unicode character under the shell's default encoding; it was rerun successfully with UTF-8 stdout. The old temporary supplemental-archive paths did not yield a Java ZIP; the restored, hash-matching archive above was used instead. No runtime test, automated smoke test, freeze-state gate, production edit, manifest edit, commit, or history rewrite was performed by this auditor.

Read-time SHA-256 identities, not a full dirty-tree snapshot:

| Repository file | SHA-256 |
|---|---|
| `ieee1516-2010-normative-obligations.json` | `67e6690b42db00a85a3dbc1d2999ce09127daa11bb18db45dfb7d5812114b6ec` |
| `ieee1516-2010-evidence.json` | `3e5d484714a51d763f6657c49396430a5fab6ddd87bb04f3e6c31cf4ce36feed` |
| `ieee1516-2010-quality-gates.md` | `e30f71c1cba47520076895a88b49410193d0b5185db648cbd8b6f351184f2da8` |
| `ieee1516-2010-requirements-matrix.md` | `6e48c35c83ebe42a38f32927ab9fa13b8b59bff32cc1e4b7184c0ba98688e86e` |
| Standard `RTIambassador.java` | `e110a9d2d433d36e257f22a0cd22219a687bb04a4b9db3f29fde78489a5103d9` |
| Standard `FederateAmbassador.java` | `c7d53b17248606ea80684480544d2fe13c6afa189675f60c4d414c52554d3767` |

## Bounded Next Disposition

1. Record the RTI-only responsibility split above, including reviewed author/federate/tool exclusions and retained RTI-facing facets. Keep external conformance-label assessment separate from internal verification.
2. Keep the engineering ledger and normative obligation inventory distinct. Link regression evidence to reviewed normative facets instead of copying ENG IDs into a purported full inventory or clearing incomplete-coverage flags.
3. Add and independently exercise the missing-FOM/default-MIM public Java admission oracle. Then extend source-derived Java coverage through the known missing service/callback/model families, retaining relevant existing Go/Python/race evidence as support.
4. Report passing named internal subsets until complete applicable source review and obligation-level evidence exist. Seed20 can support that work; it cannot establish full internal RTI conformance, even if all its current test links pass.

This report resolves the bounded coverage/applicability question. It neither asserts a complete inventory nor blocks ongoing research or useful partial service verification on staging checks.
