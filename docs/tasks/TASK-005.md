# TASK-005: Diagnostic FOM-004 (duplicate attribute within class incl. inherited)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-001 |
| Blocks | none |

## Goal

Reject FOMs where an attribute name appears twice in the same object class — directly declared or inherited from an ancestor — with diagnostic `FOM-004`.

## Scope (in)

- Create `rti/pkg/fom/parser/duplicate.go`: walk each class, accumulate attributes from the inheritance chain, detect collisions.
- Wire into `parser.Parse` after structural parsing.

## Scope (out)

- Duplicate parameters in interactions — TASK-087.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-004` (driven by `tests/conformance/foms/bad/FOM-004-duplicate-attribute.xml`).

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-004`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] Coverage on `rti/pkg/fom/parser` does not regress (≥ previous task's level).
- [ ] `make verify` green.

## Notes / hints

- Inherited attributes count: a child may not redeclare a name already on an ancestor.
