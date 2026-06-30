# om_request_attribute_update_class

Late-joiner requests an attribute update by **class** handle;
publisher's `provideAttributeValueUpdate` callback fires, and the
publisher sends a fresh update. M31 dispatch plan §2.2 fixture #12.

## Scenario

1. **Publisher** publishes `Vehicle.Position` + `Velocity` and
   registers `car-cls` (no initial update — late-joiner missed it).
2. **Subscriber** joins ~1s later, subscribes, observes DISCOVER.
3. Subscriber calls
   `requestAttributeValueUpdate(classHandle, {Position, Velocity}, tag)`
   (§6.19 — the class-handle overload).
4. RTI fires `provideAttributeValueUpdate(object, attrs, tag)` on
   publisher (§6.20 / catalogue row 4.24).
5. Publisher calls `updateAttributeValues(object, {Pos=11, Vel=22})`.
6. Subscriber observes REFLECT.

## Why this fixture

Catalogue row 11.7 (MAJOR): gorti M17 has only the DDM variant of
`requestAttributeValueUpdate`. DLC requires both class-handle and
instance-handle (non-DDM) overloads per §6.19. Catalogue row 4.24
(MAJOR): `provideAttributeValueUpdate` callback absent from M17.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: PROVIDE_UPDATE` — §6.20 provideAttributeValueUpdate (catalogue row 4.24)
- `PUB: UPDATE` — §6.10 updateAttributeValues
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REQUEST_UPDATE` — §6.19 requestAttributeValueUpdate (class-handle overload, catalogue row 11.7)
- `SUB: REFLECT` — §6.11 reflectAttributeValues
- `SUB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E's TASK-363 clears.
