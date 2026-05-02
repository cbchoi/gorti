# TASK-058: Python HLAopaqueData

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

Mirror TASK-018. Python `HLAopaqueData` — length-prefixed byte blob.

## Scope (in)

- Create `pysdk/rti1516e/encoding/opaque.py`.
- Create `pysdk/tests/test_encoding_opaque.py`.

## Scope (out)

- Other composites.

## Implements

- Requirements: FR-ENC-2.

## TDD entry point

- Start with: empty blob → `b"\x00\x00\x00\x00"`.

## Acceptance criteria

- [ ] All opaque vectors green; byte-identical.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
