# om_local_delete

`localDeleteObjectInstance` removes a subscriber's local view of an
object WITHOUT notifying other federates.

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

## Purpose

The fixture validates both the `localDeleteObjectInstance` service and
its defining semantic: the deletion remains invisible to remote
federates.

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
- `SUB: LOCAL_DELETE` — §6.16 localDeleteObjectInstance
- `SUB: RESIGN` — §4.10 resignFederationExecution

## Status

**RED. `WILL_FAIL TRUE`.** The committed goldens do not yet contain
event-level expected traces, so the fixture cannot produce a meaningful
conformance diff.
