# M1 Specification Tests

Orchestrator-written specification tests for milestone M1 (Agent B owner: FOM parser, MIM, encoding rules).

These tests encode the **milestone contract**. Per `docs/TDD.md` §5:

- They are written test-first (committed RED before M1 starts).
- Agent B may **add** tests here but may **not** weaken or remove existing assertions.
- All tests in this directory must be GREEN to advance from M1.

This directory is in the frozen-paths list (see `docs/AGENTS.md` §4); modifications by agents trigger an automatic `verification:contract` issue.

## Layout

- `parser_diagnostics_test.go` — exercises `rti/pkg/fom/parser` against canned good and bad FOMs at `tests/conformance/foms/`.
- `encoding_vectors_test.go` — exercises `rti/pkg/encoding` against the golden vectors at `tests/conformance/encoding_vectors.json`.

## Running

```bash
go test ./tests/spec/M1/...
```

Initially RED (stubs return `ErrNotImplemented`). Agent B turns each test green incrementally during M1 work.

## Extending

Agent B is expected to add **per-package** TDD tests under their owned paths (`rti/pkg/fom/parser/*_test.go`, `rti/pkg/encoding/*_test.go`). Those tests cover detailed unit behavior; the spec tests here cover the milestone contract. Both must be green at the M1 gate.

## Related

- `docs/srs.md` §5.5, §5.6 — FR-FOM-* and FR-ENC-* requirements.
- `docs/idd.md` §1.2 — numbered FOM diagnostics (`FOM-001` ... `FOM-101`).
- `docs/agent-b-fom-encoding.md` §5.5 — TDD patterns for this work.
- `tests/conformance/encoding_vectors.json` — the golden vector set.
- `tests/conformance/foms/` — canned good and bad FOM fixtures.
