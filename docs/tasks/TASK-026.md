# TASK-026: EventLog Replayer — re-feeds events through live code path

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-025, TASK-032 |
| Blocks | TASK-040 |

## Goal

Implement `Replayer` that takes an event log and re-feeds events through the same code path used by live operation, producing a new event log byte-identical to the source.

## Scope (in)

- Create `rti/internal/eventlog/replayer.go`.
- Replayer drives federation/object code paths via the same in-process API used by gRPC handlers.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-EVT-3, NFR-DET-2.
- Spec tests: `tests/spec/M2/eventlog_test.go::TestSpec_M2_Replay_ByteIdentical`.

## TDD entry point

- Start with: write a small log, replay, sha256 second log == first.

## Acceptance criteria

- [ ] Spec test green.
- [ ] No fast-path bypass — replay goes through the same write-ahead path as live operation (`docs/agent-a-rti-core.md` §7).
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.

- Critical: do NOT add a "fast path" that skips the event log on replay — it would break the determinism guarantee.
