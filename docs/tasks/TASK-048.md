# TASK-048: M3 stall test — kill federate; assert timeout fires within 60s ± 5s

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-046 |
| Blocks | none |

## Goal

Integration test that kills one federate mid-run and asserts:
1. Stall timeout fires within configured window (60s ± 5s).
2. Diagnostic identifies the killed federate by handle and name.

## Scope (in)

- Create `examples/go-timed/stall_test.go`.

## Scope (out)

- Determinism — TASK-047.
- Replay — TASK-049.

## Implements

- Requirements: FR-TM-6.
- Spec tests: `tests/spec/M3/stall_integration_test.go::TestSpec_M3_StallFiresAndIdentifiesFederate`.

## TDD entry point

- Start with: launch 3 federates, kill federate 2 at tick 5, assert `FederationHalted` event for federate 2 appears within 60s.

## Acceptance criteria

- [ ] Spec test green.
- [ ] Diagnostic message names the federate by both handle and name.
- [ ] `make verify` green.
