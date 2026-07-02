# own_negotiated_divest_two_phase

Two-federate negotiated divest exercising the IEEE 1516.1-2010 §7
ownership transfer state machine (figure 7.1):

1. **Alice** owns `car-divest.Position` and calls
   `negotiatedAttributeOwnershipDivestiture(object, attrs, tag)` (§7.3).
2. RTI fires `requestAttributeOwnershipAssumption(object, attrs, tag)`
   on **Bob** (§7.4).
3. Bob calls `attributeOwnershipAcquisition(object, attrs, tag)` (§7.8 —
   the **tag** arg is the divergence catalogue row 12.2 BLOCKING fix).
4. RTI fires `requestDivestitureConfirmation(object, attrs)` on Alice
   (§7.5 — spec-correct callback per IEEE 1516.1-2010
   FederateAmbassador.h line 414; M31 fixture used the non-existent
   `attributeOwnershipDivestitureNotification`; M33-K-2 fix).
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

## M31 status

RED. Links fail with undefined reference to `rti1516e::*`. Goldens are
`TBD-pitch-capture` placeholders until Agent E's TASK-363 (Pitch EULA
review) clears.

## gorti parity status (M35, parity-CD)

Verdict: **SPEC-FULL 14/14** (alice 7/7, bob 7/7; goldens are
spec-derived, Pitch capture still pending). Captured run:
`gorti-captured.{alice,bob}.log`. Both federate wait loops use the
evoke-drain pattern (gorti M17 delivers callbacks on the evoking
thread).

Every golden line matches, including both §7 callbacks: Bob receives
§7.4 `requestAttributeOwnershipAssumption` (server `fanoutAssumption`
to attribute subscribers on §7.3 divest) and Alice receives §7.5
`requestDivestitureConfirmation`.

### Semantic divergence: two-phase collapses to one-phase server-side

Under IEEE 1516.1-2010, ownership must not transfer until the
divester's §7.6 `confirmDivestiture`; only then does the RTI fire
§7.7 on the acquirer. In gorti the transfer completes **eagerly
inside the acquirer's Acquire RPC**
(`rti/internal/ownership/manager.go` `completeTransfer`): it flips
the owner record, then emits `ownership_divest_confirmed` (§7.5) to
the old owner and `ownership_acquired` (§7.7) to the acquirer,
back-to-back. There is no ConfirmDivestiture RPC at all
(proto/rti/v1/ownership.proto has 8 RPCs; none is confirm), so the
DLC `confirmDivestiture` is a documented no-op (parity-CA) — §7.5
arrives as a fire-and-forget notification, not a request awaiting
confirmation.

Consequences invisible to this fixture's line set but real:
- Bob's §7.7 can arrive **before** Alice calls (no-op)
  `confirmDivestiture`; if Alice instead decided to cancel
  (§7.9 cancelNegotiatedAttributeOwnershipDivestiture) after §7.5,
  she could not — ownership is already gone.
- A denial path (never confirming) cannot keep ownership with Alice
  once Bob has called §7.8.

Missing impl for spec-faithful two-phase: a ConfirmDivestiture RPC,
server-side pending state that holds the transfer between the
acquirer's §7.8 and the divester's §7.6, and completion emission only
on confirm.

## M37 EE re-verdict (2026-07-02) — integrated main, REAL two-phase

**SPEC-FULL 14/14** (alice 7/7, bob 7/7) re-confirmed against the real
§7.6 two-phase handshake (M37 EA/EC: ConfirmDivestiture RPC; the M35
"eager one-phase transfer inside the acquirer's RPC" residual is
gone). The golden's order — alice REQUEST_DIVESTITURE_CONFIRMATION →
alice CONFIRM_DIVESTITURE → bob ACQUISITION_NOTIFICATION — is now
produced by the actual gated transfer rather than emulation. No
residual.
