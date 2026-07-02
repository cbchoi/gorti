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

## gorti parity status (M35, parity-CC)

BLOCKED(DLC §8 time surface unwired) — capture stops at 2/8 per side
(CONNECT, JOIN). Captured run: `gorti-captured.{publisher,subscriber}.log`.

The very first §8 call aborts both federates:
`enableTimeRegulation` / `enableTimeConstrained` throw from
`DLCRTIambassadorImpl` with "M17 time surface not yet wired into
DLCRTIambassadorImpl (M34 follow-up — needs a private M17 client
member on RTIambassadorImpl.h; tracked as 'M34 header pImpl' in the
dispatch plan)". Everything downstream (TAR/grant, the §6.14 TSO
delete, the §6.15 REMOVE_TSO callback) is unreachable through DLC.

Beyond the §8 wiring, this fixture will still need:
- DLC `deleteObjectInstance` (both overloads) wired — currently no-ops
  (RTIambassadorImpl.cpp:814-835; the TSO overload returns an invalid
  MessageRetractionHandle);
- `removeObjectInstance` declared on the M17 FederateAmbassador +
  bridge conversion (catalogue rows 11.5/4.22) — the server DOES emit
  it (rti/internal/object/delete.go, proto slot remove=12).

Fixture side is ready: all callback-wait loops use the §10.42
evoke-drain pattern, the subscriber gates its TAR on DISCOVER (so the
grant cannot race the TSO delete) and drains until both REMOVE_TSO and
the grant land.

### Update — after merging parity-CA §7/§8/§9 wire-through

Re-verdict: PARTIAL 15/16 (publisher 8/8 byte-identical, subscriber
7/8). The §8 time surface now works end-to-end through DLC
(TIME_REGULATION_ENABLED / TIME_CONSTRAINED_ENABLED / both grants all
captured). Sole missing event:

- `SUB: REMOVE_TSO handle=<H> sentOrder=TIMESTAMP time=10.000000 receivedOrder=TIMESTAMP`

because DLC `deleteObjectInstance` (TSO overload) is still a silent
client-side no-op — the delete never reaches the wire (the publisher's
DELETE_TSO line matches the golden because the no-op raises nothing).
Remaining impl: DLC deleteObjectInstance wire-through + M17
FederateAmbassador removeObjectInstance declaration + bridge converter
(server already emits remove=12, rti/internal/object/delete.go).
