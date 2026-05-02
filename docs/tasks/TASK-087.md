# TASK-087: Diagnostic FOM-005 (interaction parameter duplicated within class)

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

Reject FOMs where an interaction class declares duplicate parameter names — directly or via inheritance. Emit `FOM-005`. Symmetric to TASK-005 but for interactions.

## Scope (in)

- Create `rti/pkg/fom/parser/duplicate_parameter.go`: walk each interaction class, accumulate parameters from ancestors, detect collisions.
- Wire into `parser.Parse`.

## Scope (out)

- Object-class attribute duplicates — TASK-005.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-005`.

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-005`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] `make verify` green.

## Notes / hints

- **Pre-dispatch prerequisite:** orchestrator first adds `tests/conformance/foms/bad/FOM-005-duplicate-parameter.xml` and extends the spec test (frozen, orchestrator-only).
