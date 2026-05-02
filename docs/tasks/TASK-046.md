# TASK-046: examples/go-timed — 3 federates with lookaheads {1.0, 2.0, 0.5}, NER over 100 ticks

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-045 |
| Blocks | TASK-047, TASK-048, TASK-049 |

## Goal

Reference example: three regulating federates with lookaheads {1.0, 2.0, 0.5} advance via NER over 100 logical ticks.

## Scope (in)

- Create `examples/go-timed/main.go`.

## Scope (out)

- Determinism harness — TASK-047.
- Stall test — TASK-048.
- Replay test — TASK-049.

## Implements

- All M3 FR-TM-* end-to-end.
- Spec tests: `tests/spec/M3/timed_test.go::TestSpec_M3_TimedCompletes`.

## TDD entry point

- Start with: spec test runs example, asserts all 3 federates reach tick 100.

## Acceptance criteria

- [ ] Example runs to completion deterministically.
- [ ] `make verify` green.
