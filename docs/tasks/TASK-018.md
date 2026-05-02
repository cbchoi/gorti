# TASK-018: Composite codec — HLAopaqueData

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-010, TASK-011, TASK-012, TASK-013 |
| Blocks | TASK-019, TASK-058 |

## Goal

Implement length-prefixed opaque byte blobs: `HLAinteger32BE` length + raw bytes (no element typing, no nested padding).

## Scope (in)

- Create `rti/pkg/encoding/opaque.go`.
- Append ≥3 vectors covering: empty blob, small blob, blob requiring no padding.
- Add `rti/pkg/encoding/opaque_test.go`.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.

## TDD entry point

- Start with: empty blob → `[0x00, 0x00, 0x00, 0x00]`.

## Acceptance criteria

- [ ] Vectors round-trip.
- [ ] Coverage ≥ 80%.
- [ ] `make verify` green.

## Notes / hints

- Opaque is the simplest composite: no internal alignment beyond the 4-byte length prefix.
