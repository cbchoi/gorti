# TASK-064: SDK — publish/subscribe object class API

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-063 |
| Blocks | TASK-065, TASK-066, TASK-067 |

## Goal

`fed.publish_object_class(name, attributes=[...])` / `fed.subscribe_object_class(...)` and the interaction equivalents.

## Scope (in)

- Create `pysdk/rti1516e/declaration.py`.
- Create `pysdk/tests/test_declaration.py`.

## Scope (out)

- Object register/update — TASK-065.
- Send-interaction — TASK-066.

## Implements

- Requirements: IR-PYAPI-1.

## TDD entry point

- Start with: publish, then check `FakeRtiServer` recorded a `PublishObjectClassAttributesRequest`.

## Acceptance criteria

- [ ] Test green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
