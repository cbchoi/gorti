# TASK-052: Python octet/boolean/char codecs

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-050 |
| Blocks | TASK-053, TASK-059 |

## Goal

Python codecs for `HLAoctet`, `HLAoctetPairBE/LE`, `HLAboolean`, `HLAASCIIchar`, `HLAunicodeChar`.

## Scope (in)

- Create `pysdk/rti1516e/encoding/byte_codec.py`.
- Create `pysdk/tests/test_encoding_byte.py`.

## Scope (out)

- Strings — TASK-053.
- Composites.

## Implements

- Requirements: FR-ENC-2.

## TDD entry point

- Start with: `boolean-true`, `octet-ab` parametrized round-trip.

## Acceptance criteria

- [ ] All corresponding vectors green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
