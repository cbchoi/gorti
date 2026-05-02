# TASK-010: Primitive codecs — HLAinteger16/32/64 BE+LE

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | none |
| Blocks | TASK-013, TASK-014, TASK-015, TASK-016, TASK-017, TASK-018, TASK-019, TASK-050 |

## Goal

Implement encoders and decoders for all six integer primitive types per IEEE 1516.2-2010 §4. Encode produces byte-identical output to the integer vectors in `tests/conformance/encoding_vectors.json`; decode round-trips.

## Scope (in)

- Create `rti/pkg/encoding/integer.go`: six type definitions implementing `Codec` (`HLAinteger16BE`, `HLAinteger16LE`, `HLAinteger32BE`, `HLAinteger32LE`, `HLAinteger64BE`, `HLAinteger64LE`).
- Append vectors to `tests/conformance/encoding_vectors.json` for `HLAinteger16BE`, `HLAinteger16LE`, `HLAinteger32LE`, `HLAinteger64LE` (existing file already has `HLAinteger32BE` and `HLAinteger64BE` vectors). At least 3 vectors per type per `docs/agent-b-fom-encoding.md` §4 M1 exit criterion #4.
- Wire `PrimitiveByName` to recognize these six type names.
- Add `rti/pkg/encoding/integer_test.go` with table-driven round-trip tests against the vectors.

## Scope (out)

- Float, byte, boolean, char, string, composite — separate tasks.
- `CodecFor(model.DataType)` — TASK-019.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.
- Spec tests: `tests/spec/M1/encoding_vectors_test.go::TestSpec_M1_PrimitiveVectorsRoundTrip` (integer subtests).

## TDD entry point

- Start with: integer subtests of `TestSpec_M1_PrimitiveVectorsRoundTrip`. Currently red because `PrimitiveByName` returns `ErrNotImplemented`.

## Acceptance criteria

- [ ] All integer vectors in `tests/conformance/encoding_vectors.json` round-trip green.
- [ ] Coverage on `rti/pkg/encoding/integer.go` ≥ 80%.
- [ ] No new vectors modify or delete existing entries (additive-only per `docs/ORTHOGONALITY.md` §3).
- [ ] `make verify` green.

## Notes / hints

- `OctetBoundary` is 2/4/8 for 16/32/64-bit respectively.
- Two's-complement for negatives.
