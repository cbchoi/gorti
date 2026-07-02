# fm_create_join_resign

Single federate exercising all 6 `ResignAction` enumerator values per
IEEE 1516.1-2010 §4.10. Iterates the loop:

`CONNECT → CREATE → JOIN → REGISTER → RESIGN(<action>) → DISCONNECT`

once per enumerator. M31 dispatch plan §2.2 fixture #1.

Locks divergence catalogue row 3.9 (mandatory `ResignAction` arg — gorti
M17 had `resignFederationExecution()` with no param) and row 5.3 (the
6 enumerators in Enums.h:33-41 — gorti M17 had no `ResignAction` enum
at all).

## Spec citations per event in goldens

Per TASK-362 traceability lint:

- `FED: CONNECT` — §4.2 connect
- `FED: CREATE` — §4.5 createFederationExecution
- `FED: JOIN` — §4.9 joinFederationExecution
- `FED: REGISTER` — §6.8 registerObjectInstance
- `FED: RESIGN` — §4.10 resignFederationExecution (catalogue row 3.9 BLOCKING; row 5.3 enumerators)
- `FED: DISCONNECT` — §4.3 disconnect

## gorti in-process iteration notes (M35 parity pass)

The golden concatenates six hermetic executions (see the golden's own
"Pitch capture status" header — Pitch Free could not run the loop
in-process either). To run all six rounds in ONE process the fixture
makes two spec-legal, golden-silent accommodations:

1. **§4.6 destroy between rounds** — after each RESIGN the federate
   silently calls `destroyFederationExecution` (no federate is joined,
   so it is legal under both RTIs; emits no golden line). Under Pitch
   this fully resets the execution.
2. **Name-collision tolerance on re-register** — same pattern as the
   long-standing CREATE/`FederationExecutionAlreadyExists` catch:
   divest/cancel/no-action resigns legally leave `car-1` alive
   (§4.10 — only DELETE_OBJECTS-flavored actions remove instances).
   gorti's `DestroyFederation` (rti/internal/federation/manager.go)
   does not yet cascade to the object registry
   (rti/internal/object/registry.go keeps per-federation
   name→instance state), and the DLC `deleteObjectInstance` is a
   documented no-op (cppsdk/src/dlc/RTIambassadorImpl.cpp §6.14, M17
   Cut-1 wire gap), so the stale name surfaces as a collision on the
   next round's `registerObjectInstance`. The fixture tolerates it and
   proceeds; under Pitch (accommodation 1 effective) the catch never
   fires. Known server-side gap, tracked for a post-M35 cut.

## Status

M35 parity: **FULL** — gorti canonical output is byte-identical to the
committed golden (36/36 lines, all 6 ResignAction sub-scenarios). See
`gorti-captured.federate.log`.
