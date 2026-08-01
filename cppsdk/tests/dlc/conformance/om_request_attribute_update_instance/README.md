# om_request_attribute_update_instance

Same scenario as `om_request_attribute_update_class` but the subscriber
targets a SPECIFIC instance handle. Exercises the **instance-handle**
overload of §6.19 `requestAttributeValueUpdate`.

## Scenario

1. **Publisher** publishes `Vehicle.Position` + `Velocity`, registers
   `car-inst`.
2. **Subscriber** joins ~1s later, subscribes, observes DISCOVER.
3. Subscriber calls
   `requestAttributeValueUpdate(objectInstanceHandle, attrs, tag)`
   — the **instance-handle** overload of §6.19 (the other overload is
   in `om_request_attribute_update_class`).
4. RTI fires `provideAttributeValueUpdate` on publisher (§6.20).
5. Publisher updates; subscriber observes REFLECT.

## Purpose

Per §6.19, `requestAttributeValueUpdate` is a 2-overload set: one
takes a class handle (applies to all matching instances), one takes
an instance handle (targets ONE object). Both must exist; locking
each via its own fixture ensures the surface doesn't collapse to
the wrong one.

This fixture requires the instance-handle overload on the non-DDM
surface and complements the class-handle fixture.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: PROVIDE_UPDATE_INSTANCE` — §6.20 provideAttributeValueUpdate
- `PUB: UPDATE` — §6.10 updateAttributeValues
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REQUEST_UPDATE_INSTANCE` — §6.19 requestAttributeValueUpdate (instance-handle overload)
- `SUB: REFLECT` — §6.11 reflectAttributeValues
- `SUB: RESIGN` — §4.10 resignFederationExecution

## Status

**SPEC-FULL** — publisher 6/6, subscriber 7/7 (exact, zero extra) against the
committed canonical goldens.

The DLC §6.19 `requestAttributeValueUpdate` instance-handle overload delegates
through `M17Bridge::requestAttributeValueUpdate` to the
`RequestAttributeValueUpdate` RPC. The stream loop dispatches the
`provide_update` event, the bridge-facing FederateAmbassador exposes the
§6.20 `provideAttributeValueUpdate` slot, and the DLC bridge
converts it — so the full request → provide → update → reflect chain
runs.

Launch-order caveat: the current driver launches the SUBSCRIBER first, so the
§5.6 subscribe precedes the publisher's register and §6.9 DISCOVER fans
out normally. True late-join discovery (subscribe AFTER register firing
a retroactive discover) remains a Go server gap;
the §6.19 request and §6.20 callback are fully witnessed either way.
