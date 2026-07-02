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

## gorti parity status (M35, parity-CC)

Publisher PARTIAL 4/6, subscriber PARTIAL 5/7. Captured run:
`gorti-captured.{publisher,subscriber}.log`.

Missing events:

- `SUB: DISCOVER name=car-inst handle=<H>` — gorti M17 has no late-join
  discovery: subscribing after the instance was registered does not
  retroactively fire §6.9 discoverObjectInstance (om_helloworld_pubsub
  passes because there the subscribe precedes the register).
- `PUB: PROVIDE_UPDATE_INSTANCE handle=<H> attrs=2` — the DLC
  §6.19 requestAttributeValueUpdate instance-handle overload (RTIambassadorImpl.cpp:845)
  is a silent no-op, so the request never reaches the server, even
  though the server CAN emit provide_update (request_update.go); and
  §6.20 provideAttributeValueUpdate is not declared on the M17 Cut-1
  FederateAmbassador, so the bridge has nothing to convert (catalogue
  row 4.24).
- `PUB: UPDATE name=car-inst Position=33.000000 Velocity=44.000000`
  and `SUB: REFLECT name=car-inst Position=33 Velocity=44` — causal
  downstream of the missing provide (fixture only responds when asked).

Fixture side is ready: evoke-drain loops everywhere, and the response
update is guarded on provide arrival, so the capture will pick up the
full chain as soon as the DLC request wiring + bridge converter land.
