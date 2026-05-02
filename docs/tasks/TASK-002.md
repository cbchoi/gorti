# TASK-002: Parser handles interactions + parameters; pyjevsim-bridge.xml accepts

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-001 |
| Blocks | TASK-009, TASK-073, TASK-087, TASK-088 |

## Goal

Extend the parser so `tests/conformance/foms/good/pyjevsim-bridge.xml` parses cleanly with zero diagnostics. The bridge FOM contains interactions and parameters — features beyond `good/minimal.xml`.

## Scope (in)

- Create `rti/pkg/fom/parser/interaction.go`: parsing logic for `<interactions>`, `<interactionClass>`, `<parameter>`.
- Wire interaction parsing into `parser.Parse`'s top-level walk.
- Populate `model.FOM.InteractionClasses` deterministically (name-sorted iteration).

## Scope (out)

- Diagnostic detection on interactions (FOM-005, FOM-012) — TASK-087, TASK-088.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_ParsePyjevsimBridgeFOM_NoDiagnostics`.

## TDD entry point

- Start with: `TestSpec_M1_ParsePyjevsimBridgeFOM_NoDiagnostics`.

## Acceptance criteria

- [ ] Spec test above passes.
- [ ] `go test ./rti/pkg/fom/...` green.
- [ ] Coverage on `rti/pkg/fom/parser` ≥ 65%.
- [ ] `make verify` green.

## Notes / hints

- The bridge FOM is also Agent C's M4 example reference (`docs/agent-c-pysdk.md` §4.4); do not modify the file — it is producer-consumer per `docs/ORTHOGONALITY.md` §3.
