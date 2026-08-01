# dm_pub_sub_active_passive

Subscriber toggles `subscribeObjectClassAttributes(active=false)` →
`active=true`. Per IEEE 1516.1-2010 §5.6:

- `active=false` (passive subscribe) MUST NOT fire
  `startRegistrationForObjectClass` (§5.10) on the publisher.
- `active=true` MUST fire it exactly once.

The golden enforces this: the publisher log contains exactly **one**
`PUB: START_REGISTRATION` line, occurring after the subscriber's
active=true subscribe.

The fixture requires the `active` argument on
`subscribeObjectClassAttributes` and the declaration-management
`startRegistrationForObjectClass` callback.

## Spec citations per event in goldens

The traceability lint enforces these citations:

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: PUBLISH` — §5.3 publishObjectClassAttributes
- `PUB: ADVISORY_SWITCH` — §6.20 enableObjectClassRelevanceAdvisorySwitch
- `PUB: START_REGISTRATION` — §5.10 startRegistrationForObjectClass (fires only after active=true)
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes (`active` argument)
- `SUB: RESIGN` — §4.10 resignFederationExecution

Publisher wait loop uses the §10.42 evoke-drain pattern so the event
is captured reliably.

## Status

**SPEC-FULL 11/11** (publisher 6/6, subscriber 5/5;
`_harness/run_fixture.sh dm_pub_sub_active_passive`). The §5.10
`startRegistrationForObjectClass` callback is on the wire and bridged,
and `PUB: START_REGISTRATION class=Vehicle` fires
exactly once after the subscriber's active=true re-subscribe. No
residual.
