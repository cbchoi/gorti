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

## M31 status

RED. `TBD-pitch-capture` until Agent E TASK-363 clears.
