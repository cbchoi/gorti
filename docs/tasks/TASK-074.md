# TASK-074: M4 determinism harness — 10× same seed; sha256 byte-identical

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-073 |
| Blocks | TASK-075 |

## Goal

Run `examples/pyjevsim/` 10 consecutive times with the same seed; assert all 10 RTI-side event-log files have identical sha256.

## Scope (in)

- Create `examples/pyjevsim/determinism_test.py`.

## Scope (out)

- Lint/coverage gate — TASK-075.

## Implements

- Requirements: NFR-DET-1, NFR-DET-2.

## TDD entry point

- Start with: 2-run harness; extend to 10.

## Acceptance criteria

- [ ] 10 runs all byte-identical.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
