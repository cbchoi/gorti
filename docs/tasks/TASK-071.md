# TASK-071: HLAFederate — pyjevsim select() preservation under simultaneous events

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-070 |
| Blocks | TASK-073 |

## Goal

When multiple events have identical timestamps, sort by pyjevsim's `select()` order BEFORE handing to HLA — the HLA tie-break sees them in pyjevsim-preferred order.

## Scope (in)

- Create `pysdk/pyjevsim_bridge/select_preserve.py`.
- Create `pysdk/tests/test_select_preserve.py`.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-PYJ-4.

## TDD entry point

- Start with: two simultaneous interactions at `t=3.0`, pyjevsim `select()` returns event B first → assert delivery order is B, then A.

## Acceptance criteria

- [ ] Simultaneity tests green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
