# om_message_retraction

Publisher sends a TSO interaction with retraction handle, then calls
`retract(handle)`; subscriber's `requestRetraction` callback fires
BEFORE the original `receiveInteraction` would have delivered. M31
dispatch plan §2.2 fixture #14.

This is technically the §8.21/§8.22 path (time-management surface)
applied to the §6 object/interaction path; the M31 dispatch plan
groups it under object management (fixture #14 in §6 OM).

## Scenario

1. **Publisher** enables time regulation (§8.2) with 1.0 lookahead,
   publishes `Honk`.
2. Publisher calls
   `sendInteraction(honk, params, tag, time=10.0)` — the §6.12
   TSO+retract overload returning `MessageRetractionHandle` (catalogue
   row 11.4 BLOCKING — M17 has no retract handle return).
3. Publisher immediately calls `retract(handle)` (§8.21; catalogue
   row 9.13 BLOCKING when `MessageRetractionHandle` is promoted from
   gorti M17's uint64 alias to a real DLC handle class, catalogue
   row 7.2).
4. **Subscriber** is time-constrained, advances to t=15.
5. RTI fires `requestRetraction(handle)` on subscriber (§8.22;
   catalogue row 4.36 BLOCKING — M17 absent) BEFORE the original
   `receiveInteraction` would deliver.
6. The original interaction MUST NOT be delivered. The subscriber's
   defensive `receiveInteraction` override emits `SUB: RECEIVE_SPURIOUS`
   if invoked; its appearance fails the golden diff.

## Why this fixture

This is the single hardest behavioral test in §6/§8 surfaces:
- Locks `sendInteraction` TSO+retract overload signature.
- Locks `retract` taking a real `MessageRetractionHandle` class.
- Locks `requestRetraction` callback (callback row 4.36).
- Locks the ordering invariant: retraction wins, original is suppressed.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: TIME_REGULATION_ENABLED` — §8.3 timeRegulationEnabled (catalogue row 4.33)
- `PUB: PUBLISH` — §5.7 publishInteractionClass
- `PUB: SEND_TSO` — §6.12 sendInteraction TSO+retract overload (catalogue row 11.4)
- `PUB: RETRACT` — §8.21 retract (catalogue row 9.13)
- `PUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant (catalogue row 4.35)
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: TIME_CONSTRAINED_ENABLED` — §8.6 timeConstrainedEnabled (catalogue row 4.34)
- `SUB: SUBSCRIBE` — §5.8 subscribeInteractionClass
- `SUB: REQUEST_RETRACTION` — §8.22 requestRetraction callback (catalogue row 4.36)
- `SUB: TIME_ADVANCE_GRANT` — §8.13 timeAdvanceGrant
- `SUB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E's TASK-363 clears.
