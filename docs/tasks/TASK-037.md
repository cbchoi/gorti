# TASK-037: cmd/rtid wiring + Prometheus metrics

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-034, TASK-035, TASK-036 |
| Blocks | TASK-038 |

## Goal

Replace the `TODO(#1)` in `rti/cmd/rtid/main.go` (M0 skeleton). Wire all four gRPC services + Prometheus metrics handler on `--metrics-listen`.

## Scope (in)

- Modify `rti/cmd/rtid/main.go`: instantiate `Manager`, declaration manager, object registry, time manager (stub for M2), event log; register all four gRPC services; start metrics endpoint.
- Create `rti/cmd/rtid/metrics.go`: Prometheus collectors for federation counts, federate counts per federation, event log seq.

## Scope (out)

- Time manager wiring (M3).
- TLS — flags exist from M0 but M2 keeps server insecure; TLS is a C-* security item, not in cut 1.

## Implements

- Requirements: NFR-OPS-1, NFR-OPS-2.
- Spec tests: `tests/spec/M2/rtid_smoke_test.go::TestSpec_M2_Rtid_*` (process-level smoke).

## TDD entry point

- Start with: launch `rtid --listen :8442 --metrics-listen :9090`; curl `/metrics` → returns Prom text format.

## Acceptance criteria

- [ ] `rtid` starts; both endpoints reachable.
- [ ] `/metrics` exposes federation/federate/seq counters.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.
