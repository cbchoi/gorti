# TASK-015: Composite codec — HLAvariableArray

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-010, TASK-011, TASK-012, TASK-013 |
| Blocks | TASK-019, TASK-055 |

## Goal

Implement encode/decode for variable-length arrays: 4-byte length prefix (`HLAinteger32BE` count of elements), then elements with element-type padding.

## Scope (in)

- Create `rti/pkg/encoding/variable_array.go`.
- Append ≥3 composite vectors covering: empty variable array, variable array of `HLAinteger32BE`, variable array of `HLAfloat64BE` (padding test).
- Add `rti/pkg/encoding/variable_array_test.go`.

## Scope (out)

- `CodecFor` wiring — TASK-019.
- Fixed array — TASK-014.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.

## TDD entry point

- Start with: empty variable array → `[0x00, 0x00, 0x00, 0x00]` (4-byte zero length).

## Acceptance criteria

- [ ] Unit tests green.
- [ ] Coverage ≥ 80% on the new file.
- [ ] `make verify` green.

## Notes / hints

- `OctetBoundary` of variable array = max(4, element boundary). The length prefix forces at least 4-byte alignment.
