# Synchronization Completion Milestone

Date: 2026-09-30. Initial milestone passed; reproduced retry-generation risk fixed
and parent root integration freshly verified below.
This is not a whole-RTI conformance claim or a global production freeze approval.

## Delivered

## Freeze Correction: Retry Versus Resign

The subsequent independent integration review reproduced a concrete hole in the
previous freeze: background synchronization retry recreated one mandatory callback
receipt after ClearFederate while resign still held the FM writer and the member
was still visible. The new deterministic test uses the actual strict domain
manager and MandatoryCallbackOutbox/CallbackReceipts, not a speculative counter.

`sync_lifecycle.go` now accepts optional retryAdmission and uses it only for
background Retry and Restored-after-commit. New `sync_retry_admission.go` acquires
checkpoint observation then the current generation lease, BEFORE operationMu.
Joined/Resigned never acquire it; tests prove a writer-held resign hook can finish
while background retry waits on the writer. Failure/cancellation releases pins,
retains retry work and does not call the domain outside admission.

The helper tests are green. `TestSynchronizationProductionRetryAdmissionInstalled`
was RED before parent installed the root hook; it is now GREEN.
The exact assignment and placement are in the
[integration contract](ieee1516-2010-sync-integration-contract-20260930.md).
Parent wired the common `acquireCallbackService` checkpoint/generation closure
to syncLifecycle.retryAdmission at run.go:1669, also shared with receipt admission.
Joined/Resigned remain outside that acquisition path. No run.go edit was made by
this worker. The reproduced lifecycle defect is closed by bounded acceptance;
G13 remains a separate unfinished milestone rather than a synchronization failure.

The three new retry-admission helper tests passed Windows acceptance and the
offline Linux race detector with `-count=20` (1.412s). Existing actual-root TCP,
SDK, checkpoint replay and restart tests also passed after the helper change.
The production installation is tested separately and now passes. After parent
integration, focused Windows root/lifecycle/TCP/replay/restart/FDD/live-FOM checks
passed (1.444s). Offline Linux `-race` selecting all `TestSynchronization` tests
and `TestRestoreRestartFreshGenerationAllManagerState` with `-count=10` passed
(28.261s). The earlier missing-root RED was not hidden by helper-only success.

## Earlier Delivered Milestone

- [Whole-RTI audit](ieee1516-2010-completion-audit-20260930.md) and
  [selected G01-G23 inventory](ieee1516-2010-completion-inventory-20260930.json).
  The normative inventory remains incomplete; recognized-authority qualification
  has not occurred and no certification claim is made.
- G06-G09 domain corrections in hla/synchronization: duplicate registration uses
  failed confirmation; empty/omitted target sets select all; outsiders cannot
  achieve; all-member points expand on late join only until the all-achievements
  boundary; explicit nonempty sets do not expand; resign removes participants.
- Atomic all-recipient callback admission before service mutation; committed
  membership changes retain unsent obligations and have ordered retry.
- Label epochs and callback identities distinguish reused labels. Strict label
  retirement waits for synchronized callback COMPLETION, not queue/stream send.
- Announcement ENTRY authorizes strict achievement. An entered handler may
  reenter achievement before announcement completion. Never-invoked queued
  announcements are rejected.
- Exact detached DTO and GSY2 checkpoint codec preserve membership mode, epoch,
  entry/completion ledger, failed achievements and unsent callback obligations.
  Strict active legacy images without this evidence are rejected, not synthesized.
- Version-3 Obligations retains all four mandatory callback kinds after queue
  admission, including registration confirmations independent of point retirement.
  Restore reconstructs retry work with original payloads, sequences and epochs.
  Entered callbacks are not mistaken for completed callbacks.
- The shared savebundle.StrictSynchronization adapter validates/rebinds the DTO
  through RestoreGraph. Typed handles are rewritten; callback identities are not.
  Lifecycle retry discovers restored pending federations without a join/resign
  error wakeup. The all-manager cross-generation restart fixture now passes.
- New core lifecycle/entry/completion ports and application helpers are documented
  in the [integration contract](ieee1516-2010-sync-integration-contract-20260930.md).

## TDD and Verification

The original behavior was RED for duplicate registration, outsider achievement,
and explicit empty target sets. A separate RED reproduced strict achievement
being accepted before announcement invocation. A new RED showed that four
admitted callbacks disappeared on restore. After correction, all 17 new domain
tests, three application-helper tests and two strict adapter tests pass. Direct
production TCP tests pass all four time-implementation/stream combinations.
Two live Go SDK cases verify entry before the handler, reentrant achievement,
same-ticket handler deduplication and completion after a throwing handler.
Two fresh-root TCP checkpoint-replay cases retain registration confirmation and
both announcements after admission, rewrite handles for a new generation and
reverse join order, and finish obligations only through authenticated invocation
reports. This bounded adapter/root test complements rather than replaces the
whole save/restore coordinator restart test.

| Check | Latest outcome |
|---|---|
| Focused domain/application tests, Windows Go 1.26.5 | PASS |
| Focused domain/application race tests, Ubuntu-24.04 Go 1.22.2, offline cached modules | PASS; no race report |
| Entire synchronization package | PASS |
| Entire savebundle package | PASS |
| Entire gRPC adapter package | PASS |
| Six complete Windows packages: synchronization/savebundle/grpc/server/M37/DDM registration | PASS, including all new live SDK and replay cases |
| Direct production TCP callback lifecycle | PASS: Float64/Integer64 x Events/EventBatches |
| Cross-generation whole-manager restart with strict sync DTO | PASS |
| Live Go SDK callback dispatch | PASS: Float64 and Integer64 |
| Fresh-root TCP strict checkpoint replay | PASS: Float64 and Integer64 |
| Extra entire Go SDK package | PASS after owner session metadata correction |
| Fresh seven-package campaign | PASS all seven packages |
| Synchronization retry-generation fix | GREEN, root wired and freshly verified |
| New production retry-admission guard | PASS |
| Fresh rtid binary build, Windows Go 1.26.5 | PASS; module stat-cache write warning, output binary verified |
| Owned tracked-file diff whitespace check | PASS |

The binary is outside Git:
`D:/workspace/gorti/rtid-sync-durable-restore-20260930.exe`,
SHA-256 `dfaa595f880cbcb6a962eef54e517490541d3f06b20187ce8dd4f9c8b4fc844d`.
It is build evidence only. Parent wired strict root construction, lifecycle hooks,
retry and entry/completion multicast. Direct live TCP tests exercise that current
production composition; the binary is not claimed to have passed Java/Python
SDK acceptance or external qualification.

Earlier MOM/resign/DDM failures no longer appeared in the six-package campaign.
The shared DDM collector now reports authenticated entry before its callback
body and completion afterwards; all DDM-registration tests pass. M37's duplicate
registration expectation now uses the standard failed confirmation, not an extra
synchronous error. The malformed Protobuf codec test continues to reject errors
without panicking when the adapter has no manager.

The new SDK acceptance first found a distinct integration gap: join did not
advertise CallbackInvocationEntrySupported. Parent added conditional negotiation
with positive/negative adapter tests. Both real Go SDK cases are now green, without
weakening the domain or spoofing a private SDK capability field. Temporary compile
failures in concurrent MOM transportation/test changes were corrected by their
owner before the final campaign.

The extra full Go SDK package exposes an independent regression:
`TestConfirmedUnaryContextPreservesSessionAndCallerMetadata` at session_test.go:23
reports `confirmed unary lost authenticated session: map[x-fixture:[kept]]`.
Its owner corrected this failure. This worker has not edited SDK files. The
subsequent fresh seven-package run passed all packages: synchronization 0.701s,
savebundle 0.797s, grpc 8.710s, server 21.278s, M37 0.618s, DDM registration
33.114s and Go SDK 1.020s.

## Exact Acceptance Commands

Run from `D:/workspace/gorti/gorti`, with the existing cached modules and
`GOPROXY=off`. Windows uses Go 1.26.5 and
`GOCACHE=D:/workspace/gorti/go-cache-sync-completion-20260930`.

```powershell
go test ./rti/internal/hla/synchronization ./rti/internal/adapters/savebundle ./rti/internal/adapters/grpc ./rti/internal/application/server ./rti/tests/integration/spec/M37 ./rti/tests/integration/ddm_registration ./rti/pkg/federate -count=1 -timeout=240s
```

All seven packages now pass after the independent metadata correction. Direct
current-root acceptance independently passed:

```powershell
go test ./rti/internal/application/server -run 'TestSynchronizationProductionTCP|TestRestoreRestartFreshGenerationAllManagerState' -count=1 -timeout=180s
```

Focused race campaign passed all four packages under the installed Ubuntu-24.04
Go 1.22.2, using offline cache `/mnt/d/workspace/gorti/.tools/gomodcache`:

```sh
go test -race ./rti/internal/hla/synchronization ./rti/internal/adapters/savebundle ./rti/internal/adapters/grpc ./rti/internal/application/server -run 'TestCompletion|TestSynchronization|TestStrictSynchronization|TestRestoreRestartFreshGenerationAllManagerState|TestMandatoryCallback|TestCallbackEntry|TestSupportCallbackEntry|TestJoinCallbackEntryCapability' -count=1 -timeout=180s
```

Exact new root TCP test names are
`TestSynchronizationProductionTCPInvocationLifecycle`,
`TestSynchronizationProductionTCPGoSDKReentrantCallbacks`, and
`TestSynchronizationProductionTCPStrictCheckpointReplay`.
The existing `TestRestoreRestartFreshGenerationAllManagerState` preserves the
active valid synchronization point, authenticates entry/completion and checks
failed-achievement identity after cross-generation restore. It was not weakened
to accept a missing point.

A fresh combined root-fixture run also passed (0.839s), selecting those three TCP
tests and the restart test together with
`TestMOMCompletionReviewDynamicFDDPublishesToSubscribers`,
`TestMOMCompletionReviewLateSubscriptionReceivesHostAndFDD`,
`TestLiveFOMJoinProductionWireAndSnapshot`, and
`TestRestoreGeneratedRateBundleUsesSavedFOMCandidate`.

The subsequent isolated G13 regression file deliberately introduces semantic
REDs in the separate `tests/unit/object` package. It does not modify this frozen
synchronization milestone. See the
[next-milestone review](ieee1516-2010-g13-delayed-subscription-review-20260930.md).

## Remaining Integration Blockers

1. Parent retains this frozen synchronization milestone while separately assessing
   newly integrated service changes; negotiation and full Go SDK tests now pass.
2. Hubble's actual Python/Java SDK campaigns must exercise the same receipt and
   reentrant boundaries; their internal tests are not substituted for live proof.
3. Whole-process recovery must not claim opaque-ticket continuity or exactly-once
   application execution from the domain DTO alone. Its proven boundary is durable
   callback kind/payload/sequence/epoch plus entry/completion history and requeue.
4. Root membership resolution must not reacquire a blocking membership reader
   while a caller already holds a read lease and a writer is waiting. Exercise
   registration concurrent with join/resign in the actual-server race campaign.
5. Run a freshly built actual RTID with authenticated Go/Python/Java clients,
   including reentrant announce-handler achievement, blocked/throwing synchronized
   callbacks, stale/duplicate reports, late join/resign, capacity failure and restore.
6. Resolve the specific page-59 departed-designator/membership redline tension
   against a clean edition or applicable correction; do not manufacture an oracle.

## Freeze Readiness

The earlier owned source interfaces and installed lifecycle/receipt hooks passed
broad/race acceptance. The subsequently reproduced retry-generation race reopens
the freeze, then the helper fix and installed root admission passed fresh focused
Windows and repeated Linux race acceptance. The owned sync milestone is GREEN
again; parent owns the broader final release campaigns and separate G13 work. The
broader release assessment and other service milestones. Whole normative inventory
and recognized-authority qualification remain unresolved proof work; the page-59
source decision is also not declared complete.

No commits, production root edits, existing receipt-tracker edits, schema/SDK
edits, declaration/ownership/deletion edits, evidence-manifest edits, claim toggles,
downloads or docs/assets edits were made in this work.
