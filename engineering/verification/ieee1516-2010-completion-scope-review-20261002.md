# IEEE 1516-2010 Completion Scope Review

Date: 2026-10-02. Independent bounded sidecar review, not a conformance certificate.

## Result and Boundaries

Reviewed all 25 currently unresolved-applicability records, including all six candidate interpretations and all 13 federation-management callback `FederateInternalError` records. Proposed primary dispositions: **13 user-federate, seven definite RTI, five unresolved**. These are responsibility dispositions, not verified requirements or automatic changes to the source slices. Definite RTI responsibility can coexist with unresolved wording or incomplete behavioral coverage.

No confirmed conformance failure was established. No new runtime campaign, test, slice, production, completion-tool, baseline, or Git change was made. This document is the only repository file written. Whole-source coverage remains incomplete.

S1516.1 means the supplied `15161_2010-2004_redline.pdf`; S1516.2 means `15162_2010-2000_redline.pdf`. All page numbers below are one-based PDF pages. Exact-red-exclusion JSONL projections are candidate reading aids, not authoritative clean editions. The fault clauses, callback admission exceptions, and ownership chart received supplementary visual inspection; unresolved redline/source conflicts are retained. This review does not require arbitrary user federate models to conform, and does not turn nonselected C++/WSDL bindings into universal exemptions.

## Thirteen Callback Error Records

Each listed exception subsection describes an error inside the receiving federate. Its callback-body condition is **user-federate**, not a demand that the RTI prove arbitrary application internals correct. The Java `FederateAmbassador` declaration independently permits `FederateInternalError` for every callback below. The RTI's invocation, thread, error handling, receipt, and state-transition duties remain in scope and must not be excluded along with the application condition.

| Record | S1516.1 clause; PDF pages | Actual Java callback declaration(s) | Disposition |
| --- | --- | --- | --- |
| FM2010-S4-EXCEPTION-A | 4.4.5(a); 52 | `connectionLost(String)` | User-federate condition; separate RTI lifecycle/error path below |
| FM2010-S8-EXCEPTION-A | 4.8.5(a); 54-55 | `reportFederationExecutions(FederationExecutionInformationSet)` | User-federate condition |
| FM2010-S12-EXCEPTION-A | 4.12.5(a); 60 | `synchronizationPointRegistrationSucceeded(String)`, `synchronizationPointRegistrationFailed(String, SynchronizationPointFailureReason)` | User-federate condition; both callback branches remain RTI delivery facets |
| FM2010-S13-EXCEPTION-A | 4.13.5(a); 61 | `announceSynchronizationPoint(String, byte[])` | User-federate condition; selected failed-outcome oracle exists below |
| FM2010-S15-EXCEPTION-A | 4.15.5(a); 63-64 | `federationSynchronized(String, FederateHandleSet)` | User-federate condition |
| FM2010-S17-EXCEPTION-A | 4.17.5(a); 65-66 | `initiateFederateSave(String)`, `initiateFederateSave(String, LogicalTime)` | User-federate condition; timed and untimed overloads are distinct |
| FM2010-S20-EXCEPTION-A | 4.20.5(a); 68-69 | `federationSaved()`, `federationNotSaved(SaveFailureReason)` | User-federate condition; success/failure branches distinct |
| FM2010-S23-EXCEPTION-A | 4.23.5(a); 72-73 | `federationSaveStatusResponse(FederateHandleSaveStatusPair[])` | User-federate condition |
| FM2010-S25-EXCEPTION-A | 4.25.5(a); 74-75 | `requestFederationRestoreSucceeded(String)`, `requestFederationRestoreFailed(String)` | User-federate condition; success/failure branches distinct |
| FM2010-S26-EXCEPTION-A | 4.26.5(a); 75-76 | `federationRestoreBegun()` | User-federate condition |
| FM2010-S27-EXCEPTION-A | 4.27.5(a); 76-77 | `initiateFederateRestore(String, String, FederateHandle)` | User-federate condition |
| FM2010-S29-EXCEPTION-A | 4.29.5(a); 78-80 | `federationRestored()`, `federationNotRestored(RestoreFailureReason)` | User-federate condition; success/failure branches distinct |
| FM2010-S32-EXCEPTION-A | 4.32.5(a); 81-82 | `federationRestoreStatusResponse(FederateRestoreStatus[])` | User-federate condition |

Do not treat these 13 records as 13 tested callback methods: their Java binding spans 18 declarations, including alternate branches and the save overload. No exact checked-error facade oracle covering all 18 declarations was established in this review.

## Other Twelve Records

An asterisk marks one of the six current candidate interpretations. None is promoted to complete clause review here.

| Record | Source locator | Primary disposition and remaining gap |
| --- | --- | --- |
| DMOM2010-REVIEW-BOUNDARY * | S1516.1 5.1.4, 6.1.8; 86-91, 111 | **Definite RTI.** Formal declaration/object knowledge and ownership states constrain RTI services. Incomplete inventory of Figures 10-16 is a decomposition/evidence gap, not evidence of inapplicability. Need transition, guard, rejection, and nonmutation oracles; no full chart mapping supplied. |
| FM2010-FAULT-RELIABLE-TSO * | S1516.1 4.1.5; 49-50 | **Definite RTI.** Fault handling and reliable TSO guarantees concern the RTI. Both retained passages are visibly present: one frames delivery by last-good time, another reliable-message deliverability. Exact breadth and delivery-versus-deliverability interpretation remain unresolved; ordinary successful time advancement does not close a fault-boundary guarantee. |
| FM2010-FAULT-NOTIFICATION * | S1516.1 4.1.5, 4.4; 50, 52 | **Definite RTI.** Connection-loss notification and consequences are RTI behavior. Retained attempt/permissive language and 4.4 best-effort framing need reconciliation. Existing terminal-stream tests cover selected paths, not every fault, compulsory notification timing, or notification-body failure. |
| FM2010-SYNC-SUBSET-MEMBER-AMBIGUITY * | S1516.1 4.11; 59 | **Definite RTI.** Synchronization-set admission belongs to the RTI. Former-member handle language and the subsequent qualifier about designated joined federates require precise interpretation; do not assume they necessarily contradict. Inspect `rti/internal/hla/synchronization/manager.go` for explicit former, current, never-member, and wrong-federation handle cases before closure. |
| OWN2010-STATE-LEGALITY * | S1516.1 7.1; 154-155 | **Definite RTI.** The source requires rejection of operations absent from the ownership statechart. `rti/internal/hla/ownership/manager.go` and `known_command.go` are relevant, but the full Figure 16 guard/transition inventory remains open. A few valid ownership flows cannot establish all forbidden transitions or rejection nonmutation. |
| SM-SUPPORT-CALLBACK-ADMISSION-TENSION | S1516.1 10.43.5; 284-285, with adjacent 10.44 | **Definite RTI** for callback enable/disable semantics. Visually retained abstract exceptions include nonmembership and added nonconnection while the joined precondition is deleted. Java declarations omit those two exceptions. Preserve the abstract/binding mismatch as unresolved; save/restore gate tests do not adjudicate disconnected/nonmember admission. |
| SM-MOM-TABLE-SOURCE-PRECEDENCE | S1516.1 11.6; 292, 298-299, 307 | **Definite RTI.** Clause 11 MOM tables take precedence over conflicting Annex G definitions. `rti/internal/hla/mom/catalog.go` must be assessed against that precedence and individual names/types/values. Source-table spelling conflicts remain open. RTI MIM construction/admission is in scope; arbitrary author's complete FOM/model behavior is not. |
| OMTSLICE2010-NORMALIZE-RTISET * | S1516.2 B.3; 95 | **Unresolved.** The enumerated-set normalization definition uses an RTI-set mapping into a bounded domain; it does not by itself establish a standalone public Java service named `RTIset`. Separate OMT/federate normalization usage from actual RTI Normalize Federate Handle/Service Group services (S1516.1 Table 8, 307). Existing handle/service-group tests do not prove generic set-domain behavior. |
| JAVA2010-ENCODING-TABLE-RECONCILIATION | S1516.1 12.11.3.2; 345, 440-441 | **Unresolved.** Retained encoding/table entries for handle/logical-time/logical-time-interval helpers differ from the official archived Java type inventory. The official API signature match is evidence for that archive, not resolution of conflicting retained source. Deleted legacy Annex B bodies cannot fill the gap. |
| JAVA2010-CUSTOM-TIME-REGISTRATION | S1516.1 12.11.5.24.4; 360 | **Unresolved conditional responsibility.** Registration applies when a nonstandard implementation is supplied: RTI vendor for its supplied provider, federation/federate implementer for its own provider. Do not require an unclaimed custom type to exist, or exempt RTI admission of a valid provider without reviewing the separate support contract. |
| MAP-CUSTOM-FACTORY-ENCODING | S1516.1 12.3; 325-326; related 4.1.6, 50 | **Unresolved.** Source custom-factory support must be distinguished from mandatory built-in scalar families. `AmbassadorServices.selectTimeFactory` rejects every nonbuilt-in selector before custom provider lookup; `TimeWire` encodes numeric built-in families rather than opaque custom encodings. This is a concrete implementation limitation. Unknown-selector rejection tests are not a valid-provider acceptance oracle. No valid installed custom provider was exercised, so no confirmed conformance failure is claimed. |
| MAP-FLOAT64-FACTORY-AMBIGUITY | S1516.1 12.4.2; 340 | **Unresolved.** The Float64 section retains an Integer64-named factory/interval bullet. Preserve editorial/source ambiguity rather than changing required public types from that bullet alone. The separately specified built-in Java time declarations remain definite obligations. |

## Java Evoke and Callback Propagation

Relevant implementation paths are `javasdk/client/src/main/java/org/gorti/rti/AmbassadorServices.java`, `CallbackInvocationDelivery.java`, `OwnershipCallbacks.java`, `SaveRestoreCallbacks.java`, and `TimeWire.java`. Exact API declarations are in `javasdk/ieee1516-api/src/main/java/hla/rti1516e/{RTIambassador,FederateAmbassador}.java`; source binding context is S1516.1 clause 12 and normative Annex B, 439-443. Legacy source on 444-502 is not used as the retained API contract.

- `RTIambassador.java:1822` and `:1828` declare both evoke methods with `CallNotAllowedFromWithinCallback` and `RTIinternalError`, not `FederateInternalError`. Merely demanding that the checked callback exception escape unchanged from an evoke call would conflict with these signatures.
- `AmbassadorServices.java:534` uses one common evoke implementation for both methods. It rejects callback reentry, sets `inCallback`, wraps an `RTIexception` from the callback as `RTIinternalError` with the original cause, and clears `inCallback` in `finally`. This is inspection evidence, not a complete behavioral oracle or a prescribed universal error policy inferred from the abstract exception list.
- `AmbassadorServices.java:579` records the original callback failure before the evoke wrapper. Its delivery finalizer completes invocation reporting and attaches reporting failures without replacing the original callback failure. Checked errors, unchecked exceptions, and `Error` are captured for that outcome path; unchecked application errors are not necessarily wrapped by evoke. No blanket standard requirement to wrap every unchecked user error was established.
- `AmbassadorServices.java:1011` has a separate `connectionLost` path. It clears session/membership/time/queues before invoking the target, wraps its `FederateInternalError` as `RTIinternalError` with the cause, and resets the callback flag in `finally`. This path is not the ordinary streamed completion-report path; do not infer a receipt requirement for it from the proprietary receipt mechanism.
- Missing selected oracles: exact original-cause identity through **each** public evoke method; reentry flag reset and later callback usability after checked body failure; throwing `connectionLost` with teardown and same-ambassador reconnection; alternate success/failure callback branches and the timed save overload. Test adapters or record counts cannot substitute for these assertions.

## Inspected Literal Tests and Their Limits

Paths in this table share repository prefix `javasdk/client/src/test/java/org/gorti/rti/`. These mappings come from test bodies and helpers, not keyword matches.

| Literal declaration | Concrete assertion/oracle | Scope limit |
| --- | --- | --- |
| `OwnershipCallbacksTest.userCallbackFailuresAreNotSwallowedOrRetried` | Injects `FederateInternalError` into divestiture confirmation and asserts the same exception from direct adapter delivery. | Ownership adapter only; neither public evoke method nor one of the 13 FM conditions. |
| `LocalNotificationCheckpointTest.queuedCopiesArePurgedEvenWhenBodyFailsWithoutLosingMultiplicity` | First `attributeIsNotOwned` invocation throws checked callback error; `evokeCallback(0)` must throw `RTIinternalError`; checkpoint retains the distinct second notification and removes duplicate copies of the first; exactly two bodies run. | Proves selected local queue bookkeeping and wrapper type, not exact cause identity or all FM callbacks. |
| `ProviderLifecycleTest.assertionErrorInCallbackIsReportedAsFailedInvocation` | Time-grant body throws `AssertionError`; checks one invocation, entry receipt, failed completion with nonempty exception, and thread/receipt correspondence. | Accepts `AssertionError` or `RTIinternalError`; does not pin exact checked-error class/cause. Entry success does not mean successful body completion. |
| `ProviderLifecycleTest.terminalStreamFailureCanBeEvokedAndSameAmbassadorReconnected` | Checks one `connectionLost` notification on the evoking thread and reuse/rejoin of the same ambassador without stale stream failure. | The notification body does not throw `FederateInternalError`; cannot close that failure path. |
| `LiveControlOutcomeRestoreTest.pendingControlAndKnownOutcomeSurviveServerRestartAndFreshJvm(String, boolean)` | Four literal CSV cases: Float64/Integer64 crossed with success true/false. `announceSynchronizationPoint` for the finished label throws `FederateInternalError` in false cases. Helpers assert saved and restored completion success equals the parameter and exception name is exactly empty or `FederateInternalError`; restore crosses a server restart and fresh JVM. | Selected S13 completed-outcome persistence; not all 13 callback conditions or exact outward evoke cause. |
| `LiveFederationTimeSelectionTest.serverRejectsUnsupportedSelectorsWithTypedMetadataAndNoCreatedFederation` | Unsupported/unknown selector requests produce `CouldNotCreateLogicalTimeFactory`; listing shows no created federation; facade rejection also checked. | Negative unknown-provider admission only. Does not demonstrate valid registered custom-factory support. |
| `LiveCallbackGateContractTest.enableDuringSaveThrowsSaveInProgress`, `disableDuringSaveThrowsSaveInProgressWithoutDisablingDelivery`, `enableDuringRestoreThrowsRestoreInProgress`, `disableDuringRestoreThrowsRestoreInProgressWithoutDisablingDelivery` | Exact save/restore exceptions and retained delivery behavior on rejected gate operations. | Does not resolve nonconnection/nonmembership exceptions missing from Java declarations. |

Historical record inspected directly: `D:/workspace/gorti/defect-java-backend-final-20261002/final-whole-live-reports/TEST-org.gorti.rti.LiveControlOutcomeRestoreTest.xml`. It records exact invocation names `pendingControlAndKnownOutcomeSurviveServerRestartAndFreshJvm(String, boolean)[1]` through `[4]`, separately from `pendingPureLocalResultsSurviveServerRestartAndFreshJvm(String)[1]` and `[2]`. The four CSV source cases are associated in declaration order; XML names do not independently encode their scalar/boolean labels. Do not collapse the four invocations into a complete requirement verification.

The supplied `results-summary.json` reports 535 passed, zero failures/errors/skips for the historical Java run. These remain **passing records only, completeness unassessed**, not fresh execution or whole-standard conformance. The supplied Go and Python records are unchanged and were not reexecuted or used to inflate Java callback coverage.

## Snapshot Correspondence

Reviewed slice SHA256 values at this bounded review snapshot:

| `engineering/verification/requirements2010/` file | SHA256 |
| --- | --- |
| declaration-object.json | bebf5c9c4b3644ab5db29b06b604194cdf2ccdaf7296e012e2e6c6bc29fa2633 |
| fm.json | e86116b12e44b072058c16503c8a49a0e9ed0e6cdc050d09bfa5f5e62d828656 |
| omt.json | c52e37bac8c69efc32d3c3c7d5451b1d6c3f2183915bd3e4d93ada43436fe5cf |
| ownership-ddm.json | b125c9a33f8401bcc0c0810c98ae67426912f62f3836ff2cf6c8094f79e1d076 |
| java-annex.json | 9fa09ce98930dc64a2952e0837c92a623014156efb53445e6b13dc6f0fd6bed0 |
| support-mom.json | 16f4d6bd5dc3f1202998759ae02824f41938acd26a016561b16e91033330bb7e |
| time-mapping.json | f8f125d4b355fed9b499d833f50bda64afb5b46897f9da9918cb2b5d909f051e |

Read-only hash comparison found repository files byte-identical to the execution worktree under `D:/workspace/gorti/defect-java-backend-final-20261002/work/javasdk/` for these selected paths:

| Path within javasdk | Shared SHA256 |
| --- | --- |
| client/src/main/java/org/gorti/rti/AmbassadorServices.java | 8ce55e1fe140cb9bd78df1786114c4226c6f19f67f3239ccf7a64f5bae37c51e |
| client/src/test/java/org/gorti/rti/LiveControlOutcomeRestoreTest.java | 6d9d2f879c7340952ba56d7c2b987c328ce44f93839ad794dba055caef9085d7 |
| client/src/test/java/org/gorti/rti/LocalNotificationCheckpointTest.java | 0e68743a5f131d94ee9e5f8f59c92bdc05262f12f5a089c25fb3f498f618e153 |

This selected correspondence is not an entire-tree equivalence claim. Main must rebase dispositions if relevant inputs change. Existing official API comparison records under `D:/workspace/gorti/internal-java-assessment-20261002/` distinguish signature inventory from the two discovery-helper body adaptations; an API signature match is not runtime semantics coverage.

Pinned server identity was rechecked: `D:/workspace/gorti/rtid-defect-final-20261002.exe`, SHA256 `24b9c82590543159e6234c8dc7d473b6ee1699329c34677d1779035aad115b05`. It was not started in this review. Supplied S1516.1 identity remains `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4`; that trusted source identity was not newly recomputed here.

## Fresh Artifacts and Handoff

Fresh JUnit path: none. Fresh Go JSON path: none. New runtime executions: zero; new pass/failure/skip counts are not applicable. Confirmed newly reproduced failures: zero. No owned server/JVM/test process was started; completed source-rendering processes leave no owned running process.

Outside-Git source-review artifacts only: `D:/workspace/gorti/completion-check-scope-20261002/{fault-049,fault-050,callback-284,callback-285,ownership-155}.png`. No raw standard text was copied into the repository.

Main should preserve the unresolved wording/provider questions, move definite RTI responsibility out of any blanket applicability exclusion only after coordinated reconciliation, and retain callback delivery/error facets when classifying the 13 application error conditions. Highest-risk false closure would be treating unknown-selector rejection as custom-provider support, a shared evoke implementation as complete callback-error testing, or a few chart paths as full statechart conformance. No production change is proposed without main coordination.
