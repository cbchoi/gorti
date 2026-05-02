# TASK-004: Diagnostic FOM-002 (cyclic class hierarchy)

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

Detect cycles in object-class parent chains (and interaction-class parent chains, once interactions are parsed in TASK-002 — but the diagnostic for interactions is TASK-088); emit `FOM-002` with the offending class name.

## Scope (in)

- Create `rti/pkg/fom/parser/cycle.go`: DFS over the parent graph with a visited stack; on back-edge, emit `FOM-002`.
- Wire into `parser.Parse` after structural parsing.

## Scope (out)

- Missing-parent detection — TASK-007.
- Multiple-parents detection — TASK-086.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-002` (driven by `tests/conformance/foms/bad/FOM-002-cyclic-class-hierarchy.xml`).

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-002`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] Coverage on `rti/pkg/fom/parser` ≥ 70%.
- [ ] `make verify` green.

## Notes / hints

- A cycle of any length (self-loop, A→B→A, etc.) must be rejected.
