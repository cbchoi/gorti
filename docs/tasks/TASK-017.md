# TASK-017: Composite codec — HLAvariantRecord

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-016 |
| Blocks | TASK-019, TASK-057 |

## Goal

Implement variant records: discriminator field + alternative payload types selected by discriminator value.

## Scope (in)

- Create `rti/pkg/encoding/variant_record.go`.
- Append ≥3 vectors with different discriminator values exercising different alternatives.
- Add `rti/pkg/encoding/variant_record_test.go`.

## Scope (out)

- FOM-side validation that variant record has a discriminator field — TASK-089.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.

## TDD entry point

- Start with: a variant record with discriminator `HLAinteger32BE` and two alternatives (one `HLAoctet`, one `HLAfloat64BE`); encode with each discriminator value, byte-diff against expected.

## Acceptance criteria

- [ ] Variant vectors round-trip.
- [ ] Decode with unknown discriminator returns `ErrEncTypeMismatch` from `core.errors`.
- [ ] Coverage ≥ 80%.
- [ ] `make verify` green.

## Notes / hints

- Discriminator is encoded first; payload follows with padding to payload-type's boundary.
