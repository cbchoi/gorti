# TASK-057: Python HLAvariantRecord

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-056 |
| Blocks | TASK-059 |

## Goal

Mirror TASK-017. Python `HLAvariantRecord` with discriminator → `Union[...]`.

## Scope (in)

- Create `pysdk/rti1516e/encoding/variant_record.py`.
- Create `pysdk/tests/test_encoding_variant_record.py`.

## Scope (out)

- Fixed records — TASK-056.

## Implements

- Requirements: FR-ENC-2.

## TDD entry point

- Start with: variant with discriminator and two alternatives; round-trip with each.

## Acceptance criteria

- [ ] All variant_record vectors green; byte-identical.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
