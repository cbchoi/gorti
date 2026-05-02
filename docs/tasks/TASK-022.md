# TASK-022: FederationStore.ResignFederation

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-021 |
| Blocks | TASK-035 |

## Goal

Implement `ResignFederation` with `UNCONDITIONALLY_DIVEST_ATTRIBUTES` only (cut 1). Cleans up owned objects from the federation.

## Scope (in)

- Create `rti/internal/federation/resign.go`.
- On resign: divest all attributes owned by the resigning federate; remove federate from federation roster.

## Scope (out)

- Other resign actions (`NoAction`, `DeleteObjects`, etc.) — cut 2.

## Implements

- Requirements: FR-FM-3.
- Spec tests: `tests/spec/M2/federation_test.go::TestSpec_M2_ResignFederation_*`.

## TDD entry point

- Start with: sequence test {Create, Join alice, Join bob, Resign alice → expect OK + alice's owned objects divested}.

## Acceptance criteria

- [ ] Spec test green.
- [ ] Idempotency: resign of already-resigned federate returns `ErrFederateNotJoined`.
- [ ] `make verify` green.

## Notes / hints

- "Owned objects" not yet meaningful until TASK-030 lands; until then resign is a no-op on object state. Test the federation-side bookkeeping only.
