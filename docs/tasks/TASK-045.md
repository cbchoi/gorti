# TASK-045: Stall timeout

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-043 |
| Blocks | TASK-046, TASK-048 |

## Goal

Configurable per-federation stall timeout (default 60s). On fire, halt federation with diagnostic naming the stalled federate.

## Scope (in)

- Create `rti/internal/time/stall.go`.
- `StallTimeout` field on `core.CreateFederationRequest` is already present (M0); read and apply.
- Halt via `FederationHalted{cause: stall, federate: H}` event in event log.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-TM-6, NFR-PERF-3.
- Spec tests: `tests/spec/M3/stall_test.go::TestSpec_M3_Stall_*`.

## TDD entry point

- Start with: sequence test with `FakeClock` — start federation with 60s timeout, advance clock past timeout without progress, assert `FederationHalted` event appears with correct federate handle.

## Acceptance criteria

- [ ] Spec tests green.
- [ ] Timeout is observable through event log only — no panic, no log.Fatal.
- [ ] `make verify` green.
