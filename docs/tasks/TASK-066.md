# TASK-066: SDK — subscribe interaction + send_interaction

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

`fed.subscribe_interaction_class(name)` and `fed.send_interaction(class_name, params)`.

## Scope (in)

- Create `pysdk/rti1516e/interaction.py`.
- Create `pysdk/tests/test_interaction.py`.

## Scope (out)

- Async event stream — TASK-067.

## Implements

- Requirements: IR-PYAPI-1.

## TDD entry point

- Start with: send interaction — assert `FakeRtiServer` recorded `SendInteractionRequest`.

## Acceptance criteria

- [ ] Test green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
