# TASK-024: EventLog Writer — magic header, length-prefixed records, monotonic seq

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-019 |
| Blocks | TASK-025, TASK-030 |

## Goal

Implement `core.EventLog.Append` and the binary file format defined in `proto/rti/v1/eventlog.proto`. Magic header `KDRTI\0\1\0`, version field, monotonic sequence numbers.

## Scope (in)

- Create `rti/internal/eventlog/format.go`: magic constant, header struct, record framing helpers.
- Create `rti/internal/eventlog/writer.go`: `Writer` type implementing `core.EventLog` write side.
- Round-trip property test (writer + in-memory reader stub): write → read → equal events.

## Scope (out)

- Reader — TASK-025.
- Replayer — TASK-026.

## Implements

- Requirements: FR-EVT-1, NFR-CRASH-1.
- Spec tests: `tests/spec/M2/eventlog_test.go::TestSpec_M2_EventLog_HeaderFormat`.

## TDD entry point

- Start with: assertion that first 8 bytes of any new log == `KDRTI\0\1\0`.

## Acceptance criteria

- [ ] Header byte-identical across runs.
- [ ] Record format: 4-byte length prefix + Protobuf-encoded record.
- [ ] Monotonic seq starts at 1, increments by 1 per `Append`.
- [ ] `make verify` green.

## Notes / hints

- Use `proto/rti/v1/eventlog.proto` types (already frozen).
- `core.EventLog` interface in `rti/internal/core/eventlog.go` is frozen — implement against it.
