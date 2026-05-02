# TASK-084: Encoder benchmarks (conditional, gated by perf results)

| Field | Value |
|---|---|
| Status | CANCELLED |
| Assignee | agent-b |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-30 |
| Depends-on | TASK-080 |
| Blocks | none |

## Goal

Add `go test -bench` benchmarks for primitive + composite codecs **only if** TASK-080's perf baseline shows encoding on the hot path.

## Decision rule

Agent B reads `docs/reports/M5/agent-a.md` (TASK-080's deliverable). If encoding accounts for **>5% of CPU time at federation size 25** OR **>10% at size 100**, proceed with the scope below. Otherwise, comment on this task with measured numbers and request the orchestrator move it to `Status: CANCELLED`.

**Do not optimize speculatively** (per `docs/agent-b-fom-encoding.md` §4 M5 anti-goal).

## Scope (in) — IF the decision rule says proceed

- Create `rti/pkg/encoding/integer_bench_test.go`.
- Create `rti/pkg/encoding/float_bench_test.go`.
- Create `rti/pkg/encoding/composite_bench_test.go`.

## Scope (out)

- Optimization itself (separate task once benchmarks pinpoint the hotspot).

## Implements

- Requirements: NFR-PERF-1..4 (benchmark side).

## TDD entry point

- Start with: re-read `docs/reports/M5/agent-a.md`. Confirm or refute hotspot. Decision recorded in the PR description.
- If proceeding: write a baseline benchmark for `HLAfixedRecord` round-trip; observe; expand.

## Acceptance criteria

- [ ] Decision rule resolution noted in PR description (either "hotspot confirmed: <numbers>" or "no hotspot, proposing CANCEL").
- [ ] If proceeding: `go test -bench=. ./rti/pkg/encoding/...` runs cleanly; numbers stable across runs (±5%).
- [ ] `make verify` green.

## Notes / hints

- This is the project's only conditional task. Orchestrator audits compliance at PR review.

## Cancellation (2026-04-30)

Cancelled by orchestrator decision under the task's own decision rule (§ "Decision rule"): TASK-080's perf baseline (`docs/reports/M5/agent-a.md`) is absent — TASK-080 has not run, so no measurement exists demonstrating an encoding hotspot. With no evidence of `>5% CPU at size 25` or `>10% at size 100`, the rule's no-hotspot branch fires: do not optimize speculatively (`docs/agent-b-fom-encoding.md` §4 M5 anti-goal).

Per `docs/DISPATCH.md` §7 / §8, this TASK file remains in `docs/tasks/` for traceability and the ID is not reused. If a future TASK-080 baseline reveals an encoding hotspot, the orchestrator opens a fresh TASK-NNN at the next available number.
