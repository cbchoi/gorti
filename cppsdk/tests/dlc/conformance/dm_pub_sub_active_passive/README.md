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
