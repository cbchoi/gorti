# Synchronization Integration Contract

Date: 2026-09-30. Work in flight; not a conformance or certification claim.

## Audit Handoff

- [Whole-RTI completion audit](ieee1516-2010-completion-audit-20260930.md)
- [Selected gap inventory JSON](ieee1516-2010-completion-inventory-20260930.json)

The JSON exists, contains G01-G23, and explicitly sets inventory_complete=false.
S1 13.2 requires recognized-authority-certified publicly available verification
results for the formal conformance label. No authority, certificate or completed
external assessment is asserted.

## Owned Scope

This worker owns hla/synchronization, NEW core/synchronization_callbacks.go,
NEW application/server/sync_lifecycle.go and new tests. It does not edit run.go,
existing callback receipt code, schema, SDKs, declaration, ownership or deletion.
The available app messaging tool rejected sending to the native ancestor;
this file is the integration handoff.

## Parent/Zeno Root Wiring

Strict announcement eligibility needs authenticated invocation ENTRY before the
ambassador handler body. Parent/Hubble's additive invocation_entry bool is now
available in ReportCallbackInvocation. True reports entry before the body;
false/omitted reports completion. This worker does not edit schema or SDKs.

1. Construct synchronization.Manager with Options.RequireInvocations=true,
   Members, and a core.ReservableOutbox. Zero-value compatibility mode is not
   the assessed production profile.
2. core.SyncLifecycle exposes FederateJoined(ctx,fed,h) error,
   FederateResigned(ctx,fed,h) error, and RetryPending(ctx,fed) error.
   Call join/resign after membership commit and outside membership-manager
   locks. Errors retain the synchronization change plus its pending obligations;
   schedule RetryPending until delivery capacity/stream binding returns.
3. core.SyncCallbackObserver exposes ObserveSyncCallback(ctx,fed,h,
   kind core.SyncCallbackKind,label string,sequence uint64) error.
   Kinds are Announced, Synchronized, RegistrationSucceeded, RegistrationFailed
   (each with the SyncCallback prefix).
   ObserveSyncCallbackEntry has the same signature: entry establishes announcement
   knowledge; completion is used for synchronized-label retirement.
4. NEW syncCallbackInvocationObserver(observer) adapts the already authenticated
   CallbackReceipts Observe payload. Multicast it independently of optional MOM
   reporting. Invoke it for successful or throwing actual ambassador callbacks:
   the invocation occurred in both outcomes. Keep it out of stream-send hooks.
   NEW syncCallbackEntryObserver(observer) adapts the authenticated entry phase.
5. Parent's MandatoryCallbackOutbox now reserves mandatory opaque receipts before
   queue admission, retains failed-send callbacks, and supports authenticated
   entry/completion. Direct live TCP tests verify these root observers. Optional
   Prepare without a mandatory receipt is still not the strict profile.
6. Use savebundle.StrictSynchronization (the application synchronizationCheckpoint
   alias) for version-3 domain images. It preserves exact payload obligations,
   epochs, sequence IDs, entry/completion flags and all-member mode. Legacy strict
   version-2 prototypes lack admitted registration confirmations and are rejected.

## Invocation/Reentrancy Boundary

Announcement callbacks can invoke SynchronizationPointAchieved reentrantly,
before their post-callback ReportCallbackInvocation. Require authenticated
announcement ENTRY, not completed announcement, for strict achievement.
Queue acceptance and stream send alone cannot authorize achievement. The
compatibility profile remains explicitly weaker.
Label retirement, unlike achievement, waits for every remaining required
FederationSynchronized actual-invocation receipt. Stream send does not retire it.

A receipt's event.Seq is the manager-issued callback identity; label text alone
cannot identify the epoch. Duplicate and stale reports must not retire a reused
label. Entry must actually be implemented by the assessed SDK and receipt tracker;
this server hook alone is not full SDK announcement-knowledge evidence.

## Mandatory Transport Handoff

DELIVERED: version-3 ManagerState retains an
Obligations DTO for every uncompleted strict synchronization callback, including
registration success/failure, after queue admission. PrepareImport rebuilds
pending retry from those obligations with unchanged sequence IDs and label
epochs. The retry worker discovers restored pending work; Restored offers an
immediate attempt after commit outside checkpoint/membership locks. GSY2 remains the codec
envelope; its internal JSON version changes. Active strict version-2 prototypes
lack admitted registration confirmations and cannot prove exact recovery.
Two additive core callback kinds carry registration success/failure receipts;
the NEW application observer helper maps them. Parent need not edit the tracker
for those kinds beyond its generic synchronization multicast.

CONCRETE SAVE ADAPTER: use
savepoint.ManagerSnapshotKeySync: savebundle.StrictSynchronization{Manager: syncMgr}
in the typed save composition. Existing savebundle.Synchronization automatically
selects that codec for a strict manager, so old root composition no longer loses
the DTO. Both adapters implement RewriteFederates; RestoreGraph's default sync
reader recognizes GSY2, validates domain obligations and saved-roster recipients,
and rewrites every typed FederateHandle, not sequence IDs or label epochs.
The application synchronizationCheckpoint helper is now an alias to this shared
adapter. No typed_save.go or root edit is performed by this worker.

The retry worker now also queries core.SyncPendingQuery.PendingFederations.
Therefore the root's existing lifecycle Run discovers restored retry work even
when no join/resign admission failure previously populated its local retry map.
Restored(ctx,fed) remains available for an immediate post-commit attempt outside
checkpoint locks. NEW live TCP production tests pass Float64/Integer64 and
Events/EventBatches. The shared DDM collector process_test.go now reports entry
before its collector body and completion after it; the whole DDM registration
batch passes. M37's duplicate-registration test expects the standard failed
confirmation without a synchronous duplicate error. The all-manager restart
fixture now invokes authenticated entry/completion and its cross-generation
restore passes. Region fixture changes remain with their owner.

The fresh six-package Windows campaign (synchronization, savebundle, grpc, server,
M37, DDM registration) now passes including the live Go SDK acceptance test.
Focused Linux race checks also pass for domain, persistence, transport, direct
TCP, live Go SDK, checkpoint replay and cross-generation restart/rebind.
The parent capability patch advertises the configured authenticated entry
receiver. Both Float64 and Integer64 Go SDK cases now pass without weakening
RequireInvocations or spoofing an SDK field.

Parent owns mom_receipts.go and the required-receipt outbox adapter. Its agreed
admission boundary is Reserve -> convert the domain event -> PrepareRequired ->
reserve raw queue capacity. Store the immutable pretracked wire envelope. Commit
only admits that envelope to the queue; Release abandons its reserved receipt.
Mark sent only at the actual stream boundary, never as invocation evidence.

Preparing a receipt only after draining a stream batch is insufficient: a
Prepare error at that point can lose the mandatory callback. After admission,
failed stream delivery must remain in the transport tracker for retry/reconnect
with the same callback identity and receipt. The parent's transport ledger owns
these admitted-but-not-invoked obligations and their exact persistence.

Live queue commit clears Pending but keeps every callback in Obligations until
completion. Import rebuilds Pending from all remaining Obligations in sequence
order, including entry-without-completion and registration confirmations. Live
post-admission reconnect still belongs to the transport tracker. Domain recovery
preserves callback identity/kind/payload; it does not claim opaque-ticket or
exactly-once application execution continuity across a whole-process restart.

The authenticated entry path calls syncCallbackEntryObserver before the SDK
handler body. The completion path calls syncCallbackInvocationObserver after
return or throw. Parent must independently multicast both paths to synchronization
even when optional MOM reporting is disabled. Root construction, membership hooks,
retry and observer multicast are now wired by parent. Actual Go SDK acceptance
passes; actual Python/Java SDK acceptance remains separately owned by Hubble.

## Source Decisions

Use retained/added S1 PDF pages 59-64, not deleted redline text. Duplicate
registration is a failed confirmation, not a new synchronous duplicate-label
exception. Empty/omitted sets select all; late joins stop at the all-achievements
boundary; explicit nonempty subsets do not expand; resign removes participants.
Retire a label only after the required synchronized invocations.

Page 59's valid-departed-designator wording versus retained membership sentence
needs a clean-edition/correction decision. Do not call that ambiguous branch
normatively completed. Existing current-members-only handling remains pending
that decision; no fabricated clean source is used.

## Integration Verification

Resolved freeze correction: new deterministic
`TestSynchronizationRetryCannotRecreateReceiptDuringResign` reproduced one
mandatory receipt being recreated after ClearFederate while an FM writer still
held the roster member alive. G13 implementation is paused for this fix.
`sync_lifecycle.go` now has optional `retryAdmission` used ONLY by background
Retry and post-commit Restored. Admission is acquired BEFORE operationMu; Joined
and Resigned never acquire it. New `sync_retry_admission.go` composes checkpoint
observation then AcquireGeneration, releasing in reverse order.

Parent-owned root integration: lifecycle is constructed at run.go:1320, but
checkpoint admission is not available there yet. Install AFTER the save admission
block around run.go:1530-1554 and BEFORE Serve starts, using its optional
`observationAdmission` interface (not a typed-nil saveAdmission pointer):

```go
syncLifecycle.retryAdmission = newSynchronizationRetryAdmission(fedMgr, observationAdmission)
```

Use the established runtime checkpoint admission object, or nil only in an
explicit legacy embedding without checkpoint support. Do not put this lease
inside Joined/Resigned, manager RetryPending or mandatory-receipt validation:
that would recursively acquire the FM writer boundary. This worker does not
edit run.go. Parent instead installed the equivalent common
`acquireCallbackService` closure at run.go:1669. It acquires checkpoint observation
then the generation lease and is shared with receipt AcquireService. Member hooks
do not use it. The production installation guard and fresh acceptance are GREEN.

Exact helper acceptance is GREEN:
`go test ./rti/internal/application/server -run 'TestSynchronizationRetryCannot|TestSynchronizationRetryAdmissionFailure|TestSynchronizationRetryAdmissionCheckpointGenerationOrder' -count=1`.
The same three tests passed Linux `-race -count=20` (1.412s), with no race
report. The real root guard `TestSynchronizationProductionRetryAdmissionInstalled`
was RED until parent installed the assignment; it now passes. Fresh Windows
acceptance selecting all `TestSynchronization` tests, all-manager restart, dynamic
FDD, late FDD subscription and live FOM join passed (1.444s). Linux `-race`
selecting all `TestSynchronization` tests and all-manager restart with `-count=10`
passed (28.261s), including the new installed-root guard and actual TCP campaigns.

Earlier frozen handoff: all seven Windows packages passed and focused Linux race
acceptance passes, including actual root TCP, Go SDK and checkpoint replay.
The active all-manager restore point remains preserved. No further sync-domain
change is planned for G12. The next isolated G13 review/test is documented in
[G13 RED milestone](ieee1516-2010-g13-delayed-subscription-review-20260930.md).
Its NEW `tests/unit/object/delayed_subscription_review_test.go` deliberately
adds two semantic REDs and one passing control. Parent should distinguish this
requested next-milestone RED from the frozen synchronization acceptance. Shared
object/time send files were not changed; coordinate ports with their owners first.

Integration update: before a synchronization-worker adapter edit was made,
the concurrent parent capability patch appeared in `adapters/grpc/federation.go`
and `server_typed.go`, with `federation_callback_capability_test.go`. This worker
has not edited those files. Installed negotiation and actual live Go SDK behavior
now pass. No root, schema, SDK or receipt-tracker edit was made by this worker.

Transient broad-build blocker observed after that patch: the new
`application/server/mom_transportation_confirmation.go` references nonexistent
`deps.Federations.LogicalTimeRepresentation` (line 62) and
`core.TimeRepresentationInteger64` (line 67). Join-capability adapter tests pass,
and prevented server compilation. Its owner corrected those references; it is
not a remaining blocker and this worker did not edit that helper.

The transportation references were corrected concurrently. The next compile
transient compile blocker was `mom_service_production_test.go:39`: nonexistent
`rtiv1.PubInteractionRequest`. This is another owner-controlled test file, not a
synchronization failure. Its owner corrected it. New owned TCP checkpoint-replay
acceptance now passes and covers
three admitted obligations across a fresh generation and reverse join order,
followed by authenticated entry, reentrant achievement and final completion.

Latest results and blockers are in the
[synchronization milestone handoff](ieee1516-2010-synchronization-completion-20260930.md).
The 17 new domain tests, three application-helper tests and two strict adapter
tests pass. Direct production TCP acceptance has four passing combinations
(Float64/Integer64 x Events/EventBatches), including reentrant achievement and
strict terminal completion. Both live Go SDK cases and both fresh-root TCP
checkpoint-replay cases pass. A fresh rtid binary builds.

The seven-package broad command passed all six server-side packages but failed
the extra `rti/pkg/federate` package at
`TestConfirmedUnaryContextPreservesSessionAndCallerMetadata`, session_test.go:23:
confirmed unary lost authenticated session, metadata `map[x-fixture:[kept]]`.
This worker has not edited SDK files. Its owner corrected this regression; a
fresh seven-package campaign now passes all seven packages, including the full
Go SDK package. The owned synchronization milestone is frozen without additional
domain changes. Parent's broader acceptance remains a separate release gate.

Three preexisting-path tests were RED before implementation: duplicate
registration returned a synchronous error; outsider achievement was accepted;
explicit empty set selected no members. A separate RED reproduced achievement
before announcement entry. The corrected component lifecycle, capacity,
entry/completion and persistence cases now pass, including focused race checks.
The durable domain, graph codec and owned synchronization milestone are ready.
Python/Java acceptance and
the page-59 source ambiguity remain outside this passing component milestone.
