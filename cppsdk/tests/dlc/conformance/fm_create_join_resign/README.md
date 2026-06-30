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

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E TASK-363 clears.
