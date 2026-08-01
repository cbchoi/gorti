# own_negotiated_divest_two_phase

Two-federate negotiated divest exercising the IEEE 1516.1-2010 §7
ownership transfer state machine (figure 7.1):

1. **Alice** owns `car-divest.Position` and calls
   `negotiatedAttributeOwnershipDivestiture(object, attrs, tag)` (§7.3).
2. RTI fires `requestAttributeOwnershipAssumption(object, attrs, tag)`
   on **Bob** (§7.4).
3. Bob calls `attributeOwnershipAcquisition(object, attrs, tag)` (§7.8 —
   the **tag** argument is mandatory).
4. RTI fires `requestDivestitureConfirmation(object, attrs)` on Alice
   (§7.5 — spec-correct callback per IEEE 1516.1-2010
   FederateAmbassador.h line 414).
5. Alice calls `confirmDivestiture(object, attrs, tag)` (§7.6).
6. RTI fires `attributeOwnershipAcquisitionNotification(object, attrs, tag)`
   on Bob (§7.7, including the tag and no owning-federate argument).

## Spec citations per event in goldens

The traceability lint enforces these citations:

### Alice

- `ALICE: CONNECT` — §4.2 connect
- `ALICE: JOIN` — §4.9 joinFederationExecution
- `ALICE: REGISTER` — §6.8 registerObjectInstance
- `ALICE: NEGOTIATED_DIVEST` — §7.3 negotiatedAttributeOwnershipDivestiture
- `ALICE: REQUEST_DIVESTITURE_CONFIRMATION` — §7.5 requestDivestitureConfirmation
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

## Status

**SPEC-FULL 14/14** (alice 7/7, bob 7/7). The goldens are spec-derived.
Both federate wait loops
use the evoke-drain pattern. The §7.6 `ConfirmDivestiture` RPC gates the
two-phase transfer, producing the golden's order: alice
REQUEST_DIVESTITURE_CONFIRMATION →
alice CONFIRM_DIVESTITURE → bob ACQUISITION_NOTIFICATION — is now
produced by the actual gated transfer. No residual.
