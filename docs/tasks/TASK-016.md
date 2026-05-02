# TASK-016: Composite codec — HLAfixedRecord

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-010, TASK-011, TASK-012, TASK-013 |
| Blocks | TASK-017, TASK-019, TASK-056 |

## Goal

Implement encode/decode for fixed records — ordered named fields with per-field alignment padding.

## Scope (in)

- Create `rti/pkg/encoding/fixed_record.go`.
- Enable the `_disabled: true` example vector `fixed-record-octet-float64` in `tests/conformance/encoding_vectors.json` by removing its `_disabled` flag (treated as a vector addition since the entry was a placeholder, NOT a modification).
- Add ≥2 more fixed-record vectors covering nested records and varied padding shapes.
- Add `rti/pkg/encoding/fixed_record_test.go`.

## Scope (out)

- Variant records — TASK-017.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.

## TDD entry point

- Start with: round-trip of `fixed-record-octet-float64` — `{a: 5, b: 1.0}` → `0x05` + 7 padding bytes + 8 bytes of `0x3FF0000000000000`.

## Acceptance criteria

- [ ] `fixed-record-octet-float64` vector passes (encode + decode + length).
- [ ] Padding correctness verified by explicit byte-diff (not just round-trip).
- [ ] Coverage ≥ 80%.
- [ ] `make verify` green.

## Notes / hints

- Per-field padding rule: pad before each field to its `OctetBoundary`, computed from start of record.
- Record's `OctetBoundary` = max boundary across fields.
- Don't trust round-trip alone — write tests that assert exact byte layout.
