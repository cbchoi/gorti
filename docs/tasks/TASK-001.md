# TASK-001: Parser skeleton + good/minimal.xml accepts cleanly

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | none |
| Blocks | TASK-002, TASK-003, TASK-004, TASK-005, TASK-006, TASK-007, TASK-008, TASK-009, TASK-019, TASK-061, TASK-086, TASK-089 |

## Goal

Replace the M0 `rti/pkg/fom/parser/parser.go` stub with a working skeleton that parses a minimal valid 1516-2010 DIF XML FOM and returns a non-nil `model.FOM` with zero diagnostics. Build out the `model` package's core types so the parser has something to populate.

## Scope (in)

- Modify `rti/pkg/fom/parser/parser.go`: implement `Parse([]Module) (Result, error)` for the happy path on `tests/conformance/foms/good/minimal.xml`.
- Create `rti/pkg/fom/model/fom.go`: root `FOM` struct with constructor `NewFOM`; immutable after construction (no setters); deterministic name-sorted iteration when listing object classes / interaction classes / dataTypes.
- Create `rti/pkg/fom/model/dataclass.go`: `DataType` sum (`BasicData`, `SimpleData`, `EnumeratedData`, `ArrayData`, `FixedRecordData`, `VariantRecordData`).
- Create `rti/pkg/fom/model/dataclass_test.go`: round-trip property test (build a FOM → emit canonical XML → reparse → assert equality).

## Scope (out)

- Diagnostic detection (FOM-001..101) — those are TASK-003..009 and TASK-086..089.
- MIM embedding/merge — TASK-008/009.
- Parser support for interactions/parameters in `good/pyjevsim-bridge.xml` — TASK-002.

## Implements

- Requirements: FR-FOM-1 (acceptance path).
- Spec tests: `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics`.

## TDD entry point

- Start with: `TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics` — currently fails because parser returns `ErrNotImplemented`. This is the first red test. From there add unit tests under `rti/pkg/fom/parser/*_test.go` test-first per `docs/TDD.md`.

## Acceptance criteria

- [ ] `tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics` passes.
- [ ] `go test ./rti/pkg/fom/...` is green.
- [ ] Coverage on `rti/pkg/fom/parser` ≥ 60% (will rise as later tasks add diagnostics).
- [ ] PR follows TDD commit pattern A or C per `docs/TDD.md` §3.
- [ ] `make verify` green locally before opening the PR.

## Notes / hints

- IEEE 1516.2-2010 Annex A for the DIF XML schema.
- The `parser.Parse` signature is frozen (see `rti/pkg/fom/parser/parser.go` from M0); do not change `Module`, `Result`, or `Diagnostic` types — they are part of the M0 contract per `docs/ORTHOGONALITY.md` §3.
- Use `encoding/xml` for cut 1; streaming optimization is a M5 conditional decision.
