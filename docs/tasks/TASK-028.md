# TASK-028: DeclarationManager — interaction class pub/sub matrix

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

Per-federation interaction-class → publishers/subscribers map. Symmetric to TASK-027 for interactions.

## Scope (in)

- Create `rti/internal/declaration/interaction_class.go`.
- Methods: `PublishInteractionClass`, `UnpublishInteractionClass`, `SubscribeInteractionClass`, `UnsubscribeInteractionClass`.

## Scope (out)

- Object-class matrix — TASK-027.
- Order helper — TASK-029.

## Implements

- Requirements: FR-DM-2.
- Spec tests: `tests/spec/M2/declaration_test.go::TestSpec_M2_InteractionClassPubSub_*`.

## TDD entry point

- Start with: publish, subscribe, query — symmetric to TASK-027.

## Acceptance criteria

- [ ] Spec test green.
- [ ] `make verify` green.
