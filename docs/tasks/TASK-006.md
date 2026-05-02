# TASK-006: Diagnostic FOM-009 (strict-mode unknown XML element/attribute)

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

Reject FOMs that contain XML elements or attributes not in the 1516.2-2010 DIF (Annex A). Strict mode: anything not on the whitelist → `FOM-009`. No "permissive" fallback.

## Scope (in)

- Create `rti/pkg/fom/parser/strict.go`: a whitelist-based check during XML walk that emits `FOM-009` for unrecognized element/attribute names.
- Wire into `parser.Parse`.

## Scope (out)

- Other diagnostics.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-009` (driven by `tests/conformance/foms/bad/FOM-009-unknown-element.xml`).

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-009`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] Whitelist is in source (not external resource); enumerated from Annex A.
- [ ] `make verify` green.

## Notes / hints

- Per `docs/agent-b-fom-encoding.md` §7 anti-goals: "Do not invent FOM extensions or 'convenience' data types not in the spec." The whitelist must mirror Annex A exactly.
