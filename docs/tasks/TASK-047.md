# TASK-047: M3 determinism harness — 20 randomized scenarios

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-046 |
| Blocks | TASK-049 |

## Goal

Generate 20 randomized scenarios (varying message timestamps within lookahead, join order, regulating subsets); assert each scenario produces byte-identical event logs across multiple iterations.

## Scope (in)

- Create `examples/go-timed/determinism_test.go` (build tag `integration`).
- Scenario generator with explicit seed; record seed in scenario metadata.

## Scope (out)

- Stall test — TASK-048.

## Implements

- Requirements: NFR-DET-1, NFR-DET-2.
- Spec tests: `tests/spec/M3/determinism_test.go::TestSpec_M3_Determinism_*`.

## TDD entry point

- Start with: 1-scenario harness; extend to 20.

## Acceptance criteria

- [ ] All 20 scenarios deterministic across 3 iterations each.
- [ ] `make determinism` green.
- [ ] `make verify` green.
