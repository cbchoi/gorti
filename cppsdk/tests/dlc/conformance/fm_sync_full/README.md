# fm_sync_full

3-federate full-federation synchronization point. The registrar federate
calls `registerFederationSynchronizationPoint(label, tag)` (§4.11, 1-arg
overload — no `FederateHandleSet` means the whole federation). All 3
federates receive `announceSynchronizationPoint` (§4.13), all call
`synchronizationPointAchieved(label, successfully=true)` (§4.14). RTI
fires `federationSynchronized(label, failedToSyncSet)` (§4.15) with
`failedToSyncSet.size()==0`.

## Spec citations per event in goldens

The traceability lint enforces these citations:

### Registrar

- `REG: CONNECT` — §4.2 connect
- `REG: JOIN` — §4.9 joinFederationExecution
- `REG: REGISTER_SYNC_POINT` — §4.11 registerFederationSynchronizationPoint
- `REG: SYNC_REGISTRATION_SUCCEEDED` — §4.12 synchronizationPointRegistrationSucceeded
- `REG: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `REG: ACHIEVED` — §4.14 synchronizationPointAchieved (`successfully` argument)
- `REG: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized (`failedToSyncSet` argument)
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

## Status

Goldens are spec-derived, so a clean diff is **SPEC-FULL**. The locally
configured parity runtime is limited to two federates and cannot run
this three-federate scenario.

Wait loops drain via `evokeMultipleCallbacks` because callbacks are
delivered through the caller-thread evoke queue.

`_harness/run_fixture.sh fm_sync_full` (protocol in `driver.conf`:
registrar first, peers gated on JOIN markers inside the registrar's
700 ms window, `FED_NAME=BOB/CAROL` env) is deterministic.

**SPEC-FULL 20/20** (registrar 8/8, bob 6/6, carol 6/6).
`§4.12 synchronizationPointRegistrationSucceeded` is present on the wire
and bridged to the DLC ambassador. No residual.
