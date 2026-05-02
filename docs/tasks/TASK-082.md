# TASK-082: Federate-side mode verification (best-effort attribute via Python)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-077, TASK-081 |
| Blocks | none |

## Goal

Declare a best-effort attribute in a FOM; confirm Python SDK transmits it with RO semantics through a live federation in best-effort mode.

## Scope (in)

- Create `pysdk/tests/test_modes.py`.

## Scope (out)

- Verbose-mode tests (covered implicitly by other tests).

## Implements

- Requirements: FR-OM-3 (Python side); NFR-PERF-1..4.

## TDD entry point

- Start with: configure federation `mode=best-effort`; declare best-effort attribute; publish updates; capture event log; assert no TSO ordering applied.

## Acceptance criteria

- [ ] Test green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
