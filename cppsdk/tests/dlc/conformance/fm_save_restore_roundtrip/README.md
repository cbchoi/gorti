# fm_save_restore_roundtrip

Federation save mid-execution → resign → re-join → request restore →
state preserved. Exercises §4.16-§4.32 of IEEE 1516.1-2010, the
callback-driven save/restore flow.

The DLC contract is callback-driven: `federateSaveBegun` and the rest of
the save/restore callback chain carry progress. This fixture's golden
enforces that flow rather than a polling result.

## Spec citations per event in goldens

The traceability lint enforces these citations:

- `FED: CONNECT` — §4.2 connect
- `FED: JOIN` — §4.9 joinFederationExecution
- `FED: REGISTER` — §6.8 registerObjectInstance
- `FED: UPDATE` — §6.10 updateAttributeValues
- `FED: REQUEST_FEDERATION_SAVE` — §4.16 requestFederationSave
- `FED: INITIATE_FEDERATE_SAVE` — §4.17 initiateFederateSave callback
- `FED: FEDERATE_SAVE_BEGUN` — §4.18 federateSaveBegun
- `FED: FEDERATE_SAVE_COMPLETE` — §4.19 federateSaveComplete
- `FED: FEDERATION_SAVED` — §4.20 federationSaved (no label)
- `FED: RESIGN` — §4.10 resignFederationExecution
- `FED: REJOIN` — §4.9 joinFederationExecution
- `FED: REQUEST_FEDERATION_RESTORE` — §4.24 requestFederationRestore
- `FED: RESTORE_REQUEST_SUCCEEDED` — §4.25 requestFederationRestoreSucceeded
- `FED: RESTORE_BEGUN` — §4.26 federationRestoreBegun
- `FED: INITIATE_FEDERATE_RESTORE` — §4.27 initiateFederateRestore (`federateName` argument)
- `FED: FEDERATE_RESTORE_COMPLETE` — §4.28 federateRestoreComplete
- `FED: FEDERATION_RESTORED` — §4.29 federationRestored (no label)

## Status

**SPEC-FULL 18/18** (`_harness/run_fixture.sh fm_save_restore_roundtrip`).
The golden is spec-derived, so this status is SPEC-FULL. The §4.25
`requestFederationRestoreSucceeded` and §4.26 `federationRestoreBegun`
callbacks have wire slots, and `InitiateFederateRestore` carries
`federate_name` (`federate=saver`).

Determinism limitation: rtid persists savepoint bundles
(gorti-saves/) in its cwd; a stale bundle from a previous run makes the
next requestFederationSave abort. The harness now starts rtid with its
cwd in the per-run temp dir, making back-to-back runs deterministic
(verified 3x FULL). No residual.
