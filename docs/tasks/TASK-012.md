# TASK-012: Primitive codecs — HLAoctet, HLAoctetPairBE/LE, HLAboolean, HLAASCIIchar, HLAunicodeChar

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | none |
| Blocks | TASK-013, TASK-014, TASK-015, TASK-016, TASK-017, TASK-018, TASK-019, TASK-052 |

## Goal

Implement single-octet and small-fixed-width primitive codecs: `HLAoctet`, `HLAoctetPairBE`, `HLAoctetPairLE`, `HLAboolean`, `HLAASCIIchar`, `HLAunicodeChar` (UTF-16BE).

## Scope (in)

- Create `rti/pkg/encoding/byte.go` with six codec types.
- Append ≥3 vectors per type to `tests/conformance/encoding_vectors.json` (existing file has one each of `boolean-true/false`, `octet-ab`, `ascii-char-A`, `unicode-char-A`).
- Wire `PrimitiveByName`.
- Add `rti/pkg/encoding/byte_test.go`.

## Scope (out)

- Strings — TASK-013.
- Composites — TASK-014..018.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.
- Spec tests: `tests/spec/M1/encoding_vectors_test.go::TestSpec_M1_PrimitiveVectorsRoundTrip` (boolean / octet / char subtests).

## TDD entry point

- Start with: `boolean-true`, `octet-ab`, `ascii-char-A`, `unicode-char-A` subtests.

## Acceptance criteria

- [ ] All corresponding vectors round-trip green.
- [ ] Coverage on `rti/pkg/encoding/byte.go` ≥ 80%.
- [ ] `make verify` green.

## Notes / hints

- `HLAboolean` is encoded as `HLAinteger32BE` per spec (existing vectors confirm).
- `HLAunicodeChar` is UTF-16BE, octet boundary 2.
