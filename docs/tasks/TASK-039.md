# TASK-039: M2 determinism harness — 10× same seed; sha256 byte-identical event logs

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-038 |
| Blocks | TASK-040 |

## Goal

Run `examples/go-pingpong/` 10 consecutive times with the same seed; assert all 10 event-log files have identical sha256.

## Scope (in)

- Create `examples/go-pingpong/determinism_test.go` (build tag `integration`).

## Scope (out)

- Replay test — TASK-040.

## Implements

- Requirements: NFR-DET-1.
- Spec tests: `tests/spec/M2/determinism_test.go::TestSpec_M2_Determinism_TenRuns` (orchestrator pre-work; this task's local test feeds into the spec test).

## TDD entry point

- Start with: assertion that two runs produce identical sha256 logs; extend to 10.

## Acceptance criteria

- [ ] 10 runs all byte-identical.
- [ ] `make determinism` green.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.

- A determinism flake is a real bug; do not paper over (`docs/agent-a-rti-core.md` §7).
