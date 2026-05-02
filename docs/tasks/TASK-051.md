# TASK-051: Python float codecs

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-050 |
| Blocks | TASK-059 |

## Goal

Mirror TASK-011: Python codecs for `HLAfloat32BE/LE`, `HLAfloat64BE/LE`. Pass all float vectors byte-identical to Go.

## Scope (in)

- Create `pysdk/rti1516e/encoding/float_codec.py` (named `float_codec` to avoid shadowing built-in).
- Create `pysdk/tests/test_encoding_float.py`.

## Scope (out)

- Other types.

## Implements

- Requirements: FR-ENC-2 (Python).

## TDD entry point

- Start with: `float64be-half` round-trip parametrized test.

## Acceptance criteria

- [ ] All float vectors green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.

## Notes / hints

- Use `struct.pack(">f", v)` / `struct.pack(">d", v)`.
