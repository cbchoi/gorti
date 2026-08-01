# om_request_attribute_update_class

Late-joiner requests an attribute update by **class** handle;
publisher's `provideAttributeValueUpdate` callback fires, and the
publisher sends a fresh update.

## Scenario

1. **Publisher** publishes `Vehicle.Position` + `Velocity` and
   registers `car-cls` (no initial update — late-joiner missed it).
2. **Subscriber** joins ~1s later, subscribes, observes DISCOVER.
3. Subscriber calls
   `requestAttributeValueUpdate(classHandle, {Position, Velocity}, tag)`
   (§6.19 — the class-handle overload).
4. RTI fires `provideAttributeValueUpdate(object, attrs, tag)` on
   publisher (§6.20).
5. Publisher calls `updateAttributeValues(object, {Pos=11, Vel=22})`.
6. Subscriber observes REFLECT.

## Purpose

DLC requires both class-handle and instance-handle non-DDM overloads of
`requestAttributeValueUpdate` per §6.19. This fixture covers the
class-handle overload and the §6.20 `provideAttributeValueUpdate`
callback.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: PROVIDE_UPDATE` — §6.20 provideAttributeValueUpdate
- `PUB: UPDATE` — §6.10 updateAttributeValues
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REQUEST_UPDATE` — §6.19 requestAttributeValueUpdate (class-handle overload)
- `SUB: REFLECT` — §6.11 reflectAttributeValues
- `SUB: RESIGN` — §4.10 resignFederationExecution

## Status

**SPEC-FULL** — publisher 6/6, subscriber 7/7 (exact, zero extra) against the
committed canonical goldens.

The DLC §6.19 `requestAttributeValueUpdate` class-handle overload delegates through
`M17Bridge::requestClassAttributeValueUpdate` to the
`RequestClassAttributeValueUpdate` RPC. The stream loop dispatches the
`provide_update` event, the bridge-facing FederateAmbassador exposes the
§6.20 `provideAttributeValueUpdate` slot, and
the DLC bridge converts it — so the full request → provide → update →
reflect chain runs.

Launch-order caveat: the current driver launches the SUBSCRIBER first, so the
§5.6 subscribe precedes the publisher's register and §6.9 DISCOVER fans
out normally. True late-join discovery (subscribe AFTER register firing
a retroactive discover) remains a Go server gap; the §6.19 request and
§6.20 callback are fully witnessed either way.
