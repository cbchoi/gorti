# TASK-079: Perf baseline harness at sizes 2/5/25/100

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-078 |
| Blocks | TASK-080, TASK-084 |

## Goal

Reproducible perf harness measuring throughput and p50/p99 latency at federation sizes 2, 5, 25, 100.

## Scope (in)

- Create `rti/internal/perf/baseline.go`.
- Create `rti/internal/perf/baseline_test.go`.

## Scope (out)

- Running it + recorded report — TASK-080.

## Implements

- Requirements: NFR-PERF-1, NFR-PERF-2, NFR-SCALE-2.
- Spec tests: `tests/spec/M5/perf_test.go::TestSpec_M5_PerfHarnessRuns`.

## TDD entry point

- Start with: harness runs at size 2, produces JSON output with throughput + p50/p99 fields.

## Acceptance criteria

- [ ] Harness runs cleanly at all 4 sizes.
- [ ] JSON output schema documented in source.
- [ ] `make verify` green.
