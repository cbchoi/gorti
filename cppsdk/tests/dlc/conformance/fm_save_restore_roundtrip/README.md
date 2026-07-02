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

## Status (M35 parity pass)

**PARTIAL 12/18.** The golden is spec-derived (Pitch capture pending),
so a clean diff would be SPEC-FULL, not Pitch-FULL.

Matching (12): CONNECT → JOIN → REGISTER → UPDATE →
REQUEST_FEDERATION_SAVE → INITIATE_FEDERATE_SAVE →
FEDERATE_SAVE_BEGUN → FEDERATE_SAVE_COMPLETE → FEDERATION_SAVED →
RESIGN → REJOIN → REQUEST_FEDERATION_RESTORE. The full §4.16-§4.20
save-side callback chain works after converting the fixture's wait
loops to `evokeMultipleCallbacks` drains (gorti M17 buffers callbacks
for caller-thread drain).

Missing (6): RESTORE_REQUEST_SUCCEEDED, RESTORE_BEGUN,
INITIATE_FEDERATE_RESTORE, FEDERATE_RESTORE_COMPLETE,
FEDERATION_RESTORED, final RESIGN. Three impl gaps outside this
fixture (server + DLC bridge, not fixture-fixable):

1. `rti/internal/savepoint/manager.go RequestFederationRestore`
   routes `initiateFederateRestore` to the FederateHandles recorded in
   the save manifest. gorti never reuses handles, so the resign→rejoin
   federate (new handle) never receives §4.27, and its
   `federateRestoreComplete` is rejected with
   ErrFederateNotInRestore. Spec §4.27 matches by federate NAME
   (hence the callback's `federateName` + `postRestoreFederateHandle`
   args).
2. The server emits no §4.25 `requestFederationRestoreSucceeded` and
   no §4.26 `federationRestoreBegun` events, and
   `cppsdk/src/dlc/FederateAmbassadorBridge.cpp` has no dispatch paths
   for either callback.
3. `DLCFederateAmbassadorBridge::initiateFederateRestore` forwards an
   empty `federateName` (M17 wire does not carry it); the golden
   expects `federate=saver`.
