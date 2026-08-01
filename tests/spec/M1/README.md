# Core Specification Tests

Specification tests for the FOM parser, MIM, and encoding rules.

These tests encode the **component contract**:

- They are written test-first.
- Tests may be **added**, but existing assertions must not be weakened or removed.
- All tests in this directory must be GREEN.

## Layout

- `parser_diagnostics_test.go` — exercises `rti/pkg/fom/parser` against canned good and bad FOMs at `tests/conformance/foms/`.
- `encoding_vectors_test.go` — exercises `rti/pkg/encoding` against the golden vectors at `tests/conformance/encoding_vectors.json`.

## Running

```bash
go test ./tests/spec/M1/...
```

A stub implementation that returns `ErrNotImplemented` does not satisfy these tests. Implementations should make each test green incrementally.

## Extending

Add **per-package** TDD tests under `rti/pkg/fom/parser/*_test.go` and `rti/pkg/encoding/*_test.go`. Those tests cover detailed unit behavior; the spec tests here cover the component contract. Both sets must be green.

## Related

- `engineering/specifications/current/SRS.md` — FR-FOM-* and FR-ENC-* requirements.
- `engineering/specifications/current/IDD.md` — current language and wire profiles.
- `engineering/specifications/current/STD.md` — acceptance and traceability rules.
- `tests/conformance/encoding_vectors.json` — the golden vector set.
- `tests/conformance/foms/` — canned good and bad FOM fixtures.
