# own_acquire_if_available_race

Two federates simultaneously call `attributeOwnershipAcquisitionIfAvailable`
(§7.9) on the same attribute of a carrier-owned object. Exactly one
succeeds (gets §7.7 `attributeOwnershipAcquisitionNotification`); the
other gets §7.10 `attributeOwnershipUnavailable`.

M31 dispatch plan §2.2 fixture #16. Enforces divergence catalogue:
- Row 12.3 BLOCKING — `attributeOwnershipAcquisitionIfAvailable` is
  spec-mandated but absent from gorti M17.
- Row 4.29 MAJOR — `attributeOwnershipUnavailable` callback absent
  from gorti M17.

Three federates:
- `carrier` registers `car-race`, then §7.2-divests Position and stays
  joined (parity-CD fix: the M31 carrier held Position throughout, but
  §7.9 acquisitionIfAvailable only acquires UNOWNED attributes and
  never solicits release from an owner — with the carrier owning
  Position, the bob-wins golden was unreachable under any compliant
  RTI; golden updated accordingly, spec-wins).
- `bob` (racer) calls `acquireIfAvailable`.
- `carol` (racer) calls `acquireIfAvailable` at the same moment.

The race winner is non-deterministic; the goldens capture the
"bob-wins / carol-loses" branch. If Pitch reports the other branch,
the goldens should be swapped at TASK-363 capture time.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Carrier

- `CARRIER: CONNECT` — §4.2 connect
- `CARRIER: JOIN` — §4.9 joinFederationExecution
- `CARRIER: REGISTER` — §6.8 registerObjectInstance
- `CARRIER: UNCONDITIONAL_DIVEST` — §7.2 unconditionalAttributeOwnershipDivestiture
- `CARRIER: RESIGN` — §4.10 resignFederationExecution

### Bob (winner)

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: DISCOVER` — §6.9 discoverObjectInstance
- `BOB: ACQUIRE_IF_AVAILABLE` — §7.9 attributeOwnershipAcquisitionIfAvailable
- `BOB: ACQUISITION_NOTIFICATION` — §7.7 attributeOwnershipAcquisitionNotification
- `BOB: RESIGN` — §4.10 resignFederationExecution

### Carol (loser)

- `CAROL: CONNECT` — §4.2 connect
- `CAROL: JOIN` — §4.9 joinFederationExecution
- `CAROL: DISCOVER` — §6.9 discoverObjectInstance
- `CAROL: ACQUIRE_IF_AVAILABLE` — §7.9 attributeOwnershipAcquisitionIfAvailable
- `CAROL: OWNERSHIP_UNAVAILABLE` — §7.10 attributeOwnershipUnavailable
- `CAROL: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Links fail with undefined reference to `rti1516e::*`. Goldens are
`TBD-pitch-capture` placeholders until Agent E's TASK-363 clears.

## gorti parity status (M35, parity-CD)

Verdict: **SPEC-PARTIAL 16/17** strict (17/17 by content; carrier
FULL 5/5, winner FULL 6/6, loser 5/6). Goldens are spec-derived —
Pitch capture BLOCKED (>2 federates exceeds Pitch Free EULA cap).
Captured run: `gorti-captured.{carrier,bob,carol}.log` (a bob-wins
run; the winner is non-deterministic per the golden's own NOTE).

The single strict mismatch: the loser's
`OWNERSHIP_UNAVAILABLE attrs=1` appears BEFORE her
`ACQUIRE_IF_AVAILABLE` line. gorti has no §7.9 wire method; the DLC
(parity-CA) emulates it as per-attribute query → owned subset gets
§7.10 `attributeOwnershipUnavailable` synthesized synchronously
*inside* the §7.9 call, so the callback precedes the fixture's own
post-call print. The winner's §7.7 arrives as a real OwnershipAcquired
stream event through the evoke loop, in golden order.

### Atomicity divergence (observed 1/5 runs)

The emulation is query-then-acquire, NOT atomic. In 5 recorded runs:
4 produced the spec outcome (one §7.7 winner, one §7.10 loser); in
run 4 the loser received **neither §7.7 nor §7.10** — his query
observed Position unowned, he delegated to real §7.8 acquisition, the
other racer's acquire landed first, and the server silently queued
his acquire as a PendingAcquireEntry (rti/internal/ownership/
manager.go: acquire on an owned attribute emits no event). Two
consequences: (a) the loser times out with no §7.10, and (b) a stale
pending FULL acquisition remains queued server-side — if the winner
later divests, the loser would be GRANTED ownership he only requested
"if available", which §7.9 forbids (it must never wait on a future
divestiture).

### Fixture/scenario fixes in this pass (spec-wins golden edit)

1. Carrier now §7.2-divests Position after registering (+ golden line
   `CARRIER: UNCONDITIONAL_DIVEST attrs=[Position]`): with the M31
   carrier holding Position, no compliant RTI could deliver the
   golden's §7.7 win — both racers must get §7.10. Justification in
   expected.carrier.log header.
2. Racers must subscribe BEFORE the carrier registers (launch racers
   first): gorti has no late-joiner discovery — subscribing after
   registration never yields §6.9 discoverObjectInstance, and the
   racers then race against an invalid object handle (observed:
   acquire on the phantom handle still "succeeds" server-side since
   the ownership manager does not validate object existence).
3. Racer wait loops converted to the evoke-drain pattern.

Missing impl: real §7.9/§7.10 wire (atomic acquire-if-available in
ownership.Manager + AttributeOwnershipUnavailable stream event),
late-joiner discovery, object-handle validation in ownership RPCs.
