# own_release_request_denied

Owner refuses release via `attributeOwnershipReleaseDenied(object, attrs)`
(§7.12). Two-federate scenario:

1. **Alice** owns `car-1.Position`.
2. **Bob** calls `attributeOwnershipAcquisition(object, attrs, tag)` (§7.8 with
   mandatory `tag` per divergence catalogue row 12.2).
3. RTI fires `requestAttributeOwnershipRelease(object, attrs, tag)` on
   Alice (§7.11, catalogue row 4.30 — gorti M17 absent).
4. Alice calls `attributeOwnershipReleaseDenied(object, attrs)` (§7.12,
   catalogue row 12.4 — gorti M17 absent).
5. Bob does NOT receive `attributeOwnershipAcquisitionNotification`;
   ownership stays with Alice.

M31 dispatch plan §2.2 fixture #17.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Alice

- `ALICE: CONNECT` — §4.2 connect
- `ALICE: JOIN` — §4.9 joinFederationExecution
- `ALICE: REGISTER` — §6.8 registerObjectInstance
- `ALICE: RELEASE_REQUEST` — §7.11 requestAttributeOwnershipRelease
- `ALICE: RELEASE_DENIED` — §7.12 attributeOwnershipReleaseDenied
- `ALICE: RESIGN` — §4.10 resignFederationExecution

### Bob

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: DISCOVER` — §6.9 discoverObjectInstance
- `BOB: OWNERSHIP_ACQUISITION` — §7.8 attributeOwnershipAcquisition
- `BOB: RESIGN` — §4.10 resignFederationExecution

The absence of an `ACQUISITION_NOTIFICATION` line in `expected.bob.log`
is itself the assertion that ownership transfer was refused.

## M31 status

RED. `TBD-pitch-capture` until Agent E TASK-363 clears.
