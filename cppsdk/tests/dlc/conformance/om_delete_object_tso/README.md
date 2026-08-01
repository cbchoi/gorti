# om_delete_object_tso

TSO `deleteObjectInstance` — subscriber's `removeObjectInstance` fires
with the right `LogicalTime`.

## Scenario

Two time-managed federates exercise §6.14 / §6.15:

1. **Publisher** enables time regulation (§8.2) with a 1.0 lookahead,
   registers `car-tso`, advances to t=5.
2. Publisher calls `deleteObjectInstance(object, tag, time=10.0)`
   (§6.14 TSO overload).
3. **Subscriber** enables time constrained (§8.5), advances to t=15.
4. Subscriber's `removeObjectInstance(object, tag, sentOrder, time,
   receivedOrder, retraction, reflectInfo)` fires (§6.15 timed form with
   retraction and supplemental reflect information).
5. The `time` parameter MUST equal the publisher's chosen
   `LogicalTime(10.0)` and MUST fire before the subscriber's
   `timeAdvanceGrant(15.0)`.

## Purpose

The fixture validates the timed `deleteObjectInstance` service and the
corresponding timed `removeObjectInstance` callback, including the
logical time, order, retraction handle, and supplemental information.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: TIME_REGULATION_ENABLED` — §8.3 timeRegulationEnabled callback
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant
- `PUB: DELETE_TSO` — §6.14 deleteObjectInstance TSO overload
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: TIME_CONSTRAINED_ENABLED` — §8.6 timeConstrainedEnabled
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REMOVE_TSO` — §6.15 removeObjectInstance TSO+retract overload
- `SUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant
- `SUB: RESIGN` — §4.10 resignFederationExecution

All callback-wait loops use the §10.42
evoke-drain pattern, the subscriber gates its TAR on DISCOVER (so the
grant cannot race the TSO delete) and drains until both REMOVE_TSO and
the grant land.

## Status

**SPEC-FULL 16/16** (publisher 8/8, subscriber 8/8, both byte-identical
to the goldens after canonicalization). The current behavior is:

1. `rti/internal/object/delete.go` resolves REMOVE
   recipients via `subscribersForDiscover` (the §6.9 recipient set:
   full `fanoutAttrProbe` range + DDM region subscribers) instead of
   the hardcoded `{1}` probe, so the Vehicle.Position subscriber is in
   the delete fan-out.
2. `rti/internal/time/ner.go` `emitGrant` releases buffered
   TSO BEFORE sending the grant (§8.14), so `REMOVE_TSO` precedes
   `TIME_ADVANCE_GRANT` in the subscriber stream.
3. `rti/internal/time/advance.go` grants TAR at EXACTLY
   the requested time (§8.10; incremental-grant-at-LBTS is now
   FQR-only), so the REMOVE buffered at 10 is released by TAR(15).

The harness compares both roles with their committed canonical goldens.
