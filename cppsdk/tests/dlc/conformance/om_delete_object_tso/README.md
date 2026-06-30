# om_delete_object_tso

TSO `deleteObjectInstance` — subscriber's `removeObjectInstance` fires
with the right `LogicalTime`. M31 dispatch plan §2.2 fixture #10.

## Scenario

Two time-managed federates exercise §6.14 / §6.15:

1. **Publisher** enables time regulation (§8.2) with a 1.0 lookahead,
   registers `car-tso`, advances to t=5.
2. Publisher calls `deleteObjectInstance(object, tag, time=10.0)`
   (§6.14 — the 2-arg TSO overload added per catalogue row 11.5;
   M17 has NO `deleteObjectInstance` at all).
3. **Subscriber** enables time constrained (§8.5), advances to t=15.
4. Subscriber's `removeObjectInstance(object, tag, sentOrder, time,
   receivedOrder, retraction, reflectInfo)` fires (§6.15 — 3-overload
   form per catalogue row 4.22; M17 absent).
5. The `time` parameter MUST equal the publisher's chosen
   `LogicalTime(10.0)` and MUST fire before the subscriber's
   `timeAdvanceGrant(15.0)`.

## Why this fixture

Catalogue row 11.5 (MAJOR): `deleteObjectInstance` is entirely absent
from gorti M17. Row 4.22 (MAJOR): subscriber-side `removeObjectInstance`
3-overload callback set is also absent.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: TIME_REGULATION_ENABLED` — §8.3 timeRegulationEnabled callback (catalogue row 4.33)
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant (catalogue row 4.35)
- `PUB: DELETE_TSO` — §6.14 deleteObjectInstance TSO overload (catalogue row 11.5)
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: TIME_CONSTRAINED_ENABLED` — §8.6 timeConstrainedEnabled (catalogue row 4.34)
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REMOVE_TSO` — §6.15 removeObjectInstance TSO+retract overload (catalogue row 4.22)
- `SUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant
- `SUB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. `WILL_FAIL TRUE` per dispatch plan §3 criterion 2. Goldens are
`TBD-pitch-capture` until Agent E's TASK-363 (Pitch EULA review) clears.
