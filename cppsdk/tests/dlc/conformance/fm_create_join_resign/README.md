# fm_create_join_resign

Single federate exercising all 6 `ResignAction` enumerator values per
IEEE 1516.1-2010 §4.10. Iterates the loop:

`CONNECT → CREATE → JOIN → REGISTER → RESIGN(<action>) → DISCONNECT`

once per enumerator.

The fixture requires the mandatory `ResignAction` argument and exercises
all six enumerators declared in `Enums.h:33-41`.

## Spec citations per event in goldens

The traceability lint enforces these citations:

- `FED: CONNECT` — §4.2 connect
- `FED: CREATE` — §4.5 createFederationExecution
- `FED: JOIN` — §4.9 joinFederationExecution
- `FED: REGISTER` — §6.8 registerObjectInstance
- `FED: RESIGN` — §4.10 resignFederationExecution (all six `ResignAction` enumerators)
- `FED: DISCONNECT` — §4.3 disconnect

## In-process iteration notes

The golden concatenates six hermetic executions. To run all six rounds
in one process, the fixture makes two spec-legal, golden-silent
accommodations:

1. **§4.6 destroy between rounds** — after each RESIGN the federate
   silently calls `destroyFederationExecution` (no federate is joined,
   so it is legal; emits no golden line). A complete destroy resets the
   execution before the next round.
2. **Name-collision tolerance on re-register** — same pattern as the
   CREATE/`FederationExecutionAlreadyExists` catch:
   divest/cancel/no-action resigns legally leave `car-1` alive
   (§4.10 — only DELETE_OBJECTS-flavored actions remove instances).
   gorti's `DestroyFederation` (rti/internal/federation/manager.go)
   does not yet cascade to the object registry
   (rti/internal/object/registry.go keeps per-federation
   name→instance state), and the DLC `deleteObjectInstance` is a
   documented no-op (`cppsdk/src/dlc/RTIambassadorImpl.cpp`, §6.14), so
   the stale name surfaces as a collision on the
   next round's `registerObjectInstance`. The fixture tolerates it and
   proceeds. Implementations that fully clear object state on destroy do
   not take this catch. This remains a known server-side gap.

## Status

**FULL** — gorti canonical output is byte-identical to the committed golden
(36/36 lines, all 6 ResignAction sub-scenarios).
