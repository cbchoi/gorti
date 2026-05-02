# TASK-007: Diagnostic FOM-011 (object class references non-existent parent)

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

Reject FOMs whose object class declares a `parentName` that does not resolve to any other declared class (after MIM merge once MIM lands). Emit `FOM-011`.

## Scope (in)

- Create `rti/pkg/fom/parser/parent.go`: check each object class's `parentName` against the set of known class names.
- Wire into `parser.Parse`.

## Scope (out)

- Interaction-class parent missing — TASK-088.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-011` (driven by `tests/conformance/foms/bad/FOM-011-missing-parent-class.xml`).

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-011`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] `make verify` green.

## Notes / hints

- The MIM defines `HLAobjectRoot` as the universal ancestor; once TASK-008 lands, the resolution pass sees MIM names too.
