# TASK-063: SDK — RtiConnection.connect + join_federation async context manager

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-050, TASK-062 |
| Blocks | TASK-064, TASK-065, TASK-066, TASK-067, TASK-068 |

## Goal

Idiomatic Python entry point per `docs/agent-c-pysdk.md` §4.4 Layer 1. `async with RtiConnection.connect(...) as rti` opens connection; `async with rti.join_federation(...) as fed` joins.

## Scope (in)

- Create `pysdk/rti1516e/connection.py`.
- Create `pysdk/tests/test_connection.py` using a `FakeRtiServer` (small in-process double).

## Scope (out)

- Pub/sub APIs — TASK-064.

## Implements

- Requirements: IR-PYAPI-1.

## TDD entry point

- Start with: connect → join → resign → disconnect lifecycle test against `FakeRtiServer`.

## Acceptance criteria

- [ ] Lifecycle test green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
