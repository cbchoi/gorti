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

## Status (M35 parity pass)

Goldens are spec-derived: Pitch Free's 2-federate EULA cap blocks a
3-federate capture, so a clean diff is **SPEC-FULL** (diff vs the
spec-derived golden), not Pitch-FULL.

- **bob: SPEC-FULL 6/6** — byte-identical after canonicalization.
- **carol: SPEC-FULL 6/6** — byte-identical after canonicalization.
- **registrar: SPEC-PARTIAL 7/8** — sole miss is
  `REG: SYNC_REGISTRATION_SUCCEEDED` (§4.12
  synchronizationPointRegistrationSucceeded). The M17 wire has no
  sync-registration ack event (cppsdk/include/rti1516e/
  FederateAmbassador.h surfaces only announceSynchronizationPoint +
  federationSynchronized) and
  cppsdk/src/dlc/FederateAmbassadorBridge.cpp has no dispatch path for
  it — server + bridge work, not fixture-fixable.

Wait loops drain via `evokeMultipleCallbacks` (gorti M17 buffers
callbacks for caller-thread drain; harmless yield under Pitch).

## M37 ED re-verdict (2026-07-02) — scripted driver

`_harness/run_fixture.sh fm_sync_full` (protocol in `driver.conf`:
registrar first, peers gated on JOIN markers inside the registrar's
700 ms window, `FED_NAME=BOB/CAROL` env). Reproduces the M35 baseline
deterministically: **bob SPEC-FULL 6/6, carol SPEC-FULL 6/6, registrar
SPEC-PARTIAL 7/8 (19/20)** — sole miss remains
`REG: SYNC_REGISTRATION_SUCCEEDED` (§4.12 no wire ack; M37 EA proto
vertical). Captured logs byte-identical with the committed M35 capture.
