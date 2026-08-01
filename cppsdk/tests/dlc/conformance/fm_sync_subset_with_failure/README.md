# fm_sync_subset_with_failure

3-federate sync point with a 2-federate subset and a failed achievement:

1. Alice registers sync point `checkpoint` via the **2-arg overload** of
   `registerFederationSynchronizationPoint(label, tag, FederateHandleSet)`
   (§4.11) with subset `{bob, carol}` (alice is NOT in the set).
2. Bob calls `synchronizationPointAchieved(label, successfully=true)`.
3. Carol calls `synchronizationPointAchieved(label, successfully=false)`
   (§4.14).
4. `federationSynchronized(label, failedToSyncSet)` fires on bob and
   carol; `failedToSyncSet` is non-empty and contains carol's handle
   (§4.15).

## Spec citations per event in goldens

The traceability lint enforces these citations:

### Registrar (alice — outside the sync subset)

- `REG: CONNECT` — §4.2 connect
- `REG: JOIN` — §4.9 joinFederationExecution
- `REG: REGISTER_SYNC_POINT_SUBSET` — §4.11 registerFederationSynchronizationPoint (subset overload)
- `REG: SYNC_REGISTRATION_SUCCEEDED` — §4.12 synchronizationPointRegistrationSucceeded
- `REG: RESIGN` — §4.10 resignFederationExecution

### Bob (participating, succeeds)

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `BOB: ACHIEVED` — §4.14 synchronizationPointAchieved successfully=true
- `BOB: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized (`failedToSyncSet` non-empty)
- `BOB: RESIGN` — §4.10 resignFederationExecution

### Carol (participating, fails)

- `CAROL: CONNECT` — §4.2 connect
- `CAROL: JOIN` — §4.9 joinFederationExecution
- `CAROL: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `CAROL: ACHIEVED` — §4.14 synchronizationPointAchieved (`successfully=false`)
- `CAROL: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized
- `CAROL: RESIGN` — §4.10 resignFederationExecution

## Status

Goldens are spec-derived, so a clean diff is **SPEC-FULL**. The locally
configured parity runtime is limited to two federates and cannot run
this three-federate scenario.

Wait loops drain via `evokeMultipleCallbacks` because callbacks are
delivered through the caller-thread evoke queue.

`_harness/run_fixture.sh fm_sync_subset_with_failure` (protocol in
`driver.conf`: alice/registrar first, bob and carol gated on JOIN
markers inside alice's 700 ms window) is deterministic.

**SPEC-FULL 17/17** (registrar 5/5, bob 6/6, carol 6/6). The §4.12
registration ack is present on the wire and bridged, and §4.15
`failedToSyncSet` now carries carol (`size=1` on bob and carol, and
carol's §4.14 `successfully=false` achieve is recorded as failed).
No residual.
