# fm_save_restore_roundtrip

Federation save mid-execution → resign → re-join → request restore →
state preserved. Exercises §4.16-§4.32 of IEEE 1516.1-2010, the
callback-driven save/restore flow. M31 dispatch plan §2.2 fixture #5.

Why callback-driven matters: divergence catalogue row 3.12 marks this
BLOCKING — gorti M17 uses polling `querySaveState`, the DLC spec
requires `federateSaveBegun` and the rest of the callback chain. This
fixture's golden enforces that.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

- `FED: CONNECT` — §4.2 connect
- `FED: JOIN` — §4.9 joinFederationExecution
- `FED: REGISTER` — §6.8 registerObjectInstance
- `FED: UPDATE` — §6.10 updateAttributeValues
- `FED: REQUEST_FEDERATION_SAVE` — §4.16 requestFederationSave
- `FED: INITIATE_FEDERATE_SAVE` — §4.17 initiateFederateSave callback
- `FED: FEDERATE_SAVE_BEGUN` — §4.18 federateSaveBegun (catalogue row 3.12)
- `FED: FEDERATE_SAVE_COMPLETE` — §4.19 federateSaveComplete
- `FED: FEDERATION_SAVED` — §4.20 federationSaved (catalogue row 4.9: no label)
- `FED: RESIGN` — §4.10 resignFederationExecution
- `FED: REJOIN` — §4.9 joinFederationExecution
- `FED: REQUEST_FEDERATION_RESTORE` — §4.24 requestFederationRestore
- `FED: RESTORE_REQUEST_SUCCEEDED` — §4.25 requestFederationRestoreSucceeded
- `FED: RESTORE_BEGUN` — §4.26 federationRestoreBegun
- `FED: INITIATE_FEDERATE_RESTORE` — §4.27 initiateFederateRestore (catalogue row 4.13: federateName added)
- `FED: FEDERATE_RESTORE_COMPLETE` — §4.28 federateRestoreComplete
- `FED: FEDERATION_RESTORED` — §4.29 federationRestored (catalogue row 4.14: no label)

## Status (M36, agent-DB)

**PARTIAL 15/18** (was 12/18 in the M35 parity pass). The golden is
spec-derived (Pitch capture pending), so a clean diff would be
SPEC-FULL, not Pitch-FULL.

M35 gap 1 is FIXED (M36 DB-3): `rti/internal/savepoint` now captures
federate NAMES in the save manifest (`federate_names`, index-parallel
to `federates`) and `RequestFederationRestore` routes
`initiateFederateRestore` by matching saved names against the CURRENT
roster (§4.27), with the §4.13 payload still carrying the handle the
federate had at save time. The resign→rejoin federate (same name, new
handle) now receives the initiate and its `federateRestoreComplete`
is accepted. Newly matching: FEDERATE_RESTORE_COMPLETE,
FEDERATION_RESTORED, final RESIGN — the restore round-trip completes.

Remaining misses (3), all outside DB scope (proto / DLC bridge):

1. RESTORE_REQUEST_SUCCEEDED + RESTORE_BEGUN: the proto FederateEvent
   oneof (`proto/rti/v1/stream.proto`, restore tags 43/44/45 =
   initiate/completed/failed) has NO slots for §4.25
   `requestFederationRestoreSucceeded` or §4.26
   `federationRestoreBegun`; adding them is a proto change (out of
   scope for M36 DB) plus bridge dispatch paths in
   `cppsdk/src/dlc/FederateAmbassadorBridge.cpp`.
2. INITIATE_FEDERATE_RESTORE now ARRIVES but reads `federate=` (empty)
   vs golden `federate=saver`: proto `InitiateFederateRestore` carries
   only label + federate_handle (no federate_name field), and
   `DLCFederateAmbassadorBridge::initiateFederateRestore` forwards an
   empty name. Either a proto field or a bridge-side fill from the
   federate's own join name (DA's call) turns this line into a match
   (→ 16/18).

## M37 EE final verdict (2026-07-02) — integrated main

**SPEC-FULL 18/18** (`_harness/run_fixture.sh fm_save_restore_roundtrip`).
All three M36-DB residuals are closed by the M37 proto vertical:
§4.25 requestFederationRestoreSucceeded + §4.26 federationRestoreBegun
now have wire slots (RESTORE_REQUEST_SUCCEEDED + RESTORE_BEGUN
captured), and InitiateFederateRestore carries federate_name
(`federate=saver`). Determinism note: rtid persists savepoint bundles
(gorti-saves/) in its cwd; a stale bundle from a previous run makes the
next requestFederationSave abort. The harness now starts rtid with its
cwd in the per-run temp dir, making back-to-back runs deterministic
(verified 3x FULL). No residual.
