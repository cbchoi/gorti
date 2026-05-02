# TASK-067: SDK — async events() stream + typed exceptions

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-064, TASK-065, TASK-066, TASK-059 |
| Blocks | TASK-068, TASK-069 |

## Goal

`async for event in fed.events()` yields typed events (`ReflectAttributeValues`, `ReceiveInteraction`, `TimeAdvanceGrant`, etc. as dataclasses). Each error code from `proto/rti/v1/errors.proto` maps to a typed Python exception.

## Scope (in)

- Create `pysdk/rti1516e/events.py`.
- Create `pysdk/tests/test_events.py`.

## Scope (out)

- Layer 2 ambassador — TASK-068.

## Implements

- Requirements: IR-PYAPI-1.

## TDD entry point

- Start with: fake pushes a `ReflectAttributeValues` event; consumer iterates `events()`, asserts a typed dataclass with decoded values.

## Acceptance criteria

- [ ] All error codes have matching typed exceptions (e.g. `FederationNotFound`, `FederateAlreadyJoined`).
- [ ] Async iterator yields typed dataclasses.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
