# TASK-025: EventLog Reader — streaming iterator

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-024 |
| Blocks | TASK-026 |

## Goal

Implement `core.EventLog.OpenReader` and `EventLogReader.Next`. Reader iterates records in seq order.

## Scope (in)

- Create `rti/internal/eventlog/reader.go`.
- Truncation handling: at record boundary → clean `io.EOF` from `Next`; mid-record → `io.ErrUnexpectedEOF` (no panic).
- Header validation: wrong magic → `ErrWireMalformedMessage`; wrong version → `ErrWireVersionMismatch`.

## Scope (out)

- Replayer — TASK-026.

## Implements

- Requirements: FR-EVT-2.
- Spec tests: `tests/spec/M2/eventlog_test.go::TestSpec_M2_EventLog_Reader_*`.

## TDD entry point

- Start with: write 5 events, read back via iterator, assert all 5 returned in seq order.

## Acceptance criteria

- [ ] Spec tests green.
- [ ] Truncation tests (cut at record boundary, mid-record) green.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.

- `EventLogReader` interface defined in `rti/internal/core/eventlog.go` (frozen).
