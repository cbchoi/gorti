# fm_sync_full

3-federate full-federation synchronization point. The registrar federate
calls `registerFederationSynchronizationPoint(label, tag)` (§4.11, 1-arg
overload — no `FederateHandleSet` means the whole federation). All 3
federates receive `announceSynchronizationPoint` (§4.13), all call
`synchronizationPointAchieved(label, successfully=true)` (§4.14 per
catalogue row 3.11). RTI fires `federationSynchronized(label, failedToSyncSet)`
(§4.15 per catalogue row 4.7) with `failedToSyncSet.size()==0`.

M31 dispatch plan §2.2 fixture #3.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Registrar

- `REG: CONNECT` — §4.2 connect
- `REG: JOIN` — §4.9 joinFederationExecution
- `REG: REGISTER_SYNC_POINT` — §4.11 registerFederationSynchronizationPoint
- `REG: SYNC_REGISTRATION_SUCCEEDED` — §4.12 synchronizationPointRegistrationSucceeded
- `REG: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `REG: ACHIEVED` — §4.14 synchronizationPointAchieved (catalogue row 3.11: successfully arg)
- `REG: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized (catalogue row 4.7: failedToSyncSet arg)
- `REG: RESIGN` — §4.10 resignFederationExecution

### Bob

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `BOB: ACHIEVED` — §4.14 synchronizationPointAchieved
- `BOB: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized
- `BOB: RESIGN` — §4.10 resignFederationExecution

### Carol

- `CAROL: CONNECT` — §4.2 connect
- `CAROL: JOIN` — §4.9 joinFederationExecution
- `CAROL: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `CAROL: ACHIEVED` — §4.14 synchronizationPointAchieved
- `CAROL: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized
- `CAROL: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E TASK-363 clears.
