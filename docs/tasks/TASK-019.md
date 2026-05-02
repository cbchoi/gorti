# TASK-019: CodecFor(model.DataType) dispatcher; M1 exit

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-009, TASK-014, TASK-015, TASK-016, TASK-017, TASK-018 |
| Blocks | TASK-020, TASK-024, TASK-050, TASK-060 |

## Goal

Wire `encoding.CodecFor(dt model.DataType) (Codec, error)` so the orchestrator's `TestSpec_M1_CompositeVectorsRoundTrip` un-skips and passes for all composite vectors. This is the **M1 exit task**.

## Scope (in)

- Modify `rti/pkg/encoding/codec.go` (M0 stub): replace `ErrNotImplemented` body of `CodecFor` with a switch over `model.DataType` variants → constructs the right codec from TASK-010..018.
- Create `rti/pkg/encoding/dispatch.go` with the type-mapping logic (so `codec.go` stays small and the M0 frozen-shape interface is preserved).
- Confirm coverage thresholds: `pkg/encoding` ≥ 85%, `pkg/fom` ≥ 75%.

## Scope (out)

- Adding new codec types — should be done in TASK-010..018 first.
- Optimization — explicitly forbidden per `docs/agent-b-fom-encoding.md` §7.

## Implements

- Requirements: FR-ENC-1, FR-ENC-2.
- Spec tests: `tests/spec/M1/encoding_vectors_test.go::TestSpec_M1_CompositeVectorsRoundTrip`.

## TDD entry point

- Start with: `TestSpec_M1_CompositeVectorsRoundTrip` (currently `t.Skip`); will un-skip once `CodecFor` returns non-nil for at least one composite vector.

## Acceptance criteria

- [ ] All composite vectors in `tests/conformance/encoding_vectors.json` round-trip green.
- [ ] All M1 spec tests in `tests/spec/M1/` pass.
- [ ] Coverage gates met: `pkg/encoding` ≥ 85%, `pkg/fom` ≥ 75%.
- [ ] `golangci-lint` clean; no panics reachable from public API.
- [ ] `make verify` green.

## Notes / hints

- This is the **M1 milestone gate task** — passing it triggers `verification:M1` activities by Agent A and Agent C (see briefs §5).
- `model.DataType` is a sum type from `rti/pkg/fom/model/dataclass.go` (TASK-001).
