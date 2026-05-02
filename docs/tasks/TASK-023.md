# TASK-023: FederationStore.DestroyFederation

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-021 |
| Blocks | TASK-034 |

## Goal

Implement `DestroyFederation`. Reject when joined federate count > 0 with `ErrFederationHasFederatesJoined`.

## Scope (in)

- Create `rti/internal/federation/destroy.go`.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-FM-5.
- Spec tests: `tests/spec/M2/federation_test.go::TestSpec_M2_DestroyFederation_*`.

## TDD entry point

- Start with: sequence test {Create, Join, Destroy → expect ErrFederationHasFederatesJoined; Resign, Destroy → expect OK}.

## Acceptance criteria

- [ ] Spec test green.
- [ ] `make verify` green.

## Notes / hints

- Destroy-non-existent → `ErrFederationNotFound`.
