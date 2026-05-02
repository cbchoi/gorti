# TASK-027: DeclarationManager — object class pub/sub matrix

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-021 |
| Blocks | TASK-029, TASK-035 |

## Goal

Per-federation object-class × attribute → publishers/subscribers map, with deterministic ordering.

## Scope (in)

- Create `rti/internal/declaration/object_class.go`.
- Methods: `PublishObjectClassAttributes`, `UnpublishObjectClassAttributes`, `SubscribeObjectClassAttributes`, `UnsubscribeObjectClassAttributes`.
- All map iteration produces sorted handle order.

## Scope (out)

- Interaction matrix — TASK-028.
- Subscriber-iteration helper — TASK-029.

## Implements

- Requirements: FR-DM-1.
- Spec tests: `tests/spec/M2/declaration_test.go::TestSpec_M2_ObjectClassPubSub_*`.

## TDD entry point

- Start with: publish an attribute, query publishers, assert the publisher is recorded; subscribe; assert subscriber recorded.

## Acceptance criteria

- [ ] Spec test green.
- [ ] No reliance on Go map iteration order in observable behavior.
- [ ] `make verify` green.

## Notes / hints

- Use sorted slices keyed by handle for deterministic iteration.
