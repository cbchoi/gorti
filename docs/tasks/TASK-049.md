# TASK-049: M3 replay test — event log replays byte-identical (M3 gate)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-047 |
| Blocks | TASK-076 |

## Goal

M3 event log (from `examples/go-timed/`) replays byte-identical to the original. Same shape as TASK-040 but on the time-managed example.

## Scope (in)

- Create `examples/go-timed/replay_test.go`.

## Scope (out)

- N/A.

## Implements

- Requirements: NFR-DET-2; M3 exit criterion #3.
- Spec tests: `tests/spec/M3/replay_test.go::TestSpec_M3_Replay_ByteIdentical`.

## TDD entry point

- Start with: write log from go-timed, replay, sha256 second == first.

## Acceptance criteria

- [ ] Test green.
- [ ] **This is the M3 milestone gate task.** Passing it triggers `verification:M3` activities by Agent B and Agent C.
- [ ] `make verify` green.
