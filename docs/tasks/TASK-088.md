# TASK-088: Diagnostic FOM-012 (interaction class references non-existent parent)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-002 |
| Blocks | none |

## Goal

Reject FOMs whose interaction class declares a `parentName` that does not resolve. Symmetric to TASK-007 but for interactions. Emit `FOM-012`.

## Scope (in)

- Create `rti/pkg/fom/parser/interaction_parent.go`.
- Wire into `parser.Parse`.

## Scope (out)

- Object-class missing parent — TASK-007.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-012`.

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-012`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] `make verify` green.

## Notes / hints

- **Pre-dispatch prerequisite:** orchestrator first adds `tests/conformance/foms/bad/FOM-012-missing-interaction-parent.xml` and extends the spec test.
- The MIM defines `HLAinteractionRoot` as universal interaction ancestor; once TASK-008 lands, MIM names are visible to the resolution pass.
