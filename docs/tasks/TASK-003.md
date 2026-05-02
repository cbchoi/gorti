# TASK-003: Diagnostic FOM-001 (undefined dataType reference)

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

Detect attribute and parameter references to undeclared dataTypes; emit `Diagnostic{Code: "FOM-001", ...}` carrying the offending source line. Reject the FOM (return `Result{FOM: nil, Diagnostics: [...]}`).

## Scope (in)

- Create `rti/pkg/fom/parser/datatype_ref.go`: a ref-resolution pass over the parsed model.
- Wire the pass into `parser.Parse` after structural parsing, before returning the result.

## Scope (out)

- Other diagnostics — separate tasks.

## Implements

- Requirements: FR-FOM-1 (rejection path).
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-001` (driven by `tests/conformance/foms/bad/FOM-001-undefined-datatype.xml`).

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-001`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] `Result.HasCode("FOM-001")` true on the bad fixture; `res.FOM == nil` on rejection.
- [ ] Coverage on `rti/pkg/fom/parser` ≥ 70%.
- [ ] `make verify` green.

## Notes / hints

- `docs/idd.md` §1.2.1 lists the canonical diagnostic message format.
- Diagnostic carries `ModulePath` and `Line` so users can locate the offense.
