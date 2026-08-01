# fm_list_executions

Single federate connects (no join), creates three federation executions
(`alpha`, `beta`, `gamma`), calls `listFederationExecutions()` (§4.7),
waits for the asynchronous `reportFederationExecutions` callback (§4.8),
prints the names, then destroys each federation and disconnects.

The fixture requires the asynchronous §4.7/§4.8 service-and-callback
pair: `listFederationExecutions` returns through
`reportFederationExecutions`.

## Spec citations per event in goldens

The traceability lint enforces these citations:

- `FED: CONNECT` — §4.2 connect
- `FED: CREATE` — §4.5 createFederationExecution
- `FED: LIST_FEDERATION_EXECUTIONS` — §4.7 listFederationExecutions
- `FED: REPORT_FEDERATION_EXECUTIONS` — §4.8 reportFederationExecutions callback
- `FED: DESTROY` — §4.6 destroyFederationExecution
- `FED: DISCONNECT` — §4.3 disconnect

## Status

**RED.** The committed golden does not yet contain an event-level
expected trace, so the fixture cannot produce a meaningful conformance
diff.
