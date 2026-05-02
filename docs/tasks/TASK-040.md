# TASK-040: M2 replay harness — feed log back; second log byte-identical (M2 gate)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-026, TASK-039 |
| Blocks | TASK-041 |

## Goal

Take the M2 event log produced by TASK-039; replay through fresh RTI; assert second log byte-identical to first.

## Scope (in)

- Create `examples/go-pingpong/replay_test.go`.

## Scope (out)

- N/A.

## Implements

- Requirements: NFR-DET-2; M2 exit criterion #3.
- Spec tests: `tests/spec/M2/replay_test.go::TestSpec_M2_Replay_ByteIdentical`.

## TDD entry point

- Start with: write log, replay, sha256 second == first.

## Acceptance criteria

- [ ] Test green.
- [ ] **This is the M2 milestone gate task.** Passing it triggers `verification:M2` activities by Agent B and Agent C.
- [ ] `make verify` green.
