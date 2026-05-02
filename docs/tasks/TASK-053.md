# TASK-053: Python string codecs

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-052 |
| Blocks | TASK-059 |

## Goal

Python codecs for `HLAASCIIstring` and `HLAunicodeString` (UTF-16BE). Length prefix is `HLAinteger32BE` (count of code units).

## Scope (in)

- Create `pysdk/rti1516e/encoding/string_codec.py`.
- Create `pysdk/tests/test_encoding_string.py`.

## Scope (out)

- Other primitive types.
- Composites.

## Implements

- Requirements: FR-ENC-2.

## TDD entry point

- Start with: empty string round-trip.

## Acceptance criteria

- [ ] All string vectors green; byte-identical to Go.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
