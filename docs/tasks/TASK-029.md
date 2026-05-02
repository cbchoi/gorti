# TASK-029: DeclarationManager — deterministic subscriber iteration order

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-027, TASK-028 |
| Blocks | TASK-031, TASK-035 |

## Goal

Provide a single source-of-truth helper for "iterate subscribers of (class, attr) in deterministic handle order." Used by object/interaction routing in TASK-031..033.

## Scope (in)

- Create `rti/internal/declaration/order.go`: a function `SubscribersFor(...) []core.FederateHandle` returning sorted handles.
- Property test: random pub/sub state → fixed iteration order across runs.

## Scope (out)

- The publish/subscribe matrices themselves — TASK-027/028.

## Implements

- Requirements: FR-DM-3, NFR-DET-1.
- Spec tests: `tests/spec/M2/declaration_test.go::TestSpec_M2_SubscriberIterationOrder`.

## TDD entry point

- Start with: property test — generate 100 random subscribe orders, assert iteration always sorted.

## Acceptance criteria

- [ ] Property test green.
- [ ] `make verify` green.
