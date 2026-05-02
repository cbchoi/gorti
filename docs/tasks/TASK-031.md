# TASK-031: ObjectRegistry — Discover callback fanout in deterministic order

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-029, TASK-030 |
| Blocks | TASK-036 |

## Goal

When a new object is registered, fan out `DiscoverObjectInstance` events to subscribers of that class in deterministic handle order.

## Scope (in)

- Create `rti/internal/object/discover.go`: discover-fanout logic invoking `core.Outbox.Send` per subscriber.

## Scope (out)

- Update path — TASK-032.

## Implements

- Requirements: FR-OM-2.
- Spec tests: `tests/spec/M2/object_test.go::TestSpec_M2_DiscoverFanoutOrder`.

## TDD entry point

- Start with: subscribe handles {3, 1, 2} to a class; register one object; assert outbox sees handles in {1, 2, 3} order.

## Acceptance criteria

- [ ] Spec test green.
- [ ] Uses `TASK-029`'s `SubscribersFor` helper, not its own ordering.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.
