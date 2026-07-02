# om_request_attribute_update_instance

Same scenario as `om_request_attribute_update_class` but the subscriber
targets a SPECIFIC instance handle. Exercises the **instance-handle**
overload of §6.19 `requestAttributeValueUpdate`. M31 dispatch plan
§2.2 fixture #13.

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

## Why this fixture (separate from #12)

Per §6.19, `requestAttributeValueUpdate` is a 2-overload set: one
takes a class handle (applies to all matching instances), one takes
an instance handle (targets ONE object). Both must exist; locking
each via its own fixture ensures the surface doesn't collapse to
the wrong one.

Catalogue row 11.7 (MAJOR): both overloads absent from gorti M17
non-DDM surface.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: PROVIDE_UPDATE_INSTANCE` — §6.20 provideAttributeValueUpdate (catalogue row 4.24)
- `PUB: UPDATE` — §6.10 updateAttributeValues
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REQUEST_UPDATE_INSTANCE` — §6.19 requestAttributeValueUpdate (instance-handle overload, catalogue row 11.7)
- `SUB: REFLECT` — §6.11 reflectAttributeValues
- `SUB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E's TASK-363 clears.

## gorti parity status (M36, agent-DA)

**SPEC-FULL** — publisher 6/6, subscriber 7/7 (exact, zero extra).
Captured run: `gorti-captured.{publisher,subscriber}.log`.

M36 DA-2/DA-5 closed the M35 parity-CC gaps: the DLC §6.19
requestAttributeValueUpdate instance-handle overload now delegates
through `M17Bridge::requestAttributeValueUpdate` to the M23
RequestAttributeValueUpdate RPC, the M17 stream loop dispatches the
`provide_update` event (was default-drop), the M17 FederateAmbassador
grew the §6.20 provideAttributeValueUpdate slot, and the DLC bridge
converts it — so the full request → provide → update → reflect chain
runs.

Launch-order caveat: this capture launches the SUBSCRIBER first, so the
§5.6 subscribe precedes the publisher's register and §6.9 DISCOVER fans
out normally. True late-join discovery (subscribe AFTER register firing
a retroactive discover) remains a Go server gap, Agent DC territory;
the §6.19/§6.20 rows this fixture owns (catalogue 11.7 + 4.24) are
fully witnessed either way.
