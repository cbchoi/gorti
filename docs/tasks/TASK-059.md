# TASK-059: codec_for(type) Python dispatcher; full vector conformance

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-051, TASK-052, TASK-053, TASK-054, TASK-055, TASK-056, TASK-057, TASK-058 |
| Blocks | TASK-067, TASK-073 |

## Goal

Implement `pysdk.rti1516e.encoding.dispatch.codec_for(type)` so a single conformance test passes 100% of `tests/conformance/encoding_vectors.json`.

## Scope (in)

- Create `pysdk/rti1516e/encoding/dispatch.py`.
- Create `pysdk/tests/test_encoding_conformance.py`: parametrized over **all** vectors (primitive + composite) in the JSON file.

## Scope (out)

- New codecs.

## Implements

- Requirements: FR-ENC-2; M4 exit criterion #3.

## TDD entry point

- Start with: full conformance test, all primitives green, composite skipped until dispatcher routes them.

## Acceptance criteria

- [ ] 100% of golden vectors pass byte-identical.
- [ ] `mypy --strict pysdk/rti1516e/encoding/` clean.
- [ ] `pytest pysdk/tests/test_encoding_conformance.py` green.
- [ ] `make verify` green.

## Notes / hints

- Mirror Agent B's `CodecFor` (TASK-019) in API shape so cross-language debugging is symmetric.
