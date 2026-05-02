# TASK-077: Best-effort RO semantics for declared best-effort attributes

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-076 |
| Blocks | TASK-078, TASK-082 |

## Goal

When federation mode == best-effort AND attribute is declared best-effort in FOM, deliver via RO (Receive Order) instead of TSO (Time Stamp Order). Single-attribute paths only — no batched delivery.

## Scope (in)

- Create `rti/internal/transport/grpc/best_effort.go`.

## Scope (out)

- Batched delivery (deferred).

## Implements

- Requirements: FR-OM-3.
- Spec tests: `tests/spec/M5/best_effort_test.go::TestSpec_M5_BestEffort_RODelivery`.

## TDD entry point

- Start with: declare best-effort attribute; subscribe; publish update; assert delivery is RO (no TSO ordering applied).

## Acceptance criteria

- [ ] Spec test green.
- [ ] `make verify` green.
