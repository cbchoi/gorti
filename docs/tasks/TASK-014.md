# TASK-014: Composite codec — HLAfixedArray

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-010, TASK-011, TASK-012, TASK-013 |
| Blocks | TASK-019, TASK-054 |

## Goal

Implement encode/decode for arrays of fixed cardinality. Result's `OctetBoundary` equals the element's boundary.

## Scope (in)

- Create `rti/pkg/encoding/fixed_array.go`.
- Append ≥3 composite vectors with `type: {"kind": "HLAfixedArray", "element": "...", "cardinality": N}` covering: array of `HLAinteger32BE` (no padding between), array of `HLAfloat64BE` (8-byte boundary), array of `HLAoctet` (1-byte boundary, no padding).
- Add `rti/pkg/encoding/fixed_array_test.go`.

## Scope (out)

- `CodecFor(model.DataType)` wiring — TASK-019.
- Variable arrays — TASK-015.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.
- Will contribute to `tests/spec/M1/encoding_vectors_test.go::TestSpec_M1_CompositeVectorsRoundTrip` (test currently `t.Skip`s pending TASK-019).

## TDD entry point

- Start with: a unit test in `rti/pkg/encoding/fixed_array_test.go` that constructs a `fixed_array` codec by hand (not via `CodecFor`) and round-trips an `HLAinteger32BE[3]`.

## Acceptance criteria

- [ ] Unit tests green.
- [ ] Coverage on `rti/pkg/encoding/fixed_array.go` ≥ 80%.
- [ ] Composite vectors are syntactically valid (loadable by spec test even though composite test is still skipped).
- [ ] `make verify` green.

## Notes / hints

- Padding between elements is determined by element type's `OctetBoundary`.
- Fixed array of size 0 is a valid zero-length encoding.
