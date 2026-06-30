# fm_list_executions

Single federate connects (no join), creates three federation executions
(`alpha`, `beta`, `gamma`), calls `listFederationExecutions()` (§4.7),
waits for the asynchronous `reportFederationExecutions` callback (§4.8),
prints the names, then destroys each federation and disconnects. M31
dispatch plan §2.2 fixture #2.

Locks divergence catalogue row 3.7 (`listFederationExecutions` absent
in gorti M17) and row 4.4 (`reportFederationExecutions` callback absent
in M17).

## Spec citations per event in goldens

Per TASK-362 traceability lint:

- `FED: CONNECT` — §4.2 connect
- `FED: CREATE` — §4.5 createFederationExecution
- `FED: LIST_FEDERATION_EXECUTIONS` — §4.7 listFederationExecutions (catalogue row 3.7)
- `FED: REPORT_FEDERATION_EXECUTIONS` — §4.8 reportFederationExecutions callback (catalogue row 4.4)
- `FED: DESTROY` — §4.6 destroyFederationExecution
- `FED: DISCONNECT` — §4.3 disconnect

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E TASK-363 clears.
