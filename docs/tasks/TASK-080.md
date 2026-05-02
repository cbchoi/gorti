# TASK-080: Perf baseline run + recorded report

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-079 |
| Blocks | TASK-084, TASK-085 |

## Goal

Run TASK-079's harness at all four sizes; produce a perf-baseline executable (so others can reproduce). Record results in Agent A's M5 status report.

## Scope (in)

- Create `examples/go-pingpong/perf_main.go` (separate `package main` via build tag `perf`, since `examples/go-pingpong/main.go` is the M2 example).
- Record numbers in `docs/reports/M5/agent-a.md` (per `docs/ORTHOGONALITY.md` §2 last row, status reports are owned by the respective agent).

## Scope (out)

- Encoder benchmarks — TASK-084 (conditional).

## Implements

- Requirements: NFR-PERF-1, NFR-PERF-2.

## TDD entry point

- Start with: run perf_main at size 2, save output to JSON; document in status report.

## Acceptance criteria

- [ ] Harness completes at all 4 sizes.
- [ ] Numbers recorded in `docs/reports/M5/agent-a.md`.
- [ ] Output JSON schema matches TASK-079.
- [ ] `make verify` green.
