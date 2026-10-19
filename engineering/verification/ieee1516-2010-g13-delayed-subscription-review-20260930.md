# G13 Delayed Subscription Evaluation: Review and RED Milestone

Date: 2026-09-30. Internal review separate from the implementation owners.
This is not externally independent qualification or a whole-conformance claim.
No shared object/time production file was changed. The synchronization milestone
is frozen; the new G13 regression tests are deliberately RED for the next milestone.

## Source Oracle

[IEEE 1516.1-2010, 8.1.8, physical PDF page 191](C:/Users/cbchoi/Downloads/15161_2010-2004_redline.pdf#page=191)
is the active retained/added source. The corresponding local intake row is in
`D:/workspace/gorti/ieee2010-source-intake-20260921/1516.1-pages.jsonl`,
`pdf_page=191`. No deleted redline requirement is used for the following oracle.

The creation-time FDD switch is federation-wide and immutable. When enabled,
subscription evaluation occurs when a message becomes delivery-eligible. A
receiver's current subscriptions also determine the final delivered content.
When disabled, early rejection is permitted, not mandatory; a test must not force
all disabled implementations to drop a later-subscription message.

The new tests use a federate already joined before message generation, with time
constrained enabled and a future timestamp held by another federate's LBTS.
Changing that federate's subscription before eligibility must be considered.
They do not invent retrospective delivery to a federate joining after generation.
That separate membership question needs its own source/applicability analysis.

## Verified RED Evidence

New isolated black-box file:
`rti/tests/unit/object/delayed_subscription_review_test.go`.
It composes actual federation, declaration, object and time managers, a real FOM
repository and parsed FDD with `delaySubscriptionEvaluation isEnabled="true"`.
The already-subscribed control proves that the timestamp, LBTS, publication and
delivery setup works rather than turning a broken fixture into a standards gap.

```powershell
go test ./rti/tests/unit/object -run 'TestG13EnabledDelaySubscription' -count=1 -timeout=60s -v
```

Windows Go 1.26.5, offline existing module cache:

| Case | Result | Observation |
|---|---|---|
| Late interaction subscription before TSO eligibility | RED | No interaction callback reaches the receiver |
| Already-subscribed interaction control | PASS | Original payload reaches the receiver |
| Late second-attribute interest on an already discovered object | RED | Reflection contains first attribute only; second was irreversibly pruned |

The failing tests do not assert private queue lengths or implementation-specific
recipient algorithms. They assert externally meaningful domain callbacks and
payloads after legal time advancement. No production behavior was weakened.
Both failures reproduced in all three runs of `-count=3`; the subscribed control
continued to pass. This is a semantic RED, not a build failure.
The same passing control also passed the offline Linux Go 1.22.2 race check
(1.080s), with no race report. This limited control run is not described as race
acceptance of the still-unimplemented late-interest behavior.

## Actual Retention and Projection Boundaries

1. `object/interaction.go:sendInteractionTo` selects current interaction
   subscribers before `prepareFanoutReceiveTo`. Its wire twin also selects this
   same send-time set. A zero-subscriber candidate has no message in the time
   manager, so delivery filtering cannot recover it.
2. `object/update.go:prepareAttributeFanout` calls `recipientClasses`,
   `snapshotReflectRecipients`, then `attributeDeliveries`. Both recipient
   membership and attribute payload projection occur before time admission.
   Retaining one interested attribute is insufficient: other sent values can be
   needed by a subscription added while the same message is held.
3. `core.MessageDeliveryOf[T]` already carries typed timestamp, recipient,
   sender, retraction ID, RO classification and domain event. The time manager's
   `core.ReservableTSODeliveryGateOf[T]` provides atomic candidate admission.
   Reuse these rather than adding a new network protocol.
4. `core.QueuedMessageFilter.FilterQueuedMessage` is the existing delivery-time
   projection port. `time/ro_delivery.go:queuedMessage` calls it for normal held
   messages; TAR/NMR/FQR/RO release paths share that gateway. Its contract forbids
   mutating the retained source or reentering time evaluation.
5. `object/queued_message.go` currently projects an already materialized callback.
   It can remove current unwanted data but cannot restore absent recipients or
   missing attribute values. It also preserves an object's previously conveyed
   class: delay evaluation must not silently promote that known-class view.

## Precise Port Proposal: Coordinate Before Production Edits

**FDD delivery policy reader.** Add a narrow immutable FOM capability, for example
`DelaySubscriptionEvaluation() (enabled bool, declared bool)`. The application
FOM adapter must expose the parsed switch and correct default/applicability.
Do not misuse per-federate advisory switches or a mutable config flag. Freeze
the effective value at federation creation, including FOM extension/restore rules.

**Generation-pinned candidate roster.** The object consumer needs a narrow
`ListMembers(core.FederationName) []core.FederationMember` reader, sourced from
the authoritative federation roster rather than time-regulating states or
subscribers. Use it within the already acquired service generation/member lease.
Do not recursively acquire a read lease while a writer is pending. Preserve
recipient incarnation/generation admission and coordinated resign cleanup.
`Options.Federations` currently promises only mode lookup, so an explicit
consumer port or checked capability must be agreed with the composition owner.

**Retained candidate envelope.** Keep the immutable generated message with all
sent attribute/parameter bytes, registered class context, existing recipient
known-class view where applicable, tag, timestamp/order, retraction identity and
region context. For enabled delayed evaluation, retain recipients without
current interest, not just the send-time subscriber set. Resolve discover-before-
reflect semantics for newly interested unknown objects separately; the current
RED attribute test deliberately uses an already discovered object.

**Delivery projection.** Prefer extending the implementation behind the existing
`core.QueuedMessageFilter` rather than adding transport APIs. Projection returns
nil or a detached domain callback based on current subscription/hierarchy/DDM
interest and the immutable retained envelope. Preserve known-class handle
translation and prevent filtering one recipient from mutating another or a retry.
If discovery needs atomic callback admission, coordinate a separate preparation
port with object/time owners; do not perform hidden queue sends from the filter.

**Accounting.** Broad candidate retention must not count candidates as received
interactions/reflections. Existing interaction delivery hooks run for admitted
TSO candidates, so measure/update delivery accounting at actual projection and
successful callback admission. This needs MOM coordination, not a local rename.

## Next Milestone: Plan, Do, Review, Reflect

1. Plan: parent coordinates these narrow interfaces with Schrodinger/Fermat and
   G12 callback lifetime work. Resolve switch defaults and roster lease ownership.
   The reviewer owns only this new test/report; shared send files remain theirs.
2. Do: implement retained candidates and complete payload retention in both typed
   and wire send/update paths. Preserve atomic capacity admission, no-recipient
   timestamp validation, retraction and generation fencing. Turn the two REDs green.
3. Review: add RO held-message, inherited interaction/known-object classes,
   DDM interest/realization changes, unsubscribe-before-delivery, FQR/NMR ordering,
   same-time replacement, retract, failed reservation/retry and restore oracles.
   Cover Float64 and Integer64 without changing the wire schema.
4. Reflect: run focused race/concurrent subscription and resign tests, actual-root
   TCP callback traces and checkpoint tests. Measure retained-message memory and
   throughput; required delay semantics cannot be replaced with an early-pruning
   optimization. Record bounded proof and any unresolved applicability separately.

Acceptance is not complete after these two cases alone. The whole normative
inventory and formal qualification in G23 remain separate unresolved work.

No shared production changes, SDK/schema changes, `run.go` edits, commits,
downloads, existing evidence edits or conformance-claim toggles were made here.
