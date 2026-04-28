# Agent B Brief — FOM + Encoding (codex-sandbox)

**Pre-reading required**: `docs/AGENTS.md`, `docs/srs.md`. Do not start work until you've read both.

---

## 1. Your Role

You own the **data layer**: parsing and validating FOMs (1516-2010 DIF XML), embedding the standard MIM, and implementing the HLA Evolved binary encoding rules (1516.2-2010 §4).

Your work is the foundation for both Agent A (RTI core uses your encoding for the wire) and Agent C (Python encoder must produce byte-identical output to yours). Both depend on your **golden encoding vectors**.

This component is well-scoped, spec-dense, and table-driven — perfect fit for systematic implementation. **Quality bar: byte-perfect.** A 1-bit divergence breaks cross-language interop.

## 2. Owned Paths (you may write here)

- `rti/pkg/fom/parser/` — XML → immutable FOM model.
- `rti/pkg/fom/mim/` — embedded standard MIM, merge logic.
- `rti/pkg/fom/model/` — FOM data structures (object class trees, interaction trees, dataType registry).
- `rti/pkg/encoding/` — HLA Evolved binary codec.
- `tests/conformance/encoding_vectors.json` — golden vectors (you generate; Agent C must match).
- `tests/conformance/foms/` — reference FOMs (good + bad).
- Tests in any of the above.

## 3. Forbidden Paths

You may **read** but never **write**:

- `proto/**` (frozen)
- `rti/internal/**` (Agent A owns; you don't depend on it — your packages are pure)
- `pysdk/**` (Agent C owns)
- `docs/**`, `.github/**`

Your packages must be **importable as a library**. They take no dependency on `rti/internal/*`. If you find you need state from elsewhere, redesign — your code is pure transformation.

## 4. Milestone Deliverables

### M1 — FOM Parser, MIM, Encoding Rules

Implements: **FR-FOM-1..4, FR-ENC-1..2.**

#### `rti/pkg/fom/parser/`

- Strict 1516-2010 DIF XML parser (`encoding/xml` is fine; consider streaming for large FOMs).
- Validation rules (each gets a numbered diagnostic, e.g. `FOM-001: dataType X referenced but not defined`):
  - All referenced `dataType` names exist.
  - Class hierarchy is a tree (no cycles, single parent).
  - Attribute names unique within a class (including inherited).
  - Interaction parameters unique within a class.
  - Required encoding rules / order rules / transportation rules use only valid identifiers from the spec.
  - No unknown elements/attributes (strict — reject anything not in DIF).
- Output: immutable `model.FOM` value.

#### `rti/pkg/fom/mim/`

- The standard MIM and `HLAstandardMIM` are embedded via `//go:embed standard-mim.xml` and `//go:embed hla-standard-mim.xml`. Source files come from the IEEE/SISO published versions; commit them with provenance comment in the package doc.
- `Merge(base FOM, user FOM) (FOM, error)` — for cut 1, "merge" means: load MIM, then load user's single module on top; reject any user module that redefines MIM types/classes. Multi-module merge is cut 2.

#### `rti/pkg/fom/model/`

- `FOM` (root), `ObjectClass`, `Attribute`, `InteractionClass`, `Parameter`, `DataType` (sum type: `BasicData`, `SimpleData`, `EnumeratedData`, `ArrayData`, `FixedRecordData`, `VariantRecordData`).
- All fields exported but immutable after construction (constructors take all fields, no setters).
- Deterministic ordering: when iterating, sort by name (stable, locale-independent).

#### `rti/pkg/encoding/`

Implement encoders/decoders for every HLA Evolved type per IEEE 1516.2-2010 §4:

- Primitives: `HLAinteger16BE/LE`, `HLAinteger32BE/LE`, `HLAinteger64BE/LE`, `HLAfloat32BE/LE`, `HLAfloat64BE/LE`, `HLAoctet`, `HLAoctetPairBE/LE`, `HLAboolean`, `HLAunicodeChar`, `HLAASCIIchar`, `HLAASCIIstring`, `HLAunicodeString`.
- Composite: `HLAfixedArray`, `HLAvariableArray`, `HLAfixedRecord`, `HLAvariantRecord`, `HLAopaqueData`.
- Each type has: `Encode(value) ([]byte, error)`, `Decode([]byte) (value, n int, error)`. n = bytes consumed (for nested decoding).
- Padding/alignment per the spec — get this right; it's the most common bug.

Public API shape (illustrative, refine in your PR):
```go
package encoding

type Codec interface {
    Encode(v any) ([]byte, error)
    Decode(b []byte) (v any, n int, err error)
    OctetBoundary() int
}

func CodecFor(dt model.DataType) (Codec, error)
```

#### `tests/conformance/encoding_vectors.json`

- For every primitive and composite type, generate golden vectors covering: zero, one, max, min, typical, edge cases (UTF-8 multi-byte for strings, NaN/Inf for floats, nested records).
- Format: `{"type": "...", "value": <json>, "bytes": "<hex>"}`. Stable JSON formatting (sorted keys, no trailing whitespace).
- Both Agent A's RTI and Agent C's Python encoder consume this file.

**M1 exit criteria** (objective, testable):

1. Parser strict-rejects 10 canned malformed FOMs in `tests/conformance/foms/bad/`.
2. Parser accepts the reference good FOM in `tests/conformance/foms/good/minimal.xml` and the pyjevsim-style FOM in `tests/conformance/foms/good/pyjevsim-bridge.xml`.
3. Encoder round-trips: for every type, `Decode(Encode(v)) == v`.
4. Golden vectors: `go test ./tests/conformance/...` passes (≥3 vectors per type).
5. `go test ./rti/pkg/...` green; coverage ≥85% on `pkg/encoding`, ≥75% on `pkg/fom`.
6. `golangci-lint` clean; no panics in code paths reachable from public API.

### M5 — End-to-end (you contribute)

- If perf baseline (Agent A) reveals encoding hotspots, add benchmarks (`go test -bench`) and propose optimizations — but do not optimize speculatively.
- Audit your code one final time for determinism: any map iteration without sorted keys, any reliance on `time.Now()`, etc. File issues for any you find anywhere in the repo.

## 5. Verification Responsibilities (at OTHER agents' gates)

### At M2 gate (Agent A's milestone)

- Write a fuzzer (`go-fuzz` or built-in `testing.F`) that throws malformed gRPC messages and malformed FOMs at the live RTI from Agent A. Expect:
  - No crashes (no panics out of handlers).
  - All errors carry proper error codes from `proto/rti/v1/errors.proto`.
  - Error messages identify the offending federate/message.
- File `verification:M2` issue with reproduction cases for any failure.

### At M3 gate (Agent A's milestone)

- Generate 20 randomized 3-federate time-management scenarios (varying lookaheads, message timestamps within lookahead, join order). Run each through Agent A's RTI; assert determinism (same input → same event log) across multiple iterations.
- File `verification:M3` issue with the harness, scenario generator, and any non-determinism findings.

### At M4 gate (Agent C's milestone)

- Run all golden encoding vectors against both Agent C's Python encoder/decoder AND your Go encoder/decoder. Byte-diff every vector.
- File `verification:M4` issue with diff results (should be 0).

## 5.5 TDD Patterns for Your Domain

Read `docs/TDD.md` first. Your component is a near-perfect TDD fit — pure functions, table-driven, spec-dense.

### FOM parser & validation
Every `FOM-NNN` diagnostic owns at least one **bad-FOM** fixture and one **positive** accept fixture. Cycle:

1. Add `tests/conformance/foms/bad/FOM-NNN-<short>.xml` (FOM that should be rejected).
2. Add a spec test that parses it and asserts `FOM-NNN` is reported with the offending line.
3. Run — fails (parser doesn't yet detect this).
4. Implement the validation rule — passes.
5. Refactor.

Property test: parse a good FOM, re-emit canonical XML, parse again — assert `model.FOM` equality. Catches loss-of-information bugs in the model.

### Encoding rules
Strict TDD with golden vectors:

1. Pick a type (e.g. `HLAfixedRecord{a: HLAinteger32BE, b: HLAfloat64BE}`).
2. Compute expected bytes from the spec by hand. Add the vector to `tests/conformance/encoding_vectors.json`.
3. `TestConformance_Encoding` is now red.
4. Implement encode + decode until vector passes.
5. Add property test: for generated `v`, `decode(encode(v)) == v`.
6. Refactor padding helpers, etc.

**Padding tests must be explicit.** Don't trust round-trip alone. Write a vector where alignment differs from naïve concatenation; if your encoder gets it wrong, the byte-diff catches it immediately.

### MIM embedding
- Property test: embedded `standard-mim.xml` parses cleanly with zero diagnostics.
- Spec test: a user FOM that redefines an MIM type produces `FOM-101` (red first; then implement the merge guard).

The orchestrator pre-writes specification tests for M1 under `tests/spec/M1/` covering the parser diagnostics and a representative encoding vector set. You cannot weaken them. Your own table-driven tests fill in detail (every type, every diagnostic).

## 6. Spec Pointers (IEEE 1516)

- FOM XML schema — IEEE 1516.2-2010 Annex A (the DIF).
- OMT data types — IEEE 1516.2-2010 §6.
- Encoding rules — IEEE 1516.2-2010 §4 (read this **multiple times**; alignment rules are subtle).
- Standard MIM — IEEE 1516.2-2010 Annex B.
- `HLAstandardMIM` — IEEE 1516.1-2010 §4.13.

When in doubt about an encoding edge case, write a test, look at how Portico encodes it, and document the resolution in your PR description.

## 7. Anti-Goals (Specific to You)

- Do not implement encoding rules other than HLA Evolved (no 1516-2000 "1.3" encoding).
- Do not depend on `rti/internal/*` from `pkg/`. Your packages are pure libraries.
- Do not invent FOM extensions or "convenience" data types not in the spec.
- Do not skip the standard MIM because it's tedious. The full MIM must be embedded and loaded.
- Do not optimize the encoder before Agent A's M5 baseline shows it's a hotspot. Premature optimization wastes time.
- Do not generate golden vectors with floating-point values that have ambiguous representations (use exactly-representable doubles like 0.5, 1.25, etc., for cross-language byte equality).

## 8. When to Stop and Ask

- Any time the encoding spec is ambiguous and you can't find a reference test vector.
- Any time you find a divergence between the spec and Portico's behavior — both interpretations need orchestrator decision.
- Any time you'd need to add a field to the proto or `core` interfaces.
