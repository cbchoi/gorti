# om_local_delete

`localDeleteObjectInstance` removes a subscriber's local view of an
object WITHOUT notifying other federates. M31 dispatch plan §2.2
fixture #11.

## Scenario

1. **Publisher** publishes `Vehicle.Position`, registers `car-local`,
   updates `Position=99`, resigns later.
2. **Subscriber** subscribes, observes DISCOVER + REFLECT.
3. Subscriber calls `localDeleteObjectInstance(object)` (§6.16).
4. Subscriber's local view of `car-local` is removed.
5. **No** `removeObjectInstance` callback fires on the subscriber
   (per §6.16: local-delete is a one-sided cleanup, not a federation-
   wide deletion).
6. **No** publisher-side callback fires either — the publisher never
   learns of the subscriber's local-delete.

The subscriber's defensive override of `removeObjectInstance` emits
`SUB: REMOVE_SPURIOUS` if invoked — its appearance in the captured
log would fail the golden diff.

## Why this fixture

Catalogue row 11.6 (MAJOR): `localDeleteObjectInstance` is entirely
absent from gorti M17. The spec semantic — that it's invisible to
remote federates — is a behavior contract that must be locked.

## Spec citations per event in goldens

### Publisher

- `PUB: CONNECT` — §4.2 connect
- `PUB: JOIN` — §4.9 joinFederationExecution
- `PUB: REGISTER` — §6.8 registerObjectInstance
- `PUB: UPDATE` — §6.10 updateAttributeValues
- `PUB: RESIGN` — §4.10 resignFederationExecution

### Subscriber

- `SUB: CONNECT` — §4.2 connect
- `SUB: JOIN` — §4.9 joinFederationExecution
- `SUB: SUBSCRIBE` — §5.6 subscribeObjectClassAttributes
- `SUB: DISCOVER` — §6.9 discoverObjectInstance
- `SUB: REFLECT` — §6.11 reflectAttributeValues
- `SUB: LOCAL_DELETE` — §6.16 localDeleteObjectInstance (catalogue row 11.6)
- `SUB: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. `WILL_FAIL TRUE`. Goldens are `TBD-pitch-capture` until Agent E's
TASK-363 (Pitch EULA review) clears.
