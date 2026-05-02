# TASK-008: Embed standard MIM XML; MIM parses with zero diagnostics

| Field | Value |
|---|---|
| Status | BLOCKED |
| Assignee | agent-b |
| Milestone | M1 |
| Created | 2026-04-29 |
| Updated | 2026-05-02 |
| Depends-on | TASK-001, **issue #1 (canonical MIM XML sourcing)** |
| Blocks | TASK-009 |

## Goal

Embed the IEEE/SISO standard MIM and `HLAstandardMIM` XML files into the `rti/pkg/fom/mim/` package via `//go:embed`. Confirm both parse cleanly through Agent B's parser (zero diagnostics).

## Scope (in)

- Create `rti/pkg/fom/mim/embed.go` with `//go:embed` directives and a provenance comment naming the source URL/version + retrieval date.
- Add `rti/pkg/fom/mim/standard-mim.xml` (1516.2-2010 Annex B).
- Add `rti/pkg/fom/mim/hla-standard-mim.xml` (1516.1-2010 §4.13).
- Add a property test (`rti/pkg/fom/mim/embed_test.go`): both embedded MIMs parse through `parser.Parse` with zero diagnostics.

## Scope (out)

- Merge logic — TASK-009.

## Implements

- Requirements: FR-FOM-2, FR-FOM-3.

## TDD entry point

- Start with: a unit test in `rti/pkg/fom/mim/embed_test.go` asserting `Parse(embeddedMIM)` returns zero diagnostics. Currently red because parser is incomplete (TASK-001 brings it to a state where the standard MIM should parse).

## Acceptance criteria

- [ ] `go test ./rti/pkg/fom/mim/...` green.
- [ ] Provenance comment present in `embed.go` (URL + version + date retrieved).
- [ ] `make verify` green.

## Notes / hints

- The full MIM must be embedded — do not skip pieces because they're tedious (`docs/agent-b-fom-encoding.md` §7).
- The embedded XML files are not in `tests/conformance/foms/`; they live in the package itself.

## Blocked (2026-05-02)

Status moved to BLOCKED pending resolution of [issue #1](https://github.com/cbchoi/gorti/issues/1) (`contract-change-request: source real IEEE 1516.2-2010 Annex B / 1516.1-2010 §4.13 MIM XML`). The orchestrator must commit canonical MIM XML to `main` first; otherwise this task lands on placeholder content that violates `docs/srs.md` §1 standard-conformance and breaks cross-RTI portability. Agent B is idle on this task — see `docs/DISPATCH.md` §5 idle protocol.
