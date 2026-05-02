# TASK-086: Diagnostic FOM-003 (object class has multiple parents)

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

Reject FOMs where an object class declares more than one parent class. Emit `FOM-003`.

## Scope (in)

- Create `rti/pkg/fom/parser/multiple_parents.go`: structural check during parse for `<objectClass>` elements that declare more than one parent reference.
- Wire into `parser.Parse`.

## Scope (out)

- Cycle detection — TASK-004.
- Missing parent — TASK-007.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-003`.

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-003`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] Coverage on `rti/pkg/fom/parser` not regressed.
- [ ] `make verify` green.

## Notes / hints

- **Pre-dispatch prerequisite:** orchestrator must first add `tests/conformance/foms/bad/FOM-003-multiple-parents.xml` and extend `tests/spec/M1/parser_diagnostics_test.go` to include a `FOM-003` case (frozen-path edits, orchestrator-only). Until those land on `main`, this task has no driving spec test.
- The diagnostic message must identify the offending class name and its conflicting parents.
