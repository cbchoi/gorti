# TASK-011: Primitive codecs — HLAfloat32/64 BE+LE

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | none |
| Blocks | TASK-013, TASK-014, TASK-015, TASK-016, TASK-017, TASK-018, TASK-019, TASK-051 |

## Goal

Implement encoders and decoders for the four IEEE 754 float primitive types (`HLAfloat32BE`, `HLAfloat32LE`, `HLAfloat64BE`, `HLAfloat64LE`). Encode is byte-identical to vectors; decode round-trips.

## Scope (in)

- Create `rti/pkg/encoding/float.go`: four codec types.
- Append vectors to `tests/conformance/encoding_vectors.json` for `HLAfloat32LE`, `HLAfloat64LE` (existing file has `HLAfloat32BE` and `HLAfloat64BE` cases). At least 3 vectors per type. Use exactly-representable doubles only (0.5, 1.25, 2.0, etc.) per `docs/agent-b-fom-encoding.md` §7 anti-goal.
- Wire `PrimitiveByName`.
- Add `rti/pkg/encoding/float_test.go`.

## Scope (out)

- NaN/Inf cross-language equality — separate follow-up after M1.
- Other primitive types.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.
- Spec tests: `tests/spec/M1/encoding_vectors_test.go::TestSpec_M1_PrimitiveVectorsRoundTrip` (float subtests).

## TDD entry point

- Start with: `float64be-half`, `float32be-one`, etc. subtests.

## Acceptance criteria

- [ ] All float vectors round-trip green.
- [ ] Coverage on `rti/pkg/encoding/float.go` ≥ 80%.
- [ ] Vector additions are additive-only.
- [ ] `make verify` green.

## Notes / hints

- `OctetBoundary` is 4 for float32, 8 for float64.
- Use `math.Float32bits` / `math.Float64bits`; no custom IEEE 754 packing.
