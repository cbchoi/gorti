# om_helloworld_pubsub

Vehicle + Honk pub/sub — the 2026-06-30 smoke generalized to the DLC-strict
C++ surface. Two federates: publisher registers `car-1`, updates
`Position=42` + `Velocity=7`, sends `Honk(Volume=5)`. Subscriber observes
DISCOVER → REFLECT → RECEIVE.

Exercises the IEEE 1516.1-2010 §6 Object Management surface end-to-end
plus the §5 Declaration Management subscribe path. M31 fixture #8 per
`docs/M31_DISPATCH_PLAN.md §2.2`.

## Federate code style

DLC-strict per `docs/DLC_COMPLIANCE_PROGRAM.md §5.5` migration recipe:
- `<RTI/*.h>` spec header paths (NOT M17's `<rti1516e/*.h>`).
- `RTIambassadorFactory::createRTIambassador()` returns `auto_ptr` (C++17
  resolution: aliased to `std::unique_ptr` per divergence catalogue row 2.2).
- `NullFederateAmbassador` subclass for callbacks.
- `std::wstring` for every string the RTI accepts.
- `resignFederationExecution(CANCEL_THEN_DELETE_THEN_DIVEST)` — mandatory
  `ResignAction` per §4.10 (catalogue row 3.9).

## Spec citations per event in goldens

Per TASK-362 traceability lint (`scripts/check-spec-traceability.sh`,
Agent E):

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: CREATE federation` — §4.5 createFederationExecution
- `PUB: JOIN federate` — §4.9 joinFederationExecution
- `PUB: PUBLISH class=Vehicle` — §5.3 publishObjectClassAttributes
- `PUB: PUBLISH interaction=Honk` — §5.7 publishInteractionClass
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: UPDATE` — §6.10 updateAttributeValues
- `PUB: SEND` — §6.12 sendInteraction
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN federate` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes + §5.8 subscribeInteractionClass
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REFLECT` — §6.11 reflectAttributeValues
- `SUB: RECEIVE` — §6.13 receiveInteraction
- `SUB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED by design. The federate TUs reference `rti1516e::*` impl symbols
that don't exist; CMake property `WILL_FAIL TRUE` per dispatch plan §3
criterion 2. Goldens are hand-authored from spec text and marked
`TBD-pitch-capture` until Agent E's TASK-363 (Pitch EULA review) clears.
