# TASK-056: Python HLAfixedRecord

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-050, TASK-051, TASK-052, TASK-053 |
| Blocks | TASK-057, TASK-059 |

## Goal

Mirror TASK-016. Python `HLAfixedRecord` mapped to `dataclass`.

## Scope (in)

- Create `pysdk/rti1516e/encoding/fixed_record.py`.
- Create `pysdk/tests/test_encoding_fixed_record.py`.

## Scope (out)

- Variant records — TASK-057.

## Implements

- Requirements: FR-ENC-2.

## TDD entry point

- Start with: `fixed-record-octet-float64` parametrized round-trip — must produce same bytes as Go.

## Acceptance criteria

- [ ] All fixed_record vectors green; byte-identical to Go.
- [ ] Per-field padding correct.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.

## Notes / hints

- Map record fields to `dataclass` fields in declaration order. Use `dataclasses.fields()` for iteration to preserve order.
