# TASK-054: Python HLAfixedArray

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

Mirror TASK-014. Python implementation of `HLAfixedArray` — alignment-aware.

## Scope (in)

- Create `pysdk/rti1516e/encoding/fixed_array.py`.
- Create `pysdk/tests/test_encoding_fixed_array.py`.

## Scope (out)

- Variable arrays — TASK-055.
- Other composites.

## Implements

- Requirements: FR-ENC-2.

## TDD entry point

- Start with: `HLAinteger32BE[3]` round-trip.

## Acceptance criteria

- [ ] All fixed_array vectors green; byte-identical.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
