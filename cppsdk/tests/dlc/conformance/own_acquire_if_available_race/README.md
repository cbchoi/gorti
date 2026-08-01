# own_acquire_if_available_race

Two federates simultaneously call `attributeOwnershipAcquisitionIfAvailable`
(§7.9) on the same attribute of a carrier-owned object. Exactly one
succeeds (gets §7.7 `attributeOwnershipAcquisitionNotification`); the
other gets §7.10 `attributeOwnershipUnavailable`.

The fixture requires the §7.9
`attributeOwnershipAcquisitionIfAvailable` service and the §7.10
`attributeOwnershipUnavailable` callback.

Three federates:
- `carrier` registers `car-race`, then §7.2-divests Position and stays
  joined. Section 7.9 acquisitionIfAvailable only acquires UNOWNED
  attributes and never solicits release from an owner, so this divestiture
  is required for a compliant race.
- `bob` (racer) calls `acquireIfAvailable`.
- `carol` (racer) calls `acquireIfAvailable` at the same moment.

The race winner is non-deterministic; the goldens capture the
"bob-wins / carol-loses" branch. The opposite winner is also
spec-compliant; role-specific goldens must be swapped when exercising
that branch.

## Spec citations per event in goldens

The traceability lint enforces these citations:

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

## Status

**SPEC-FULL 17/17 strict** (carrier 5/5, bob 6/6, carol 6/6). The §7.9
`acquireIfAvailable` operation uses a real wire flag; the server answers
deny-fast atomically without queuing a §7.4 pending acquire. Carol's
§7.10 `ownershipUnavailable` callback is deferred to the
evoke queue so it prints after her own ACQUIRE_IF_AVAILABLE line.

Golden-branch choreography is manual because the racer takes its name as argv[1]
and the scripted driver cannot drive it: carrier → wait
UNCONDITIONAL_DIVEST → bob → SIGSTOP bob right after his
ACQUIRE_IF_AVAILABLE line (the grant is already committed server-side
inside the RPC) → carol runs to completion (denied while bob holds) →
SIGCONT bob (drains §7.7, resigns).

Timing limitation: if carol's
acquire lands after bob's CANCEL_THEN_DELETE_THEN_DIVEST resign,
Position is unowned again and her if-available legitimately succeeds.
That is the spec-correct other branch, not a defect.
