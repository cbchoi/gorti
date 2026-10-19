# FM Oracle Review, 2026-10-03

## Plan

Scope: inventory the 295 applicable FM records that had no test links, then add
only assertion-level mappings supported by inspected existing test bodies.
This is a bounded oracle review, not a new whole-source assessment or a claim
that every implementation path for the remaining records was inspected.
Ownership is limited to this report and requirements2010/fm.json. No production,
other slice, source projection, test, or Git edits were made.

Source baseline: S1516.1, local 15161_2010-2004_redline.pdf, SHA256
789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4.
Existing source-reviewed statements, conditions, page references, subjects and
applicability are retained. Exact-red projection remains a candidate aid, not
a clean-standard replacement. No normative text is copied into this report.

Starting fm.json SHA256: bfc8b35d849cab66debf8a07c62f9c35d50af33bd49eb3f8c51ed2fda9808c22.

## Do

| Metric | Before | After |
| --- | ---: | ---: |
| FM records | 381 | 381 |
| Applicable / not applicable / unresolved | 355 / 10 / 16 | 355 / 10 / 16 |
| Records with at least one test link | 60 | 143 |
| Test links | 74 | 165 |
| Applicable records without links | 295 | 212 |
| Open-item notes | 411 | 502 |
| Reviewed units | 90 | 90 |
| coverage_complete | false | false |

Added 91 links to exactly 83 formerly unlinked applicable records, using 43
distinct existing test declarations. These are partial oracles, not 83 completed
requirements. Every affected record retains its previous open items and adds
a concrete uncovered-facet note. No new requirement, applicability decision,
source-reviewed status, or reviewed unit was added.

Implementation review covered the relevant federation manager admission and
identity paths, strict synchronization entry/completion and retirement paths,
save scheduling/replacement and restore aggregation/publication paths, FOM
repository preparation/current-FDD behavior, and Java service membership,
representation and deferred status-decoding behavior. Domain sentinels, client
guards, canned wire fixtures and live public callbacks are explicitly
distinguished in the new oracle descriptions.

### Exact Added Mappings

The following table exhausts this increment. Each listed ID receives one link
to that row's literal declaration; detailed oracle and gap text is in fm.json.

| Literal declaration | Newly mapped IDs |
| --- | --- |
| `javasdk/client/src/test/java/org/gorti/rti/ProviderTest.java::disconnectedServicesFailWithoutNetwork` | `FM2010-CONNECT-FIRST`, `FM2010-S9-PRE-A`, `FM2010-S9-EXCEPTION-J` |
| `javasdk/client/src/test/java/org/gorti/rti/ProviderTest.java::connectRejectsUnsupportedModeAndAmbiguousSettings` | `FM2010-S2-EXCEPTION-B`, `FM2010-S2-EXCEPTION-C` |
| `javasdk/client/src/test/java/org/gorti/rti/ProviderLifecycleTest.java::secondAdvanceBeforeGrantEvocationIsRejectedWithoutAnotherRpc` | `FM2010-CALLBACK-MODELS` |
| `javasdk/client/src/test/java/org/gorti/rti/ProviderLifecycleTest.java::disabledFullQueueStillSurfacesFailureAndEvokesConnectionLostOnCaller` | `FM2010-CALLBACK-MODELS` |
| `javasdk/client/src/test/java/org/gorti/rti/ProviderLifecycleTest.java::terminalStreamFailureCanBeEvokedAndSameAmbassadorReconnected` | `FM2010-S4-POST-A` |
| `rti/internal/hla/federation/manager_test.go::TestCreateFederation_DuplicateName_ReturnsSentinel` | `FM2010-S5-PRE-B`, `FM2010-S5-EXCEPTION-G` |
| `rti/internal/hla/federation/time_selection_test.go::TestUnsupportedTimeSelectionFailsBeforeModelOrGenerationMutation` | `FM2010-S5-EXCEPTION-A` |
| `rti/internal/hla/federation/selected_mim_test.go::TestIEEE2010CreateRejectsReservedMIMBeforeLoadingOrPublication` | `FM2010-S5-EXCEPTION-D` |
| `rti/internal/hla/federation/manager_test.go::TestJoinFederation_DuplicateNamePreservesOwnerAndAllowsRetry` | `FM2010-S5-POST-A`, `FM2010-S9-POST-A`, `FM2010-JOIN-IDENTITY` |
| `tests/conformance/rti/ivct-subset/tc_create_join_resign.py::test_tc001_federate_handles_monotone_increasing` | `FM2010-JOIN-IDENTITY` |
| `rti/internal/hla/federation/manager_test.go::TestJoinFederation_UnknownFederation_ReturnsNotFound` | `FM2010-S9-PRE-B`, `FM2010-S9-EXCEPTION-C` |
| `rti/internal/hla/federation/manager_test.go::TestDestroyFederation_Unknown_ReturnsNotFound` | `FM2010-S6-PRE-B`, `FM2010-S6-EXCEPTION-B` |
| `tests/conformance/rti/ivct-subset/tc_create_join_resign.py::test_tc001_destroy_with_joined_federates_raises` | `FM2010-S6-PRE-C`, `FM2010-S6-EXCEPTION-A` |
| `tests/conformance/rti/ivct-subset/tc_create_join_resign.py::test_tc001_create_join_resign_happy_path` | `FM2010-S6-POST-A` |
| `rti/internal/hla/federation/manager_test.go::TestResignFederation_UnknownFederation_ReturnsNotFound` | `FM2010-S10-PRE-B`, `FM2010-S10-EXCEPTION-E` |
| `rti/internal/hla/federation/manager_test.go::TestResignFederation_UnknownHandle_ReturnsNotJoined` | `FM2010-S10-PRE-C`, `FM2010-S10-EXCEPTION-D` |
| `rti/internal/hla/federation/resign_outcome_review_test.go::TestR4ResignOutcomeFaultOrder` | `FM2010-RESIGN-LAST-MEMBER-OVERRIDE`, `FM2010-S10-POST-A` |
| `tests/conformance/rti/ivct-subset/tc_sync_point.py::test_tc002_register_sync_point_success_callback` | `FM2010-SYNC-REGISTRATION-CALLBACK`, `FM2010-S12-POST-A` |
| `tests/conformance/rti/ivct-subset/tc_sync_point.py::test_tc002_duplicate_label_registration_fails` | `FM2010-SYNC-REGISTRATION-CALLBACK`, `FM2010-S11-PRE-E`, `FM2010-S12-POST-B` |
| `tests/conformance/rti/ivct-subset/tc_sync_point.py::test_tc002_sync_tag_round_trips_bytes` | `FM2010-S12-POST-A`, `FM2010-S13-POST-A` |
| `rti/internal/hla/synchronization/strict_completion_test.go::TestCompletionStrictQueuedAnnouncementCannotAuthorizeAchievement` | `FM2010-S14-PRE-D`, `FM2010-S14-EXCEPTION-A` |
| `rti/internal/hla/synchronization/strict_completion_test.go::TestCompletionLifecycleInvocationRetirementAndEpochReuse` | `FM2010-SYNC-INDEPENDENT-POINTS`, `FM2010-S14-POST-A`, `FM2010-S15-PRE-E`, `FM2010-S15-POST-A` |
| `tests/conformance/rti/ivct-subset/tc_sync_point.py::test_tc002_federation_synchronized_fires_once` | `FM2010-S15-PRE-E`, `FM2010-S15-POST-A` |
| `rti/internal/hla/save_restore/participant_scheduling_test.go::TestWaitingTimedSaveReplacementIsAtomicBothScalars` | `FM2010-SAVE-REQUEST-REPLACE`, `FM2010-S16-POST-B` |
| `rti/internal/hla/save_restore/requester_test.go::TestRequesterAwareSaveUsesExactTimeBackendBothScalars` | `FM2010-S16-PRE-D`, `FM2010-S16-EXCEPTION-C`, `FM2010-S16-POST-A` |
| `rti/internal/hla/save_restore/participant_scheduling_test.go::TestParticipantTimedSaveBothScalarsAndAdvanceBoundaries` | `FM2010-S16-PRE-E`, `FM2010-S16-EXCEPTION-D`, `FM2010-S17-POST-A` |
| `javasdk/client/src/test/java/org/gorti/rti/SaveRestoreServicesTest.java::membershipAndRepresentabilityFailuresDoNotSendRpc` | `FM2010-S18-PRE-C`, `FM2010-S18-EXCEPTION-C`, `FM2010-S19-PRE-C`, `FM2010-S19-EXCEPTION-C`, `FM2010-S21-PRE-C`, `FM2010-S21-EXCEPTION-B`, `FM2010-S22-PRE-C`, `FM2010-S22-EXCEPTION-B`, `FM2010-S28-PRE-C`, `FM2010-S28-EXCEPTION-C`, `FM2010-S30-PRE-C`, `FM2010-S30-EXCEPTION-B`, `FM2010-S31-PRE-C`, `FM2010-S31-EXCEPTION-B`, `FM2010-S16-EXCEPTION-B` |
| `rti/internal/hla/save_restore/save_test.go::TestSave_QuerySaveStateAcrossLifecycle` | `FM2010-S19-POST-A` |
| `pysdk/tests/test_ieee2010_fm_completion_live.py::test_save_guards_preserve_participants_and_release_rejected_sync_label` | `FM2010-S20-POST-A`, `FM2010-S20-POST-B`, `FM2010-S21-POST-A` |
| `javasdk/client/src/test/java/org/gorti/rti/SaveRestoreServicesTest.java::saveStatusIsAuthenticatedDeferredAndUsesEveryAuthoritativeProgressValue` | `FM2010-S22-POST-A`, `FM2010-S23-POST-A` |
| `javasdk/client/src/test/java/org/gorti/rti/SaveRestoreServicesTest.java::restoreStatusKeepsPrePostIdentityAndUnassignedPostHandle` | `FM2010-S31-POST-A`, `FM2010-S32-POST-A` |
| `rti/internal/hla/save_restore/restore_test.go::TestRequestRestore_BundleNotFound` | `FM2010-S24-PRE-D` |
| `rti/internal/hla/save_restore/restore_test.go::TestRequestRestore_AlreadyInProgress` | `FM2010-S24-PRE-H`, `FM2010-S24-EXCEPTION-B` |
| `rti/internal/hla/save_restore/restore_test.go::TestRestore_AggregatesAndCompletes` | `FM2010-S25-POST-A`, `FM2010-S28-POST-A` |
| `rti/internal/hla/save_restore/restore_test.go::TestRestore_FederateNotInRestore` | `FM2010-S28-PRE-D`, `FM2010-S28-EXCEPTION-A` |
| `rti/internal/hla/save_restore/terminal_publication_failure_test.go::TestTerminalPublicationSuccessStillCompletesBothFactories` | `FM2010-S29-POST-A`, `FM2010-S29-POST-B` |
| `rti/internal/application/composition/fom_extension_test.go::TestFOMExtensionPreparationFailurePreservesState` | `FM2010-FDD-COMPLIANT-MODULES` |
| `rti/internal/hla/federation/join_extension_test.go::TestJoinAdditionalFOMAtomicAdmission` | `FM2010-FDD-COMPLIANT-MODULES` |
| `rti/internal/application/composition/fom_update_rates_test.go::TestFOMUpdateRatesRetainedThroughProductionLoad` | `FM2010-FDD-CASE-SENSITIVE` |
| `rti/internal/application/composition/fom_update_rates_test.go::TestFOMUpdateRateConflictsRejectBeforeRepositoryMutation` | `FM2010-FDD-RATE-CONFLICT` |
| `rti/internal/application/composition/fom_update_rates_test.go::TestFOMUpdateRatesDecimalEquivalence` | `FM2010-FDD-RATE-CONFLICT` |
| `rti/internal/application/composition/fom_advisories_test.go::TestFOMAdvisoryInvalidOrConflictingDeclarationsRejected` | `FM2010-FDD-SWITCH-CONFLICT` |
| `rti/internal/application/composition/fom_current_fdd_test.go::TestCurrentFDDCommittedCreateAdditiveJoinAndRestore` | `FM2010-FDD-ACCESS-CURRENT-FDD` |

## Review

JSON parsing succeeded. All 43 linked declarations exist in the current source:
Python function declarations were checked through AST parsing, Go declarations
through their literal testing.T signatures, and Java through literal method
declarations. All 83 changed rows were compared with their pre-edit snapshots:
every field except tests/open_items is unchanged, and every original open-item
note remains. This is structural validation, not a test execution or normative
closure gate.

No tests were executed by this FM review. Main's coordinated corrected run was
read directly from:
D:/workspace/gorti/applicable-requirements-20261003-r1/ivct-corrected.xml.
It contains 35 cases, 35 passing, zero failures/errors and zero skips.
Seven distinct IVCT declarations are used here, including the corrected
generation-aware joined-destroy and duplicate-label callback assertions.
Their result is supplemental; earlier first-campaign failures at
D:/workspace/gorti/full-consistency-20261003-r1/ivct-subset-final.xml and the
first Java campaign are not erased or recast as passing. Main performs global
execution reconciliation; this review makes no new global verdict.

Important bounds:

- Connect/disconnect coverage is incomplete. Java evoked-mode and local guard
  tests do not prove all connection failures or the RTI's disconnected state.
- The unsupported-time selector test uses a Float64-only manager capability.
  Its Integer64 rejection is not evidence that the configured RTI lacks Integer64.
- Membership fixture assertions prove a Java client does not send an RPC for
  the enumerated nonmember operations. They do not independently prove server
  identity guards or callback-side unchanged state.
- Strict synchronization tests distinguish queued announcement from callback
  entry and preserve state on a negative achievement. One label reused across
  epochs does not prove simultaneous independent labels.
- Corrected duplicate registration uses LabelNotUnique failure callback, keeps
  original announcement/tag counts, and can still achieve the original point.
  It does not establish invalid-member-set failure behavior.
- Status-decoder tests use canned authoritative replies. All enum/identity
  values are checked, but their real-time RTI correctness is still unproved.
- Save initiation in domain outboxes is not callback invocation. Restore
  terminal publication/import/resume likewise does not prove user callback-entry
  windows. Main's selected-but-undispatched save-initiation observation remains
  a protocol-cut interpretation question, not a diagnosed defect or closed cause.
- Rate/switch conflict and current-FDD provider tests do not establish the
  entire module compliance corpus or mandatory MOM request/report access route.

## Reflect

The remaining 212 applicable/unlinked records break down as follows. Counts are
link-state counts, not counts of unimplemented behavior; main owns the exhaustive
unlinked backlog.

| Area | Remaining | Concrete next oracle work |
| --- | ---: | --- |
| General FM obligations | 29 | Initial empty object/message state; last-resign unowned cleanup; multiple-execution isolation; saved/unsaved message discrimination and persistent checkpoint lifetime; dimension/transport consistency; MOM module/MIM designators, source contents and contribution filtering; fault reporting/automatic resign and one viable partition. |
| Connect, Disconnect, Connection Lost (S2-S4) | 12 | Already-connected/reconnect failure controls, joined disconnect rejection, exact disconnected state, loss-recipient eligibility and error precedence. |
| Create, Destroy, List (S5-S7) | 11 | Missing/invalid FOM and selected MIM through public binding, connected guards, list request errors, full unchanged-state negatives. |
| Join, Resign (S9-S10) | 21 | Already-joined/restore guards, exact time/FOM errors, all six resignation action preconditions and actual delete/divest/cancel/no-action effects rather than hooks or teardown. |
| Synchronization (S11-S15) | 34 | Invalid designated member sets, save/restore and recipient-state guards, multiple independent labels, explicit/empty sets, failure/resign interleavings and full callback-entry conditions. |
| Save, Abort, Status (S16-S23) | 50 | Complete TSO/GALT alternatives and scalar boundaries; queued-versus-entered initiation; begun/complete/not-complete negatives; exact failure reasons; abort/no-save and real status progression. |
| Restore, Abort, Status (S24-S32) | 55 | Saved-type subset versus exact-census semantics, pending confirmation acknowledgment, restore-begun/initiation eligibility, negative confirmations with no effects, identity rebinding, exact failure reasons, abort/no-restore and real status progression. |
| Total | 212 | No forced links added. |

Interpretation blockers retained:

1. 4.24's saved-type subset condition and 4.25's confirmation census must not be
   silently conflated; extra members and repeated same-type pairing need review.
2. Existing fault/diagram and synchronization-member ambiguities remain unresolved.
   Detection/partition policy is not inferred from transport stream tests.
3. Existing callback FederateInternalError responsibility/applicability questions
   remain unresolved; arbitrary federate-model behavior is outside an RTI-only claim.
4. Save/restore protocol cuts require actual callback-entry observations; selecting
   or queuing an event alone is insufficient to decide whether retained control
   state is an RTI fault.

Even all-green linked executions establish only their asserted cases. Partial
linking, 212 unlinked applicable records, remaining facets on all newly linked
rows, and 16 unresolved FM applicability records prevent full FM conformance.
