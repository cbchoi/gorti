# TASK-055: Python HLAvariableArray

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-050, TASK-051, TASK-052, TASK-053 |
| Blocks | TASK-059 |

## Goal

Mirror TASK-015. Python `HLAvariableArray` with 4-byte length prefix.

## Scope (in)

- Create `pysdk/rti1516e/encoding/variable_array.py`.
- Create `pysdk/tests/test_encoding_variable_array.py`.

## Scope (out)

- Fixed arrays — TASK-054.
- Other composites.

## Implements

- Requirements: FR-ENC-2.

## TDD entry point

- Start with: empty variable array → `b"\x00\x00\x00\x00"`.

## Acceptance criteria

- [ ] All variable_array vectors green; byte-identical.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
