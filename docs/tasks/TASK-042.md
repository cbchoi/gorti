# TASK-042: LBTS calculation — deterministic all-reduce

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-041 |
| Blocks | TASK-043 |

## Goal

`LBTS = min(currentTime[h] + lookahead[h])` over regulating federates; `+Inf` when none. Tie-break per `docs/AGENTS.md` §10 (federate handle → object handle → attribute handle).

## Scope (in)

- Create `rti/internal/time/lbts.go`: pure function over a regulating-set snapshot.

## Scope (out)

- NER orchestration — TASK-043.

## Implements

- Requirements: FR-TM-3.
- Spec tests: `tests/spec/M3/lbts_test.go::TestSpec_M3_LBTS_*`.

## TDD entry point

- Start with: property test — generate random regulating-set states, assert formula holds.

## Acceptance criteria

- [ ] Property test green over ≥1000 random states.
- [ ] Empty regulating set → `+Inf`.
- [ ] Tie-break is deterministic and matches `docs/AGENTS.md` §10.
- [ ] `make verify` green.
