# Support/MOM applicable-requirement review

## Plan

Review the fifteen applicable unlinked rows in `support-mom.json` against source-reviewed obligations and exact test bodies. Prefer actual callback, state, rejection and byte assertions to handler/name inventories; leave unsupported facets visible. The RTI-only assessment does not impose arbitrary participant model conformance.

## Do

Source: S1516.1 supplied redline PDF, SHA-256 `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4`. Candidate page extraction is not an authoritative resolved edition. Source statements, applicability and reviewed units are unchanged.

| ID | Source clause / PDF pages | Disposition and remaining facets |
| --- | --- | --- |
| SM-SUPPORT-EVOKE-SINGLE | 10.41 / 283 | Link `switchesRemainLocalAndDisabledCallbacksStayQueued`: injected queued callback yields pending true while disabled, no invocation; enabling produces one invocation and pending false. Connected nonjoined local fixture, not full admission proof. Initial empty evoke has no return assertion. Multi-item at-most-one, minimum wait, precision and immediate mode remain open. |
| SM-SUPPORT-EVOKE-MULTIPLE | 10.42 / 283-284 | Unlinked: await helpers are not independent minimum/maximum/backlog/pending-indicator oracles. |
| SM-MOM-PREDEFINED-IMMUTABLE | 11.1 / 286 | Link `TestSelectedMIMSupportedProfileAndRejections`: selected predefined encoding, required-attribute and reliability changes reject; descriptive text changes remain. Structural additive extensions, full unchanged inventory and no direct RTI action on new elements remain open. |
| SM-MOM-OBJECT-CARDINALITY | 11.2 / 286 | Link M36 join/resign/federation-roster tests: actual manager/outbox discover/scope/reflect/remove counts, object identity and decoded attributes/roster. These observer events do not establish global singleton or uniqueness through destroy/restore, or every periodic state value. |
| SM-MOM-OBJECT-EXTENSION-ISOLATION | 11.2.1 / 287 | Unlinked: no participant-created subclass instance with assertions excluding RTI updates and subscription leakage. |
| SM-MOM-OWNERSHIP-PROHIBITIONS | 11.2.1 / 287,290 | Unlinked: no exact predefined-owner retention plus positive participant-owned extension-attribute and RTI nonacquisition oracle. Simulation-object ownership service tests are not MOM-object ownership tests. |
| SM-MOM-REQUEST-ATTRIBUTE-REFRESH | 11.2.1 / 287 | Unlinked: directly calling RefreshMetrics does not establish receipt of a public attribute-value request and consequent reflection. |
| SM-MOM-REPORT-EXTENSIONS | 11.3.1 / 288 | Unlinked: incoming service-subclass dispatch is not outgoing report-subclass or added-parameter behavior. |
| SM-MOM-LEAF-ADMINISTRATION | 11.4.1 / 289-290,292-294 | Unlinked: catalog names and dispatch handlers do not prove exact RTI leaf publication/active-subscription sets and absence of extra administration. |
| SM-MOM-UNTIMED-UNTAGGED | 11.4.1 / 290 | New `TestApplicableTimeSupportMOMUntimedUntaggedLifecycle`: exactly two actual reflections and one removal after join/resign, with both timestamp fields nil and user tag empty. Interactions, periodic/service reports, public Java metadata and actor regulation/constraint state remain open. |
| SM-MOM-PROHIBITED-MUTATIONS | 11.4.1 / 290 | Unlinked: MOM-requested changes to simulation objects are not transportation/order/DDM mutations of MOM objects. No unchanged MOM-state oracle exists in the inspected subset. |
| SM-MOM-TABLE7-SERVICE-DATATYPES | 11.6 / 299,302-305 | Link four servicecatalog tests and `TestMOMServiceCatalogAll32LeavesOverRPC`: positive dispatch, truncated/missing/nil-present rejection, optional absent-versus-empty semantics, propagated executor errors, actual selected target effects and invalid-parameter no-change assertions. Independent byte vectors/all decoded values, alignment/enums and both time families remain open. |
| SM-CONFORMANCE-CERTIFIED-LABEL | 13 / 390 | Unlinked: no recognized authority certificate is evidenced. Local testing cannot discharge the labeling condition. |
| SM-CONFORMANCE-RTI-SERVICES | 13.2 / 390 | Link the 32-leaf RPC test only as a service-bridge subset with real selected effects and error/no-change controls. Complete service/callback census, conditions and certification remain open. |
| SM-CONFORMANCE-RTI-LANGUAGE-SCOPE | 13.2 / 390 | Link `standardFactoryDiscovery`: Java factory creates a real ambassador, exact integer32BE 42 bytes and selected Float64 factory. Not a mere name inventory, but still not whole Clause 12/API semantics or certified identification. |

The new lifecycle test uses production MOM/declaration/object managers with a fake FOM and capturing outbox. It does not claim a live executable or Java callback campaign. The existing server catalog test uses seeded production managers and real in-process gRPC, not complete RTID bootstrap. Shared fixture encoders are not independent normative datatype vectors.

## Review

Fresh records: `D:/workspace/gorti/applicable-requirements-20261003-r1/time-support/focused-go.jsonl` and supplementary `order-state-go.jsonl`. Combined: 15 top-level tests and 300 subtests passed, zero failures/skips; all selected negative variants remain. Four M36 lifecycle tests, selected-MIM variants and the 32-by-two RPC catalog variants are included. Commands, invocation IDs and source hashes are in `evidence-summary.json` outside Git.

Reused CallbackGateTest and ProviderTest records are in `D:/workspace/gorti/full-consistency-20261003-r1/java/work/javasdk/client/target/surefire-reports/`; their literal declarations passed and current source hashes match executed copies. Neither is live-server evidence. The original complete Java campaign remains failed: 537 passed, three JVM-child timeout failures, zero errors/skips, 540 total. Separate six-case restart retry success does not supersede those failures. No new Java campaign was run for this increment.

## Reflect

Seven rows gain bounded links; eight remain unlinked with concrete missing oracles. All 47 rows and `coverage_complete: false` remain. No source unit is added, requirement marked verified, failure variant dropped, recognized certification inferred, or production file edited. No server/persistent process was started; focused commands completed and retained their outputs. Highest-risk next work is RTI actor extension/ownership isolation and exact publication/subscription sets, followed by independent Table 7 vectors and public evocation timing/admission controls.
