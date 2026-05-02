# TASK-065: SDK — register object + update_attributes

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-064 |
| Blocks | TASK-068 |

## Goal

`fed.register_object_instance(class_name)` and `fed.update_attributes(handle, {name: value})`.

## Scope (in)

- Create `pysdk/rti1516e/object.py`.
- Create `pysdk/tests/test_object.py`.

## Scope (out)

- Subscribe / receive — TASK-067.

## Implements

- Requirements: IR-PYAPI-1.

## TDD entry point

- Start with: register, update — assert recorded request matches attributes/timestamp.

## Acceptance criteria

- [ ] Test green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
