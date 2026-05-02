# TASK-009: Merge(base, user) + diagnostic FOM-101 (user redefines MIM type)

| Field | Value |
|---|---|
| Status | BLOCKED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-05-02 |
| Depends-on | TASK-002, TASK-008 (transitively blocked by issue #1) |
| Blocks | none |

## Goal

Implement `Merge(base FOM, user FOM) (FOM, error)` for cut 1: load MIM, then load the user's single module on top; reject any user module that redefines an MIM type or class with diagnostic `FOM-101`.

## Scope (in)

- Create `rti/pkg/fom/mim/merge.go`.
- Detect collisions between user-declared names and MIM names → emit `FOM-101`.

## Scope (out)

- Multi-module merge across two user modules — cut 2.

## Implements

- Requirements: FR-FOM-3, FR-FOM-4.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-101` (driven by `tests/conformance/foms/bad/FOM-101-redefines-mim-type.xml`).

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-101`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] `go test ./rti/pkg/fom/...` green.
- [ ] Coverage on `rti/pkg/fom` ≥ 75%.
- [ ] `make verify` green.

## Notes / hints

- "Redefines" means: any name (object class, interaction class, dataType) the MIM declares that the user also declares.

## Blocked (2026-05-02)

Transitively blocked by [issue #1](https://github.com/cbchoi/gorti/issues/1) via TASK-008. The FOM-101 collision check compares user-declared names against MIM names; running it on placeholder MIM content gives false positives/negatives. Wait for TASK-008's MIM sourcing to land on `main`.
