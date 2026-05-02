# TASK-078: gRPC handler hardening under cross-language load

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-075, TASK-077 |
| Blocks | TASK-079, TASK-081 |

## Goal

Long-soak + fuzz harness against the live RTI under cross-language load. Assert: no panics, no goroutine leaks, all errors carry codes from `proto/rti/v1/errors.proto`.

## Scope (in)

- Create `rti/internal/transport/grpc/load_test.go` (build tag `soak`).
- Default 10-min run; configurable via env.

## Scope (out)

- Perf baseline harness — TASK-079.

## Implements

- Requirements: NFR-PERF-1..4.
- Spec tests: `tests/spec/M5/soak_test.go::TestSpec_M5_Soak_NoPanicNoLeak`.

## TDD entry point

- Start with: 30-second soak smoke; extend to 10 min in CI under `soak` tag.

## Acceptance criteria

- [ ] Soak test green at default 10-min run.
- [ ] `pprof` shows no goroutine leak.
- [ ] All error responses carry valid error codes.
- [ ] `make verify` green.
