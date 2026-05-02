# TASK-044: Lookahead enforcement via core.Clock

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-043 |
| Blocks | none |

## Goal

All time-related operations in `rti/internal/time/` use `core.Clock`; no `time.Now()` calls. Reject NER with timestamp violating lookahead with `ErrTimeRequestInPast`.

## Scope (in)

- Create `rti/internal/time/lookahead.go`: lookahead check helper invoked by NER.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-TM-5; NFR-DET-1.
- Spec tests: `tests/spec/M3/lookahead_test.go::TestSpec_M3_Lookahead_*`.

## TDD entry point

- Start with: NER with timestamp < current + lookahead → `ErrTimeRequestInPast`.

## Acceptance criteria

- [ ] Spec test green.
- [ ] `golangci-lint` clean (forbidigo blocks `time.Now`).
- [ ] `make verify` green.
