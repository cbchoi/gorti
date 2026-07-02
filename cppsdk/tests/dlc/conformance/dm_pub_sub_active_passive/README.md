# dm_pub_sub_active_passive

Subscriber toggles `subscribeObjectClassAttributes(active=false)` →
`active=true`. Per IEEE 1516.1-2010 §5.6:

- `active=false` (passive subscribe) MUST NOT fire
  `startRegistrationForObjectClass` (§5.10) on the publisher.
- `active=true` MUST fire it exactly once.

The golden enforces this: the publisher log contains exactly **one**
`PUB: START_REGISTRATION` line, occurring after the subscriber's
active=true subscribe.

Locks catalogue row 11.9 BLOCKING (gorti M17 has no `active` flag on
`subscribeObjectClassAttributes`) and row 4.16 MAJOR (gorti M17 has no
declaration-mgmt callbacks at all).

M31 dispatch plan §2.2 fixture #6.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: PUBLISH` — §5.3 publishObjectClassAttributes
- `PUB: ADVISORY_SWITCH` — §6.20 enableObjectClassRelevanceAdvisorySwitch
- `PUB: START_REGISTRATION` — §5.10 startRegistrationForObjectClass (catalogue row 4.16; fires only after active=true)
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes (catalogue row 11.9: active flag)
- `SUB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E TASK-363 clears.

## gorti parity status (M35, parity-CC)

Publisher PARTIAL 5/6, subscriber FULL 5/5. Captured run:
`gorti-captured.{publisher,subscriber}.log`.

Missing event (exactly one): `PUB: START_REGISTRATION class=Vehicle`.

gorti never fires §5.10 startRegistrationForObjectClass: there is no
FederateEvent oneof slot for it (proto/rti/v1/stream.proto) and no Go
emitter, and the callback is not declared on the M17 Cut-1
FederateAmbassador, so the DLC bridge has nothing to convert
(catalogue row 4.16). Consequently the fixture's core assertion —
START_REGISTRATION fires EXACTLY ONCE, only on the active=true
subscribe and not on the passive one — is unobservable: the `active`
flag itself is accepted-but-ignored in the DLC layer
(RTIambassadorImpl.cpp, documented M34-AC divergence, catalogue row
11.9), so gorti cannot express passive-subscribe semantics either.

Missing impl: (1) server-side ObjectClassRelevance advisory emission
(StartRegistrationForObjectClass/StopRegistrationForObjectClass proto
events + registry publication/subscription cross-check), (2) passive
flag plumbing, (3) bridge conversion.

Publisher wait loop uses the §10.42 evoke-drain pattern so the event
will be captured as soon as the impl lands.

## M37 EE final verdict (2026-07-02) — integrated main

**SPEC-FULL 11/11** (publisher 6/6, subscriber 5/5;
`_harness/run_fixture.sh dm_pub_sub_active_passive`). The M35 residual
is closed: §5.10 startRegistrationForObjectClass is on the wire and
bridged (M37 EA/EC), and `PUB: START_REGISTRATION class=Vehicle` fires
exactly once after the subscriber's active=true re-subscribe. No
residual.
