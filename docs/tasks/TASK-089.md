# TASK-089: Diagnostic FOM-013 (variant record without discriminator field)

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

Reject FOMs declaring a `<variantRecordData>` without a discriminator field. Emit `FOM-013` at parse time (this is FOM-side validation, distinct from encoding-time variant validation in TASK-017).

## Scope (in)

- Create `rti/pkg/fom/parser/variant_discriminator.go`: structural check on `<variantRecordData>` declarations.
- Wire into `parser.Parse`.

## Scope (out)

- Encoding-time variant errors — TASK-017's responsibility.

## Implements

- Requirements: FR-FOM-1.
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_BadFOMDiagnostics/FOM-013`.

## TDD entry point

- Start with: `TestSpec_M1_BadFOMDiagnostics/FOM-013`.

## Acceptance criteria

- [ ] Spec test passes.
- [ ] `make verify` green.

## Notes / hints

- **Pre-dispatch prerequisite:** orchestrator first adds `tests/conformance/foms/bad/FOM-013-variant-no-discriminator.xml` and extends the spec test.
