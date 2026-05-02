# TASK-041: RegulationState per federate

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-040 |
| Blocks | TASK-042, TASK-043, TASK-044, TASK-045 |

## Goal

Track regulating flag + lookahead, constrained flag, per federate. Implement `core.TimeManager.EnableRegulation`, `DisableRegulation`, `EnableConstrained`, `DisableConstrained`.

## Scope (in)

- Create `rti/internal/time/regulation.go`.

## Scope (out)

- LBTS, NER, lookahead enforcement, stall — separate tasks.

## Implements

- Requirements: FR-TM-1.
- Spec tests: `tests/spec/M3/regulation_test.go::TestSpec_M3_RegulationState_*`.

## TDD entry point

- Start with: enable regulation on a federate, assert state recorded; disable, assert removed.

## Acceptance criteria

- [ ] Spec tests green.
- [ ] Errors: enabling regulation twice → `ErrTimeAlreadyRegulating`; disabling when not regulating → `ErrTimeNotRegulating`.
- [ ] `make verify` green.
