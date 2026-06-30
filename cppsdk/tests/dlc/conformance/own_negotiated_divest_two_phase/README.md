# own_negotiated_divest_two_phase

Two-federate negotiated divest exercising the IEEE 1516.1-2010 §7
ownership transfer state machine (figure 7.1):

1. **Alice** owns `car-divest.Position` and calls
   `negotiatedAttributeOwnershipDivestiture(object, attrs, tag)` (§7.3).
2. RTI fires `requestAttributeOwnershipAssumption(object, attrs, tag)`
   on **Bob** (§7.4).
3. Bob calls `attributeOwnershipAcquisition(object, attrs, tag)` (§7.8 —
   the **tag** arg is the divergence catalogue row 12.2 BLOCKING fix).
4. RTI fires `attributeOwnershipDivestitureNotification` on Alice (§7.5).
5. Alice calls `confirmDivestiture(object, attrs, tag)` (§7.6 — added
   per divergence catalogue row 12.1; gorti M17 had no equivalent).
6. RTI fires `attributeOwnershipAcquisitionNotification(object, attrs, tag)`
   on Bob (§7.7 — divergence catalogue row 4.28: tag added, owningFederate
   dropped vs. M17).

M31 dispatch plan §2.2 fixture #15.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Alice

- `ALICE: CONNECT` — §4.2 connect
- `ALICE: JOIN` — §4.9 joinFederationExecution
- `ALICE: REGISTER` — §6.8 registerObjectInstance
- `ALICE: NEGOTIATED_DIVEST` — §7.3 negotiatedAttributeOwnershipDivestiture
- `ALICE: DIVESTITURE_NOTIFICATION` — §7.5 attributeOwnershipDivestitureNotification
- `ALICE: CONFIRM_DIVESTITURE` — §7.6 confirmDivestiture
- `ALICE: RESIGN` — §4.10 resignFederationExecution

### Bob

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: DISCOVER` — §6.9 discoverObjectInstance
- `BOB: ASSUMPTION_REQUEST` — §7.4 requestAttributeOwnershipAssumption
- `BOB: OWNERSHIP_ACQUISITION` — §7.8 attributeOwnershipAcquisition
- `BOB: ACQUISITION_NOTIFICATION` — §7.7 attributeOwnershipAcquisitionNotification
- `BOB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Links fail with undefined reference to `rti1516e::*`. Goldens are
`TBD-pitch-capture` placeholders until Agent E's TASK-363 (Pitch EULA
review) clears.
