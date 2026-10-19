# IEEE 1516-2010 Source-Based Review

Date: 2026-09-21. Target: the 2010 editions of IEEE 1516, IEEE 1516.1
and IEEE 1516.2. Development branch: `codex/ieee2010-feature-completion`.

Follow-up: [Python-first behavioral corrections](ieee1516-2010-python-behavior-20260921.md)
records the subsequent time-delivery, LITS, callback metadata, update-rate and
additive regional-subscription fixes. The findings and counts below remain the
historical first source-review checkpoint, not the current implementation verdict.
The [next implementation batch](ieee1516-2010-first-batch-20260921.md) then
corrects lookahead modification/reduction, complete-class admission, interaction
region subscriptions and divestiture preconditions. Consult that record for the
latest execution and remaining-work boundary.

## Sources And Method

The user supplied the three IEEE redline publications below. They include the
2010 document text and mark changes from earlier editions. The absence of full
text recorded in the September 18 assessment no longer prevents clause-level
review. The publications identify the base publication as the official document;
an ambiguous redline passage therefore needs a specific base-copy check, not a
blanket suspension of the implementation audit.

| Source ID | Supplied filename | PDF pages | SHA-256 |
|---|---|---:|---|
| S1516 | `1516_2010-2000_redline.pdf` | 41 | `6ac0b36add984f2f2e772b8c070f75e8172fd1cd3ab55dd65defe3066e46b086` |
| S1516.1 | `15161_2010-2004_redline.pdf` | 588 | `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4` |
| S1516.2 | `15162_2010-2000_redline.pdf` | 164 | `e0ec65cb99b9a9e47cb21cd1d003b78df76e18cddd5ab2f0fd1e3a60e5223f09` |

Source IDs denote the actual 2010 document titles, not an edition inferred from
the local filename. Page locators in this report are **one-based PDF pages**.
The source PDFs, page images, extraction index and raw execution logs remain
outside the repository. This report retains paraphrases, locators and evidence
references, not copies of the standards.

The legend on PDF page 4 identifies red struck-through deletions and blue
underlined changes/additions. Extraction excludes red characters to make a
searchable candidate text; it does not produce an authoritative clean edition.
Requirements used below are checked against rendered pages. Informative notes,
examples and annexes are not promoted to mandatory service requirements.
Applicable corrections still need a separate applicability check.

Review proceeds in four steps: locate a current requirement, inspect the
implementation and its oracle, reproduce a bounded defect before changing it,
then rerun relevant tests and reconsider the wider requirement. A passing case
closes its asserted behavior, not its entire service group.

## Scope Decisions

| ID | Source and locator | Reviewed decision | Implementation consequence |
|---|---|---|---|
| NS01 | S1516.1, 13.2, PDF 390 | The RTI implements all specified joined-federate-initiated services and invokes the specified RTI-initiated services. At least one language-specific API is required; mapping follows Clause 12. | Complete the selected standard binding and service semantics. C++, Java and WSDL are not three universally required simultaneous deliverables. Go/Python product bindings do not establish the standard C++ mapping. |
| NS02 | S1516.1, 13, PDF 390 | The label "HLA Conforming" requires publicly available results certified by a recognized authority. | An internal campaign reports its scope and results, not that label or certification. External assessment availability is a separate inquiry. |
| NS03 | S1516.1, 13.1, PDF 390 | A federate's service-use declaration belongs in its SOM conformance table. | An RTI test suite alone does not establish that every example or user federate satisfies its own obligations. |
| NS04 | S1516.2, 6.1.1-6.2, PDF 82 | Model content, OMT-tool behavior and RTI use of models have different responsible subjects. XML validation is not the whole model-conformance assessment. | Separate model-author/tool requirements from the RTI's FDD, module, MIM and runtime obligations; do not infer an RTI requirement to reproduce arbitrary source formatting. |

## Framework Responsibility Map

These ten rows are an applicability review of the framework rules, not ten
executed service tests. The implementation column identifies where more detailed
1516.1/1516.2 obligations are checked. It does not declare a complete rule passed.

| ID | S1516 clause / PDF page | Requirement and responsible subject | gorti verification surface |
|---|---|---|---|
| NF01 | 5.1 / 33 | Federation authors document the exchanged data and conditions in an OMT FOM, composed of one or more FOM modules and one MIM. | `rti/pkg/fom/` and `rti/internal/application/composition/` consume the model; each experiment must supply a valid federation model. |
| NF02 | 5.2 / 33 | Federates maintain simulation-associated object representations and values; the RTI may use data for services and own federation MOM attributes. | Distinguish routing, retained callback/checkpoint data and MOM state from executing or changing simulation model behavior. |
| NF03 | 5.3 / 33-34 | Federates exchange FOM data via the RTI. The RTI coordinates and delivers according to declared requirements; federates remain responsible for substantive data correctness. | Object, declaration, DDM, time and delivery tests verify RTI behavior. An RTI cannot validate arbitrary domain-model truth. |
| NF04 | 5.4 / 34 | Joined federates use the HLA interface to access RTI services. | Assess the standard C++ binding separately from gorti-specific Go/Python APIs and the internal gRPC transport. |
| NF05 | 5.5 / 34 | At most one joined federate owns a particular instance attribute at a time; ownership can transfer during execution. | `rti/internal/hla/ownership/` and object-service atomicity/cleanup tests; do not confuse publication with ownership. |
| NF06 | 6.1 / 34 | Federate developers document available information in a SOM consisting of one or more modules and an optional MIM. | SOM completeness is a federate artifact check, not a missing server operation. |
| NF07 | 6.2 / 34 | Federates provide update/reflect and send/receive capabilities as declared in their SOMs. | Test actual callbacks and payloads for the advertised SDKs and examples; server method presence alone is insufficient. |
| NF08 | 6.3 / 35 | Federates support dynamic ownership transfer/acquisition as specified in their SOMs. | Ownership service/callback tests support the RTI side; a user's federate implementation has a separate responsibility. |
| NF09 | 6.4 / 35 | Federates can vary update conditions as specified in their SOMs. | This rule alone does not require RTI-side automatic throttling. Assess RTI update-rate services against their own 1516.1 clauses. |
| NF10 | 6.5 / 35 | Federates manage local time consistently with the federation's chosen time-management approach. | Verify all applicable RTI time services without treating one experiment's TAR choreography as a universal federation policy. |

## Corrections

Each correction below had a failing behavioral test before its implementation.
The source references identify the specific rule, not a claim to have completed
the entire containing clause. Existing unrelated working-tree changes were
preserved; no history rewrite, commit or generated-protocol edit was made.

| ID | Rule and source | Correction | Focused evidence |
|---|---|---|---|
| FIX-01 | Transportation FDD combination, S1516.1 4.1.4.1, PDF 48 | Equal name/reliability no longer conflicts solely because descriptions differ. Retain the first supplied description, filling an absent value later. | `TestFDDTransportationDescriptionsDoNotRejectMerge`; operational reliability conflicts still reject. |
| FIX-02 | Update-rate FDD combination, S1516.1 4.1.4-4.1.4.1, PDF 48 | Equal name/exact maximum rate no longer conflicts solely because descriptions differ. Preserve exact decimals and empty/absent metadata. | `TestFDDUpdateRateDescriptionsDoNotRejectMerge`, `TestFDDTransportRateMIMMergeUsesOnlyOperationalFields`; unequal rates still reject. |
| FIX-03 | Explicit MIM designator, S1516.1 4.5 and 4.5.5(d), PDF 52-53 | Reject the reserved exact designator `HLAstandardMIM` before SDK/RPC/repository/federation side effects. `HLAstandardMIM.xml` remains a different allowed designator. | Four pre-mutation regressions and `TestIEEE2010FederationMIMAndImmediateJoinedViewRealWire`. Default MIM selection and V1/V2 compatibility are unchanged. |
| FIX-04 | Outgoing TSO during advance, S1516.1 8.1.7, PDF 189 | Use requested logical time while an advance is pending; after an early grant, use the actual granted time again. | `TestOutgoingTSOUsesRequestedTimeWhileAdvancing` covers all five modes; `TestOutgoingTSOBoundEasesAfterEarlyNextMessageGrant` covers release. Positive-lookahead time-base selection only. |
| FIX-05 | Float64 epsilon, S1516.1 12.3, PDF 325-326; 12.12.6.24, PDF 377 | Use the smallest positive binary64 interval rather than machine precision at 1.0. Factory, predicate and encoding agree. | Shared `float64_epsilon.cpp` failed, then passed against both modern and official-header strict foundations. Arithmetic monotonicity is a separate unresolved defect. |
| FIX-06 | Deletion knowledge, S1516.1 6.14.3(e), 6.14.5(c), PDF 132 | Unknown joined callers receive the unknown-instance error before privilege checking. Preserve a narrow internal-owner path for retiring RTI-owned MOM instances. | `TestIEEE2010ObjectDeleteKnowledgeRealWire`, known-nonowner and MOM teardown regressions; public sentinel misuse and direct deletion of a federate-owned object remain rejected. |
| FIX-07 | Repeated ordinary acquisition, S1516.1 7.8, PDF 167-168 | Keep an already-pending request and its original tag without duplicate release/confirmation callbacks; process new attributes in a mixed request. | `TestIEEE2010OwnershipRepeatedAcquisitionRealWire`, ordinary/negotiated variants and 16 concurrent repeats. |
| FIX-08 | Acquisition-if-available precondition, S1516.1 7.9.3(h), 7.9.5(a), PDF 169-170 | Reject an already-pending ordinary acquisition before mutating any requested attribute. | `TestIEEE2010OwnershipIfAvailablePendingRealWire`; formerly returned success. |
| FIX-09 | Empty regional subscription, S1516.1 9.8 item (e), PDF 238-239 | Validate the request without replacing existing regional subscriptions or changing their active/passive settings. | `TestIEEE2010DDMRegionalEmptySetPreservesActiveRealWire`; nonempty additive semantics remain separate. |

The first-description retention choice in FIX-01/02 is an engineering policy;
the normative requirement is not to reject on those non-FDD differences. No
transport mechanism or throttling behavior was added by those changes.

## Federation And Model Review

Status vocabulary: **tested subset** means the named behavior ran, not that the
whole clause passed; **fixed** refers to the correction above; **gap** identifies
source-reviewed incompatible behavior; **open** needs a dedicated oracle. An
existing test can pass while deliberately asserting a behavior the standard
disallows. Such a result is not normative acceptance.

| ID | Source locator | Implementation / evidence | Current disposition |
|---|---|---|---|
| FM01 | S1516.1 4.5.3-4.5.4, PDF 52-53 | Federation manager and create readiness tests, including `TestCheckedCreateReviewJoinWaiterCannotJoinFailedExecution` | Tested subset: duplicate rejection, failed-setup isolation and readiness fencing. |
| FM02 | S1516.1 4.5, PDF 52; 4.1.4, PDF 48 | `composition.Repository.load`; empty-module fixtures exercised in server tests | Gap: standard-facing creation accepts no FOM modules; its omitted-MIM shortcut returns an empty model. The current clause requires module input and automatic standard MIM when no explicit MIM is supplied. Test compatibility is not a standards exemption. |
| FM03 | S1516.1 4.5.5(d), PDF 53 | SDK, federation adapter/manager and repository guards | FIX-03; named standard-exception propagation beyond `InvalidArgument` remains unassessed. |
| FM04 | S1516.1 4.6, PDF 53-54 | Destroy tests and `TestP0DestroyCleansComposedFederationState` | Tested subset: joined/nonexistent rejection, empty destruction and composed cleanup. |
| FM05 | S1516.1 4.9, PDF 55-56; 4.1.1, PDF 40-41 | Join naming, nonreused handles, readiness and real-wire FOM-view tests | Tested subset: explicit/automatic names, membership, immediate model access. Not every callback model or failure path. |
| FM06 | S1516.1 4.1.4.1, PDF 48-49; 4.9, PDF 55-56 | `TestJoinAdditionalFOMAtomicAdmission`, `TestLiveFOMExtensionLeaseAndStaleCAS`, production live-join test | Tested subset: new-class contribution, selected conflicts, stale preparation and atomic failures. |
| FM07 | S1516.1 4.1.4.1, PDF 49 | Real-wire contributor-resignation scenario | Tested subset: accepted FDD persists after its contributor resigns. The deleted old fresh-execution footnote on PDF 40 is not a reset requirement. |
| FM08 | S1516.1 4.1.4.2(a), PDF 49 | `TestSelectedMIMLiveGoSDKCreateJoinSaveRestoreAndMOM` | Tested subset: selected/default MIM reporting, current FDD and retained source/restore. Contributor-list filtering/deduplication remains open. |
| FM09 | S1516.1 4.10, PDF 57-58 | Resign manager tests and operation-admission tests | Tested subset: directives, membership removal, identity reuse and operation fencing. Complete six-directive cleanup/callback truth table remains open. |
| FM10 | S1516.1 4.10, PDF 57; 4.1, PDF 38 | Federation manager and application lifecycle resign flow | Gap: no last-member-specific object cleanup in the reviewed path. Add owned/unowned last-resigner cases while preserving execution-lifetime FDD. No new failing integration oracle in this pass. |
| FO01 | S1516.1 4.1.4-4.1.4.1, PDF 47-48; S1516.2 6.1, PDF 82 | Model, parser and FDD exporter | Responsibility resolved: RTI combination uses FDD; full OMT authoring/tool content is broader. Missing arbitrary descriptive roundtrip is not automatically an RTI violation. |
| FO02 | S1516.1 4.1.4.1, PDF 48 | Transportation/update-rate parser and MIM tests | FIX-01/02. Conflicting reliability and exact maximum rates still return an error with no partial model. |
| FO03 | S1516.2 4.11.2, PDF 60; 6.2.11, PDF 84; S1516.1 4.1.4, PDF 48 | `model.NewUpdateRate`, current parser/exporter fixtures | Gap: lexical/schema checks accept zero and negative rates. Existing green schema tests do not satisfy the strictly-positive semantic requirement. |
| FO04 | S1516.1 4.1.4.1, PDF 48 | `parser.ParseAdditive`; `TestLiveFOMExtensionPreservesEveryAssignedHandle` | Gap: dynamic joins can add attributes/parameters to an existing complete class. Stable handles do not make that changed member set admissible. Preserve new-class/subclass contributions when repairing this. |
| FO05 | S1516.1 4.1.4-4.1.4.1, PDF 47-48 | `TestCurrentFDDDescriptiveConflictingJoinIsInvisible` | Gap: other description-only differences, including object/interaction semantics and parameter descriptive whitespace, still reject joins. FIX-01/02 do not close these paths. |
| FO06 | S1516.1 4.1.4, PDF 48; S1516.2 MIM definition, PDF 23; Clause 7, PDF 91 | `mim.ParseSelected` / `compareSelectedTree` | Gap: the current structure-identical selected-MIM profile excludes user extensions. Semantics-only variant tests do not establish extension support. |
| FO07 | S1516.1 4.1.4.1, PDF 48-49; S1516.2 Table 50, PDF 92 | Parser class merge/completion | Open: final unresolved scaffolding must be distinguished from scaffolding completed by the same call's module set. No dedicated final-completion test executed. |
| FO08 | S1516.2 6.2.2-6.2.6, PDF 83-84; S1516.1 4.1.4.1, PDF 48 | Hierarchy, inherited-shadowing and dimension-merge tests | Tested subset: qualified identity, inherited member rejection, dimension consistency and references. Default dimension subrange arithmetic remains open; current validation is mainly lexical. |
| FO09 | S1516.1 4.1.4-4.1.4.1, PDF 48; S1516.2 Table 50, PDF 93 | Switch retention/export and automatic-resign model tests | Tested subset: modeled switch declarations/conflicts. All runtime switch behavior and partial-table combination are not established here. |
| FO10 | S1516.1 4.1.4, PDF 47-48; S1516.2 D.1-D.2, PDF 102-103 | Strict parser and FDD metadata tests | Gap/open coverage: local-name validation is not full namespace/context/cardinality/schema admission. Foreign namespaces, duplicate singletons and trailing document content need independent cases. Deleted DTD text was excluded. |
| FO11 | S1516.1 4.1.4.2, PDF 49; S1516.2 6.1.2-6.1.3, PDF 82 | Three independent FDD XSD oracle tests | Tested subset: retained FDD export, semantic mutations and exact decimal reconstruction. Not a complete OMT XML-product/tool assessment. |

## Time And MOM Review

| ID | Source locator | Implementation / evidence | Current disposition |
|---|---|---|---|
| TM01 | S1516.1 8.1.1-8.1.2, PDF 182-183; 8.1.7, PDF 189 | Time/delivery queues and SDK callback boundary | Scope resolved: count reflection, interaction and removal messages per recipient, not every callback envelope. Delivery/callback invocation, not callback return, is the relevant terminal. |
| TM02 | S1516.1 11.6 Table 6, PDF 297; Annex G, PDF 582-583; 8.1.7, PDF 189 | MOM `HLAROlength` / `HLATSOlength` sources; official MIM definitions | Gap: no complete production RO/TSO pending-message totals. Time-buffer size and observed-outbox diagnostics are partial stages, not substitutes. No end-to-end queue-count oracle ran. |
| TM03 | S1516.1 8.1.2 Table 2, PDF 183; 8.1.7, PDF 189 | `time/asyncdelivery.go`; existing async-drain tests | Gap: enabling asynchronous delivery bypasses/drains future TSO and relabels it RO. The standard permits asynchronous RO delivery, not premature TSO delivery. Existing tests assert the incompatible policy. |
| TM04 | S1516.1 8.1.7, PDF 189 | Outgoing TSO validator and two new regressions | FIX-04. Separate sampling of regulation/lookahead and advance state was not audited for linearizability. |
| TM05 | S1516.1 8.1.3, PDF 184 | Advance target validation/atomicity tests | Tested subset: invalid finite targets, monotonic request admission. Infinity acceptance is product behavior, not proof of standardized final-time semantics. |
| TM06 | S1516.1 8.1.5-8.1.6, PDF 185-188 | TAR/TARA/NMR/NMRA grant tests | Tested subset: requested/earlier grant predicates and peer waiting. Same-current-time queued messages and full FQR/message-before-grant guarantees remain open. |
| TM07 | S1516.1 8.1.4 and Table 3, PDF 184-188 | Outgoing validator, grant history and snapshots | Gap: unconditional inclusive comparison and fixed `1e-9` slack do not express zero-lookahead strict TAR/NMR constraints or persistence across same-time Available grants. FIX-04 does not repair this. |
| TM08 | S1516.1 8.1.5, PDF 185; 8.16, PDF 211 | Query GALT and MOM/grant calculations | Open consistency issue: query uses peers' current time/lookahead while grant logic uses pending request horizons. Needs a greatest-bound oracle with pending requests. |
| TM09 | S1516.1 8.1.5, PDF 185; 8.18, PDF 212-213 | Query LITS and MOM time metrics | Gap: buffer-only LITS omits the future-input GALT bound. No dedicated corrected LITS test was added. |
| TM10 | S1516.1 8.19-8.20, PDF 213-214 | Lookahead setter, pending advance state and snapshots | Gap: modification while genuinely advancing is not prohibited; reductions replace actual lookahead immediately instead of taking effect gradually. Existing test names do not prove pending-state coverage. |
| TM11 | S1516.1 12.4, PDF 338-339; S1516.2 4.7.1, PDF 55 | Federation time selection, wire/backend and all SDKs | Mandatory gap: both standardized integer64 and float64 representations are required. Local integer64 value/factory tests pass, but federation creation rejects integer64 and the backend/wire remain float64. |
| TM12 | S1516.1 12.4 Table 18 / 12.4.2, PDF 339-340 | Float64 time values and arithmetic | Gap: final time uses infinity instead of the largest finite positive value. Epsilon-value correction does not repair distinguishable-result arithmetic or backend sentinel policy. |
| TM13 | S1516.2 4.7.1-4.7.3, PDF 55-56; 6.2.7, PDF 84 | FDD time-reference tests | Tested subset: metadata structure/references. Full FOM/SOM time documentation and datatype-domain assessment are separate from minimum RTI FDD. |

The official supplemental MIM used for queue definitions has SHA-256
`839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023`.
Annex G gives Clause 11 precedence over conflicting MIM tables. The standard
does not prescribe gorti's acknowledgement or telemetry protocol. In this
architecture, SDK-side pending messages matter until invocation, but discovery,
grant, ownership and synchronization callbacks are not automatically RO/TSO
messages. Correct received-order classification must precede queue accounting.

## C++ Mapping Review

| ID | Source locator (S1516.1) | Implementation / evidence | Current disposition |
|---|---|---|---|
| CP01 | 12.1, PDF 323; 13.2, PDF 390 | C++ mapping selection and full service front end | One standard language API suffices, but all applicable requirements of the selected mapping still apply. Strict service front-end compilation remains failed. |
| CP02 | 12.12.1, PDF 362 | Current static SDK/foundation archives | Gap: prescribed dynamic-library packaging and replacement compatibility are not demonstrated by static linkage. No ABI replacement experiment executed. |
| CP03 | 12.12.1, PDF 362; 12.12.6.24, PDF 377 | Standard and federate factory implementations in the same source/library | Gap: federate-time library separation and actual custom-factory injection are not established. No mandate to ship every user-defined time representation is inferred. |
| CP04 | 12.12.2, PDF 362-363; 12.12.6, PDF 370 | Official ownership probes and strict runtime foundation | Mixed: strict foundation transfers `auto_ptr` ownership correctly; modern `sdk-types` comparison still fails. `unique_ptr` product behavior is not the official signature. |
| CP05 | 12.12.6.3, PDF 371; 13.2, PDF 390 | Official negative override probes, SDK exceptions and strict service overrides | Mixed: catchability and strict exception enforcement pass; `sdk-exceptions` and `strict-service-front-end` remain failed. No test requirement was relaxed. |
| CP06 | 12.2, PDF 323-324; 12.12.3, PDF 363; 12.12.6.4, PDF 371 | Handle encoding/decoding | Source-reviewed gap: ordinary encoded handles carry numeric identity without a type discriminator. A same-kind roundtrip is not type-isolation proof; public-API cross-type execution remains open. |
| CP07 | 12.12.4.1-12.12.4.2, PDF 363-365 | Default callbacks and encoding/callback utility suites | Tested subset: 94 exception/encoding/bridge cases. Not an exhaustive utility-signature or malformed-input assessment. |
| CP08 | 12.12.5, PDF 365; 13.2, PDF 390 | Service overloads and ambassador callback routing | Open: this pass did not run every live C++ overload, error or callback path. Go/Python and local C++ utility passes cannot replace those tests. |
| CP09 | 12.3, PDF 325-326; 12.12.6.24, PDF 377-379 | Local time/factory tests | Tested subset: exact integer values, encodings, endpoints, allocation failure and factories. FIX-05 corrects float epsilon; no integer64 federation support is implied. |
| CP10 | 12.3, PDF 325-326 | Direct linked float arithmetic experiment | Reproduced unresolved failure: `1.0 + 1e-20` remains `1.0`, violating the reviewed nonzero-interval ordering relation. Log reports `time_add_nonzero_increases=0`, exit 1. A deliberate rounding/error policy is needed. |

Two specific source discrepancies need authoritative resolution: the factory
argument described in 12.12.1.1 (PDF 362) differs from the official supplemental
zero-argument header; the C++ namespace prose on PDF 378 differs from Table 23
and the C++ headers. These do not excuse the independently established ownership,
exception or time-value defects and do not block unrelated implementation.

## Object, Ownership And DDM Review

All new service tests use real gRPC serialization/dispatch, membership checking,
real service managers, parsed FOM/MIM and the actual delivery outbox. Mutation
requests use the wire; some declaration setup uses the real manager directly.
Callback assertions inspect outbox messages, not a complete SDK callback loop.

| ID | Source locator (S1516.1) | Implementation / evidence | Current disposition |
|---|---|---|---|
| OD01 | 6.14.3(e-f), 6.14.5(b-c), PDF 131-132 | `object/delete.go`, real-wire knowledge and transferred-privilege cases | FIX-06; tested distinction between unknown caller and known nonowner. Receive-order deletion follows current delete privilege, not permanent registrant identity. |
| OD02 | 6.14.4(b), PDF 131-132 | Transferred-delete real-wire test | Tested subset: remove callbacks preserve tag, include known unsubscribed recipients/former registrant, and exclude the deleter. Timed deletion/retraction not reassessed here. |
| OD03 | 6.16.3-6.16.5, PDF 135-137 | `TestIEEE2010ObjectLocalDeleteRealWire` | Tested subset: owner/pending-acquisition/unknown rejections; successful local forgetting does not delete the federation object or notify peers. Continuing-subscription rediscovery remains open. |
| OD04 | 7.8.3, 7.8.5 and 7.1.5, PDF 160, 168-169 | Real-wire acquisition preconditions | Tested subset: knowledge, class/attribute publication, undefined attributes and existing ownership; invalid mixed sets do not partially acquire. Full precedence and superclass visibility remain open. |
| OD05 | 7.8 narrative, PDF 167-168 | Repeated-acquisition real-wire tests | FIX-07, including mixed old/new requests and concurrent repeats. Cancellation and in-transit callback states not covered. |
| OD06 | 7.9.3(h), 7.9.5(a), PDF 169-170 | If-available pending-acquisition real-wire test | FIX-08; no success/unavailable callback or partial mutation for the forbidden pending case. |
| OD07 | 7.3-7.6 and 7.1.5, PDF 160-166 | Negotiated-confirmation real-wire test | Tested subset: ownership retained until confirmation, one acquisition callback afterward, duplicate divest rejected atomically. Divest knowledge/undefined-attribute exception paths remain a source-reviewed follow-up. |
| OD08 | 9.8.3(d-h), 9.8.5(a-e), PDF 239-240 | Regional-context real-wire tests | Tested subset: undefined class/attribute, missing/foreign regions and disjoint dimension context reject without mutation. Proper dimension subsets were not tested. |
| OD09 | 9.8 narrative, PDF 238-239 | Regional-subscription and empty-set real-wire tests | Repeated identical complete sets and ordinary/regional unsubscribe independence pass; FIX-09 preserves existing subscriptions for empty sets. |
| OD10 | 9.8 narrative, PDF 238-239 | `ddm/manager.go` nonempty replacement flow | Provisional source-reviewed gap: sequential nonempty sets replace previous regions rather than adding them, with one active/passive setting instead of per-triple state. Requires a dedicated sequential-addition oracle. |
| OD11 | 9.6.3(i), 9.8.3(h), PDF 235, 239 | `ddm/operations.go` context equality checks | Provisional source-reviewed gap: proper dimension subsets appear rejected. Add positive-subset cases and review omitted-dimension routing before changing equality checks. |
| OD12 | 7.2.5(b-c), 7.3.5(c-d), 7.6.5(d-e), PDF 161-162, 165-166 | Divest handlers and ownership manager | Provisional source-reviewed gap: equivalent object-knowledge/attribute validation is not established for these paths. New acquisition tests do not prove divest exception behavior. |

The first integration run caught a regression introduced by the stricter delete
knowledge check: the internal MOM producer has no discovery record. The repair
permits only that internal producer acting on its own internally registered
instance and still checks delete privilege. Public membership validation is
unchanged. MOM timing and resign-removal tests now pass again. M23's known
nonowner fixture now subscribes before registration, so its privilege-error
expectation tests the intended state rather than an unknown caller.

Two exploratory fixture failures were not counted as product defects: a queued
privilege transfer requires explicit negotiated confirmation in that scenario,
and a sibling class's numeric attribute handle is not necessarily undefined in
the current class. The corrected tests use the actual transition and an
unambiguously undefined handle.

## Execution Evidence

Raw evidence and per-area detailed handoffs are outside the repository, under
the prefix `ieee2010-normative-20260921-` in the parent workspace directory.
Counts below are fresh executions, not the historical
[batch-3 results](ieee1516-2010-batch3-20260920.md). Overlapping runs are not added
to produce a unique test count.

| Campaign | Fresh result | Evidence suffix / boundary |
|---|---|---|
| FOM model/parser/MIM/exporter, Windows | 134 top-level tests, 324 including subtests; all pass, no skips | `fom-packages.jsonl`; independent FDD XSD enabled |
| Same four FOM packages, Linux race | Same counts, all pass; no skips/race warnings | `fom-linux-race-final.jsonl`; initial missing-schema-dependency attempt retained separately |
| Federation focused Linux race | 97 top-level tests pass across four packages | `federation-final-race-linux.log` |
| Selected production-server Linux race | 5 tests pass | `federation-server-race-linux.log`; selected MIM/live FOM/teardown |
| Time package and related checks, Linux | 106 time tests; 7 M37, 2 MOM-counter and 4 FDD-metadata tests pass | `time-package-linux.log`, `time-m37-linux.log`, `time-mom-linux.log`, `time-fdd-linux.log`; MOM counter tests are not queue-length tests |
| Strict C++ foundation | 5 selected CTest entries pass; forbidden exception exits with expected 86 | `cpp-strict-green-2.log`, `cpp-strict-exception-enforcement.log` |
| Modern C++ epsilon and utility suites | Epsilon passes; 3 suites with 94 GoogleTest cases pass | `cpp-modern-green.log`, `cpp-utilities.log` |
| Official-header negative comparisons | Three existing contract failures remain | `cpp-sdk-types.log`, `cpp-sdk-exceptions.log`, `cpp-strict-service-front-end.log`; not converted to skips |
| Existing evidence-script tests | 28 tests, 62 subtests pass | Manifest/policy regression only, not standard-service behavior |
| Windows Go/Python M5 integration | Pass with the actual installed Python interpreter | `windows-m5.jsonl`; independent processes in both directions |
| Object/ownership/DDM real-wire selection, Windows and Linux race | 10 new top-level service tests and 3 integration regressions pass on each | `ooddm-parent-service-windows-final.log`, `ooddm-parent-service-race-final.log`; full assigned domain packages also pass race |
| Full four-root Windows Go regression | 73 tested packages; 2,202 top-level / 4,434 including subtests pass; no failures or skipped tests | `four-root-windows.jsonl`; 12 further packages have no tests |
| Same four roots, Linux race | Same 73 packages and 2,202 / 4,434 passes; no failures, skipped tests or race warnings | `four-root-linux-race.jsonl`; 12 further packages have no tests |

The final full Go commands were `go test -count=1 -json ./rti/... ./tests/...
./examples/... ./verification/...` on Windows and the same command with `-race`
on Linux. Windows used Go 1.26.5; Linux used Go 1.22.2 and `GOFLAGS=-race` for
child builds. Both explicitly selected installed Python and the local official
FDD XSD. Linux reused the existing schema dependency directory. Both began after
the final Go changes and exited 0. The earlier narrower package selection is
retained as `windows-go-final.jsonl` / `linux-race-final.jsonl` and is not confused
with the four-root totals.

Initial shared-cache access and Windows Python alias failures were environment
failures, not behavioral RED evidence. The intermediate integration regressions
and the C++ residual arithmetic failure are retained, not erased by the final
Go passes. Full Python/C++ service campaigns, Windows C++, binary replacement,
performance and external certification were not rerun in this pass.

Six narrowly scoped source-linked requirement records and seven groups covering
20 new Go test functions were added to the existing manual evidence manifest.
Its full-coverage flag remains false and its independent-review record remains
unset. Manifest `--check` exits 2 with `CHECK_ONLY`, not an executed gate PASS;
the actual service execution evidence is the fresh Go campaigns above. No new
gate implementation or automatic CI workflow was introduced.
The final manifest contains 129 groups and 553 required test-name entries.
Each entry was matched to an actual passing test in both full Go transcripts;
none was missing. This cross-check does not relabel `--check` as `--run`.
The explicit `--claim full-2010 --check` remains rejected, with no commands
executed, because complete normative coverage and independent review evidence
are absent. Its output is retained as `full-readiness.json`; those prerequisite
messages are not a count of missing RTI functions.

## Next Implementation Order

| Priority | Work | Required next oracle |
|---|---|---|
| 1 | Correct asynchronous RO/TSO eligibility, zero-lookahead strictness, actual/requested lookahead and LITS/GALT consistency | Source-derived state/partial-order cases, then live SDK callback-before-grant, concurrent request and restore tests. Do not retain incompatible old expectations merely to preserve green tests. |
| 2 | Correct federation/FDD admission and lifetime behavior | Reject forbidden existing-class member growth and nonpositive rates; accept compatible descriptive differences and permitted MIM extensions; enforce standard creation inputs and last-resigner object cleanup without erasing lifetime FDD. |
| 3 | Complete exact standardized time representations and arithmetic | Integer64 beyond 2^53 through create/join, queues, grants, retraction and restore; finite float final and distinguishable arithmetic; negotiated factory behavior without silent double conversion. |
| 4 | Complete the selected C++ standard mapping | Keep the three current contract failures visible until fixed; test official headers, library packaging, custom factory integration, handle type isolation and actual service callbacks. |
| 5 | Complete DDM and ownership follow-ups | Sequential region additions, per-region options, proper-subset dimensions and divest exception cases; preserve knowledge, publication and atomicity. |
| 6 | Implement accurate MOM pending-message metrics and finish catalog review | Count each message once across actual pending stages until invocation; correct received order first; test retraction, failure, reconnect and restore. |
| 7 | Expand the normative inventory through all remaining services | Save/restore, synchronization, declaration/support, every service overload/callback/error and conditional obligation; distinguish source review, executed oracle and external assessment. |

This order separates immediate semantic contradictions from representation and
binding changes with wider effects. It replaces assumptions made before the
source texts were available; it is not an assertion that only these seven work
packages remain in the complete standard.

## Assessment Boundary

The supplied texts now support direct normative review. This first source-based
pass is not an exhaustive inventory of every obligation, overload, error and
state transition. `coverage_complete` remains false in the executable evidence
manifest. Q01-Q10 are review groups, not ten missing functions or a count of
mandatory IEEE clauses. A resolved source question does not automatically close
every implementation obligation in its group.

The next assessment work must follow the located requirements and reproduced
defects, rather than treating earlier planning assumptions as standard rules.
Full claim wording still depends on complete applicable evidence and the
conformance provisions above.
