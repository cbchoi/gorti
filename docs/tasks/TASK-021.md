# TASK-021: FederationStore.JoinFederation — deterministic handle assignment

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-020 |
| Blocks | TASK-022, TASK-023, TASK-027, TASK-028, TASK-030 |

## Goal

Implement `JoinFederation`. Federate handles are assigned deterministically — same input ordering produces same handles across runs.

## Scope (in)

- Create `rti/internal/federation/join.go` with `JoinFederation` method.
- Reject duplicate `federateName` within the same federation (`ErrFederateAlreadyJoined`).
- Reject join on non-existent federation (`ErrFederationNotFound`).

## Scope (out)

- Event-log integration — TASK-024..026.
- Pub/sub state — TASK-027..029.

## Implements

- Requirements: FR-FM-2, NFR-DET-1.
- Spec tests: `tests/spec/M2/federation_test.go::TestSpec_M2_JoinFederation_*`.

## TDD entry point

- Start with: spec test asserting same input → same handles across 10 invocations.

## Acceptance criteria

- [ ] Determinism property test (random join orders → fixed handle assignment by name) green.
- [ ] Concurrent-join determinism test (50 goroutines, deterministic input-order channel → handles 1..50 stable across 10 runs) green.
- [ ] `make verify` green.

## Notes / hints

- Determinism rule: handles assigned by sort order of `federateName`, not arrival order.
