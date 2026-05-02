# TASK-030: ObjectRegistry — deterministic object handle assignment + write-ahead to event log

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-021, TASK-024 |
| Blocks | TASK-031, TASK-032, TASK-033, TASK-036 |

## Goal

Per-federation `Registry` with monotonic object handle assignment, recorded in the event log before any other side effect.

## Scope (in)

- Create `rti/internal/object/registry.go`: `Register` method assigns next handle, writes `RegisterObject` event to log, returns handle.

## Scope (out)

- Discover fanout — TASK-031.
- Update path — TASK-032.
- Interaction path — TASK-033.

## Implements

- Requirements: FR-OM-1, NFR-DET-1; FR-EVT-1 (write-ahead).
- Spec tests: `tests/spec/M2/object_test.go::TestSpec_M2_RegisterObject_DeterministicHandle_WriteAhead`.

## TDD entry point

- Start with: register 5 objects, assert handles 1..5 in order; inspect log → 5 register events present.

## Acceptance criteria

- [ ] Spec test green.
- [ ] Write-ahead invariant: log entry committed before any in-memory state change observable.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.
