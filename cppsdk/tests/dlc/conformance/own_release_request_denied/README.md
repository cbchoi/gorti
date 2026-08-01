# own_release_request_denied

Owner refuses release via `attributeOwnershipReleaseDenied(object, attrs)`
(§7.12). Two-federate scenario:

1. **Alice** owns `car-1.Position`.
2. **Bob** calls `attributeOwnershipAcquisition(object, attrs, tag)`
   (§7.8 with the mandatory `tag`).
3. RTI fires `requestAttributeOwnershipRelease(object, attrs, tag)` on
   Alice (§7.11).
4. Alice calls `attributeOwnershipReleaseDenied(object, attrs)` (§7.12).
5. Bob does NOT receive `attributeOwnershipAcquisitionNotification`;
   ownership stays with Alice.

## Spec citations per event in goldens

The traceability lint enforces these citations:

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

## Status

**SPEC-FULL 11/11** (alice 6/6, bob 5/5;
`_harness/run_fixture.sh own_release_request_denied`). The goldens are
spec-derived. The §7.11
`requestAttributeOwnershipRelease` callback is on the wire and bridged:
`ALICE: RELEASE_REQUEST attrs=1` fires when
bob's acquire targets alice's owned attribute, alice denies, and bob
correctly receives no acquisitionNotification (the negative assertion
now holds for the right reason). No residual.
