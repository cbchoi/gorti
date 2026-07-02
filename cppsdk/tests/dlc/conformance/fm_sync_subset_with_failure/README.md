# fm_sync_subset_with_failure

3-federate sync point with a 2-federate subset and a failed achievement:

1. Alice registers sync point `checkpoint` via the **2-arg overload** of
   `registerFederationSynchronizationPoint(label, tag, FederateHandleSet)`
   (§4.11 per catalogue row 3.10) with subset `{bob, carol}` (alice is
   NOT in the set).
2. Bob calls `synchronizationPointAchieved(label, successfully=true)`.
3. Carol calls `synchronizationPointAchieved(label, successfully=false)`
   — the `successfully` arg is the catalogue row 3.11 BLOCKING fix.
4. `federationSynchronized(label, failedToSyncSet)` fires on bob and
   carol; `failedToSyncSet` is non-empty (contains carol's handle) per
   catalogue row 4.7.

M31 dispatch plan §2.2 fixture #4.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Registrar (alice — outside the sync subset)

- `REG: CONNECT` — §4.2 connect
- `REG: JOIN` — §4.9 joinFederationExecution
- `REG: REGISTER_SYNC_POINT_SUBSET` — §4.11 registerFederationSynchronizationPoint (2-arg overload per catalogue row 3.10)
- `REG: SYNC_REGISTRATION_SUCCEEDED` — §4.12 synchronizationPointRegistrationSucceeded
- `REG: RESIGN` — §4.10 resignFederationExecution

### Bob (participating, succeeds)

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `BOB: ACHIEVED` — §4.14 synchronizationPointAchieved successfully=true
- `BOB: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized (catalogue row 4.7: failedToSyncSet non-empty)
- `BOB: RESIGN` — §4.10 resignFederationExecution

### Carol (participating, fails)

- `CAROL: CONNECT` — §4.2 connect
- `CAROL: JOIN` — §4.9 joinFederationExecution
- `CAROL: ANNOUNCE_SYNC` — §4.13 announceSynchronizationPoint
- `CAROL: ACHIEVED` — §4.14 synchronizationPointAchieved (catalogue row 3.11: successfully=false)
- `CAROL: FEDERATION_SYNCHRONIZED` — §4.15 federationSynchronized
- `CAROL: RESIGN` — §4.10 resignFederationExecution

## Status (M35 parity pass)

Goldens are spec-derived: Pitch Free's 2-federate EULA cap blocks a
3-federate capture, so a clean diff would be **SPEC-FULL**, not
Pitch-FULL.

**SPEC-PARTIAL 6/17** (registrar 2/5, bob 2/6, carol 2/6). All legs
match through CONNECT + JOIN; everything after is blocked by four
wire/bridge gaps, none fixture-fixable:

1. **`getFederateHandle` (§10.4)** — DLC stub throws unconditionally
   ("requires federation connection (M35+)",
   cppsdk/src/dlc/RTIambassadorImpl.cpp). The registrar cannot build
   the {bob, carol} FederateHandleSet, so
   `registerFederationSynchronizationPoint` (2-arg §4.11 — which IS
   wired end-to-end, required_federates travels the M17 wire) is never
   reached, and bob/carol's `synchronizationPointAchieved` then fails
   with "synchronization point not registered" (cascade).
2. **§4.12 `synchronizationPointRegistrationSucceeded`** — no ack
   event on the M17 wire, no FederateAmbassadorBridge dispatch path
   (same gap as fm_sync_full).
3. **§4.14 `successfully=false`** — DLC shim drops the flag (M17 has
   no failed-achieve wire message; documented divergence at the shim),
   so carol's failed achieve would be recorded as success.
4. **§4.15 `failedToSyncSet`** — the bridge forwards an empty set (M17
   `federationSynchronized` carries only the label); golden expects
   `failedToSyncSet.size=1`.

Wait loops drain via `evokeMultipleCallbacks` (gorti M17 buffers
callbacks for caller-thread drain; harmless yield under Pitch).

## M37 ED re-verdict (2026-07-02) — scripted driver

`_harness/run_fixture.sh fm_sync_subset_with_failure` (protocol in
`driver.conf`: alice/registrar first, bob and carol gated on JOIN
markers inside alice's 700 ms window). Deterministic result:
**SPEC-PARTIAL 14/17** (registrar 4/5, bob 5/6, carol 5/6) — the M35
6/17 baseline no longer reproduces because M36 closed the
`getFederateHandle` stub + announce cascade + `successfully=false`
gaps. Exactly two residual gap kinds, both M37 EA proto verticals:

- `REG: SYNC_REGISTRATION_SUCCEEDED` missing (§4.12 — no wire ack
  event; same gap as fm_sync_full).
- `FEDERATION_SYNCHRONIZED ... failedToSyncSet.size=0` on bob+carol
  where golden wants `size=1` (§4.15 — failedToSyncSet forwarded
  empty).

## M37 EE final verdict (2026-07-02) — integrated main

`_harness/run_fixture.sh fm_sync_subset_with_failure` vs integrated
main (post M37-EA/EB/EC merges): **SPEC-FULL 17/17** (registrar 5/5,
bob 6/6, carol 6/6). Both ED residuals closed by the M37 proto
vertical: §4.12 registration ack now on the wire + bridged, and §4.15
`failedToSyncSet` now carries carol (`size=1` on bob and carol, and
carol's §4.14 `successfully=false` achieve is recorded as failed).
No residual.
