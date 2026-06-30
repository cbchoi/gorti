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
- `carrier` registers `car-race`, holds Position, stays joined.
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
