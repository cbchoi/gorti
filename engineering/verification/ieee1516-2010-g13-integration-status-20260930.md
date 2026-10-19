# G13 integration status

## Implemented, focused RED -> GREEN

Transferred `object/interaction.go`, `update.go`, and `queued_message.go` now
retain canonical messages for generation-time joined members when the FDD
delay-subscription switch is enabled. Current interest is evaluated at delivery
eligibility, including immediate admission. Object payload projection is deferred
until the recipient's known class is available. Existing immutable conveyed region
realizations and per-attribute unions are preserved; no schema/SDK/run.go changes.

New ports: `core.FOMSubscriptionEvaluation`, `core.SubscriptionEvaluationRoster`.
Composition's immutable FOM handle implements the switch query.
Internal callback metadata carries the delayed flag and registered object class,
so the existing typed time snapshot preserves canonical pending payloads.

Both original G13 semantic failures are GREEN:
`go test ./rti/tests/unit/object -run TestG13 -count=1 -timeout=60s` PASS 1.233s.
Time package also passed the initial broader attempt (1.790s).

## Object hot files frozen: 2026-10-01

Both original REDs and the typed Float64/Integer64 five-advance, canonical
attribute restore, inherited attribute/parameter projection, frozen publisher
DDM late-interest, retraction, unsubscribe, and disabled-switch controls pass.
The compile blocker from concurrent data-queue ports is resolved.

Fresh whole-package results:

| Package | Result |
| --- | --- |
| rti/tests/unit/object | PASS 1.061s |
| rti/internal/hla/object | PASS 0.937s |
| rti/internal/hla/time | PASS 8.044s |
| rti/internal/application/composition | PASS 1.796s |
| rti/internal/adapters/savebundle | One existing DDM persistence failure |

`TestG13CanonicalRestoreGraph` passes. Its narrow graph adaptation treats retained
attributes as registered-class coordinates, not prematurely projected known-class
coordinates; malformed canonical classes and disabled policy are rejected.

## Precise secondary persistence handoff to DDM owner

`TestDDMConveyedSnapshotPrecisionAndLeases` fails deterministically. The current
`adapters/savebundle/ddm.go:14-41` serializes only live regions/subscriptions/
publications/inactive associations and returns `proto.Marshal(wire)` without
`ManagerState.ConveyedRegions`. `PrepareRestore` (lines 61-101) likewise does not
restore those copies or pending/active receiver leases. This explains the exact
state mismatch, independently of G13's canonical time codec. Required follow-up:
compatible persistence of conveyed portable realizations and exact receiver lease
state, plus graph/rebind readers. No SDK/schema/run.go edit made by this worker.

No full-conformance claim is made. The attempted offline Linux race run returned
no test output and was interrupted; it is NOT PASS evidence. The parent may start
its fresh source campaign against the frozen three object hot files.

## Additional exact integration boundary

`run.go:1424-1425` wires generation-time `OnReflectDelivered` and
`OnInteractionDelivered` to MOM received counters. With enabled delayed
evaluation, generation-time joined candidates can remain uninterested and later
be discarded. They must not be counted as actual received data callbacks merely
because canonical candidate retention succeeded. Required narrow follow-up is a
successful queued-data admission observer after projection and atomic outbox
commit (covering all five advances, RO, retry, and restore), not a side effect in
`FilterQueuedMessage`. Filter evaluation can repeat on failed admission and must
remain pure. Non-delayed counting behavior must retain its compatibility profile.
This worker has not changed root MOM wiring or claimed that metric boundary closed.

The FDD switch is also checked during `PrepareExtension`: an absent creation-time
switch has effective false, and later modules may not change that effective value.
`TestG13FDDDelaySwitchImmutableAcrossExtension` PASS; composition G13 tests PASS
1.327s and savebundle G13 canonical graph tests PASS 0.857s.

## Final handoff

- No G13 test remains RED. Both independently reproduced defects now pass.
- No further scenario expansion or hot-file changes are in progress.
- The final five-package rerun has four whole packages GREEN and the single
  independently diagnosed DDM persistence failure above. Do not call the entire
  five-package run GREEN or conceal that existing failure.
- All new G13 graph tests are GREEN; the source-derived oracle covers Float64 and
  Integer64, all five advances, late known-object attributes, inherited payloads,
  sender-frozen DDM interest, retract/restore, and creation-switch immutability.
- Full SDK/root endpoint campaigns remain the parent's responsibility; this
  worker's G13 evidence is domain/service and adapter acceptance, not an assertion
  of an actual-endpoint G13 campaign or IEEE-wide closure.
- Previously requested destroyed-federation sync retry forgetting has not been
  implemented while prioritizing the requested focused G13 freeze. Background
  retry lease/root race correction remains GREEN as recorded separately; do not
  assume the additional destroy cleanup hook is wired.
- No commits, schema/SDK changes, run.go edits, existing evidence.json changes,
  claim/certification toggles, downloads, or docs/assets changes by this worker.

## Focused source review follow-up: 2026-10-01

The original redline PDF was visually checked at physical pages 107, 122 and 126;
the relevant discovery/known-object requirements are retained black text and/or
active blue additions, not deleted red strike-through text. Page 191's active
delay-evaluation rule remains the governing subscription-time oracle. No extended
source quotations or source-page images are added to Git.

- [6.1.1, discovery and fixed discovered class](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=107).
- [6.9.3-4, discovery preconditions and known-object postcondition](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=122).
- [6.11.3(c), reflection requires a known object](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=126).
- [8.1.8, delivery-eligibility subscription evaluation and immutable switch](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=191).

### New unknown-object oracle

`delayed_subscription_unknown_object_test.go` uses a receiver already joined but
unsubscribed when an owned object is registered and updated. Retention alone must
not discover it. After subscribing before TSO eligibility, exactly one discovery
must precede exactly one correctly projected reflection. The oracle does not
require discovery to occur within the subscribe RPC. The discovered class remains
unchanged while the object remains known. Float64/Integer64 ordinary, ancestor and
typed-time restore cases, plus an uninterested control, all pass. This is service
outbox ordering evidence, not SDK callback-invocation or actual-TCP evidence.

### Cache review: two new semantic REDs -> GREEN

The existing `TestSendInteraction_CachesNormalClassClassificationPerFederation`
two-Get assertion is unchanged and GREEN. The earlier 6-vs-2 result predates the
cached-classification-FOM reuse already present in `interaction.go`.

A new revision oracle then reproduced a genuine separate defect: after replacing
revision 1 with revision 2, policy queries still hit revision 1 four times and
revision 2 zero times. A second oracle reproduced live join extension accepting an
absent/false creation switch changed to true, because only preview extension had
the immutable-switch guard. Both REDs are now GREEN.

`core.FOMHandleReferenceRepository` supplies a stable reference to committed
immutable FOM handles. Production composition publishes it atomically alongside
RememberFor, live extension Commit, and ForgetFor. Object state retains the
reference, not an unversioned copied switch value; each policy query loads the
current committed handle. New object state on restore naturally clears the
reference/classification caches. Non-reference immutable compatibility repositories
reuse their cached classification handle, preserving the original Get-count oracle.
No extra Get, copied full FOMView, or per-message repository lock is needed for
production reference reads. Preparation does not publish a candidate revision.

Exact cache proof: revision 1 queries=2, revision 2 queries=2, Get=1; after object
restore, latest-revision queries=3 and Get=2. The real composition reference test
also checks pre-commit isolation, committed revision 2, forget-to-nil, and recreate.
The existing immutable-switch validator now covers `PrepareLiveExtension` too.

### Coordinated MOM integration

`FilterQueuedMessage` now invokes Schro's
`FilterMOMAttributePointMessage` first and returns whenever handled, before
delayed projection/default-region routing. This adds no MOM type or routing
implementation and does not erase point metadata. Fermat owns point persistence;
Hubble owns received-counter boundary review; Parfit owns sync destroy cleanup.

### Fresh bounded acceptance

Whole packages: unit object PASS 0.310s, HLA object PASS 0.246s, HLA time PASS
0.957s, composition PASS 0.908s. Savebundle still has the existing conveyed
precision/receiver-lease failure described above.

Fresh offline Linux race run, explicit package names, no regex filter, count=1:

| Package | Race result |
| --- | --- |
| rti/tests/unit/object | PASS 1.706s |
| rti/internal/hla/object | PASS 1.374s |
| rti/internal/hla/time | PASS 1.485s |
| rti/internal/application/composition | PASS 7.886s |

The command exited 0 and returned all four package results. Unlike prior
interrupted attempts, this is genuine focused race evidence. This review pass is
not external independent qualification. All source changes for this focused
handoff are frozen; no newly added RED tests remain unresolved.

Sync retry/root milestone previously GREEN; permanent missing-federation retry
cleanup requested additionally and remains queued after G13 priority work.
