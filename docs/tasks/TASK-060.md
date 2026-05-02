# TASK-060: Python FOM dataclass mirror

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-019 |
| Blocks | TASK-061 |

## Goal

Python `dataclass` mirrors of Agent B's Go FOM model (`FOM`, `ObjectClass`, `Attribute`, `InteractionClass`, `Parameter`, `DataType` sum).

## Scope (in)

- Create `pysdk/rti1516e/fom/__init__.py` (exposing public types).
- Create `pysdk/rti1516e/fom/model.py`.
- Create `pysdk/tests/test_fom_model.py`.

## Scope (out)

- Parser — TASK-061.

## Implements

- Requirements: FR-FOM-1 (Python side).

## TDD entry point

- Start with: construct a small `FOM` dataclass, query class attributes, assert sorted iteration.

## Acceptance criteria

- [ ] Unit tests green.
- [ ] Same shape as Go model (so cross-side debugging is symmetric).
- [ ] mypy/ruff clean.
- [ ] `make verify` green.

## Notes / hints

- Per `docs/ORTHOGONALITY.md` §3, Agent C **must NOT import** Agent B's Go model — re-derive structure independently from XML schema documentation.
