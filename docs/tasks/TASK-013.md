# TASK-013: String codecs — HLAASCIIstring, HLAunicodeString

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-012 |
| Blocks | TASK-019, TASK-053 |

## Goal

Implement length-prefixed string codecs: `HLAASCIIstring` (1-byte chars) and `HLAunicodeString` (UTF-16BE chars). Length prefix is `HLAinteger32BE` (count of code units).

## Scope (in)

- Create `rti/pkg/encoding/string.go`.
- Append ≥3 vectors per type covering: empty string, ASCII printable, multi-byte sequences.
- Wire `PrimitiveByName`.
- Add `rti/pkg/encoding/string_test.go`.

## Scope (out)

- UTF-8 string types (not in 1516.2 §4 set).

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.
- Spec tests: `tests/spec/M1/encoding_vectors_test.go::TestSpec_M1_PrimitiveVectorsRoundTrip` (string subtests, after vectors are added in this task).

## TDD entry point

- Start with: new vector `ascii-string-empty` round-trip.

## Acceptance criteria

- [ ] String vectors round-trip green.
- [ ] Padding to 4-byte boundary is correct (length prefix is itself 4-byte aligned).
- [ ] `make verify` green.

## Notes / hints

- The length prefix is the count of code units, NOT bytes. For `HLAunicodeString`, an N-character string is `4 + 2*N` bytes.
