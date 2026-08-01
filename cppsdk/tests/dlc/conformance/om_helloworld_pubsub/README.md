# om_helloworld_pubsub

Vehicle + Honk pub/sub on the DLC-strict C++ surface. Two federates:
publisher registers `car-1`, updates
`Position=42` + `Velocity=7`, sends `Honk(Volume=5)`. Subscriber observes
DISCOVER → REFLECT → RECEIVE.

Exercises the IEEE 1516.1-2010 §6 Object Management surface end-to-end
plus the §5 Declaration Management subscribe path.

## Federate code style

DLC-strict through the standard `RTI/` headers:
- `<RTI/*.h>` spec header paths.
- `RTIambassadorFactory::createRTIambassador()` returns `auto_ptr` (C++17
  resolution: aliased to `std::unique_ptr`).
- `NullFederateAmbassador` subclass for callbacks.
- `std::wstring` for every string the RTI accepts.
- `resignFederationExecution(CANCEL_THEN_DELETE_THEN_DIVEST)` — mandatory
  `ResignAction` per §4.10.

## Spec citations per event in goldens

The `scripts/check-spec-traceability.sh` lint enforces these citations:

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

## Status

`_harness/run_fixture.sh om_helloworld_pubsub` (driver smoke fixture;
`driver.conf`: subscriber first, publisher gated on `SUB: SUBSCRIBE`).
**FULL — subscriber 7/7, publisher 9/9**; captured logs are byte-identical
with the committed capture.
