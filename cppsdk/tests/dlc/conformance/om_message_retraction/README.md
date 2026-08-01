# om_message_retraction

Publisher sends a TSO interaction with retraction handle, then calls
`retract(handle)`; subscriber's `requestRetraction` callback fires
BEFORE the original `receiveInteraction` would have delivered.

This is technically the §8.21/§8.22 path (time-management surface)
applied to the §6 object/interaction path.

## Scenario

1. **Publisher** enables time regulation (§8.2) with 1.0 lookahead,
   publishes `Honk`.
2. Publisher calls
   `sendInteraction(honk, params, tag, time=10.0)` — the §6.12
   TSO+retract overload returning `MessageRetractionHandle`.
3. Publisher immediately calls `retract(handle)` (§8.21) with that DLC
   handle object.
4. **Subscriber** is time-constrained, advances to t=15.
5. RTI fires `requestRetraction(handle)` on subscriber (§8.22) BEFORE the original
   `receiveInteraction` would deliver.
6. The original interaction MUST NOT be delivered. The subscriber's
   defensive `receiveInteraction` override emits `SUB: RECEIVE_SPURIOUS`
   if invoked; its appearance fails the golden diff.

## Purpose

This is the single hardest behavioral test in §6/§8 surfaces:
- Locks `sendInteraction` TSO+retract overload signature.
- Locks `retract` taking a real `MessageRetractionHandle` class.
- Locks the `requestRetraction` callback.
- Locks the ordering invariant: retraction wins, original is suppressed.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: TIME_REGULATION_ENABLED` — §8.3 timeRegulationEnabled
- `PUB: PUBLISH` — §5.7 publishInteractionClass
- `PUB: SEND_TSO` — §6.12 sendInteraction TSO+retract overload
- `PUB: RETRACT` — §8.21 retract
- `PUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: TIME_CONSTRAINED_ENABLED` — §8.6 timeConstrainedEnabled
- `SUB: SUBSCRIBE` — §5.8 subscribeInteractionClass
- `SUB: REQUEST_RETRACTION` — §8.22 requestRetraction callback
- `SUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant
- `SUB: RESIGN` — §4.10 resignFederationExecution

Fixture side is ready: callback-wait loops and the spurious-RECEIVE
window use the §10.42 evoke-drain pattern.

## Status

**SPEC-FULL 15/15** (publisher 8/8, subscriber 7/7;
`_harness/run_fixture.sh om_message_retraction`). The §8.22
`requestRetraction` callback is on the wire with real retraction handles,
`SUB: REQUEST_RETRACTION handle=<H>` is delivered,
and the retracted Honk stays suppressed (no RECEIVE_SPURIOUS). No
residual.
