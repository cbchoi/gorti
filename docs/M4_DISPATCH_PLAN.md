# M4 Dispatch Plan

How the orchestrator dispatches the 26 M4 tasks (TASK-050..075) to maximize parallel sub-agent throughput while keeping every wave orthogonal at the file level. Mirrors the M2/M3 wave pattern but at larger scale (M2: 4 waves; M3: 4 waves; **M4: 7 waves**).

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md` (general protocol), `docs/agent-c-pysdk.md` (Agent C brief), `docs/M2_DISPATCH_PLAN.md` + `docs/M3_DISPATCH_PLAN.md` (the two waves we mirror), `docs/MILESTONE_CHECK.md` (probe definitions), `docs/srs.md` §10.2 (M4 exit criteria).

---

## 1. Why a wave model

M4 has 26 tasks and is the project's biggest milestone by task count. All assigned to Agent C in `docs/agent-c-pysdk.md`. Naive serial dispatch would take ~26 sub-agent rounds; the wave model cuts critical path to ~7. Parallel fan-out within each wave is bounded by the file-ownership decomposition below.

The same pattern that drove M3 to DONE in 4 waves of 5 sub-agents (~40 min wall-time) drove M2 in 4 waves of 9 sub-agents (~30 min). M4 should land in ~50–70 min wall-time given its larger scope.

## 2. Pre-work confirmation (this commit)

Before Wave 1 dispatches, the orchestrator delivers:

- **`pysdk/`** package skeleton: `pyproject.toml` (deps + tool config), `README.md`, `.gitignore`, `conftest.py` references via spec dir
- **Frozen-shape stubs** under `pysdk/rti1516e/` and `pysdk/pyjevsim_bridge/` — every public class/function has its signature + docstring; bodies raise `NotImplementedError` with a TASK-NNN reference
- **Spec tests** under `pysdk/tests/spec/m4/` — 13 test files covering encoding conformance, FOM diagnostics, SDK Layer 1+2, pyjevsim bridge, plus 2 skip-scaffolds for the M4 gate (determinism + replay)
- **Test doubles** under `pysdk/tests/spec/m4/_fakes/`: `FakeRtiServer`, `StubCoupledModel`, `vector_loader`
- **Makefile** targets: `py-codegen`, `py-test`, `py-lint`, `py-typecheck`
- **Frozen-paths check** updated: `pysdk/tests/spec/` now appears in the disallow list (Agent C may not weaken)
- **Milestone probe** updated: `scripts/check-milestones.sh` M4 looks at `pysdk/tests/spec/m4/`

Pre-dispatch state: `make py-typecheck` passes (stubs are mypy --strict clean), `pytest pysdk/tests/spec/m4/ --collect-only` collects all tests, running them yields RED with `NotImplementedError` for the right reason. Go side stays green.

## 3. Wave structure

```
                 (M0..M3 already DONE; orchestrator pre-work landed)
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 1 (5 parallel sub-agents — encoding primitives + FOM model)   │
   │   W1A — encoding/integer.py        (TASK-050)                      │
   │   W1B — encoding/float_codec.py    (TASK-051)                      │
   │   W1C — encoding/byte_codec.py     (TASK-052)                      │
   │   W1D — encoding/opaque.py         (TASK-058)                      │
   │   W1E — fom/model.py + _proto.py   (TASK-060 + TASK-062 bundle)    │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 2 (4 parallel sub-agents — strings + composites + FOM parser) │
   │   W2A — encoding/string_codec.py        (TASK-053)                 │
   │   W2B — encoding/{fixed,variable}_array.py (TASK-054 + TASK-055)   │
   │   W2C — encoding/{fixed,variant}_record.py (TASK-056 + TASK-057)   │
   │   W2D — fom/parser.py                   (TASK-061)                 │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 3 (1 sub-agent — encoding gate)                               │
   │   W3 — encoding/dispatch.py             (TASK-059; full conformance) │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 4 (1 sub-agent — full SDK Layer 1 + 2)                        │
   │   W4 — connection.py + declaration.py + object.py + interaction.py │
   │        + events.py + standard.py                                   │
   │        (TASK-063 + 064 + 065 + 066 + 067 + 068 bundle)             │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 5 (2 parallel sub-agents — bridge basics + smoke)             │
   │   W5A — pyjevsim_bridge/port_mapping.py     (TASK-069)             │
   │   W5B — tests/test_pyjevsim_smoke.py-equiv (TASK-072)              │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 6 (1 sub-agent — bridge core)                                 │
   │   W6 — time_advance.py + select_preserve.py                        │
   │        (TASK-070 + TASK-071 bundle)                                │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 7 (1 sub-agent — examples + harness + M4 gate)                │
   │   W7 — examples/pyjevsim/* + determinism harness + lint/coverage   │
   │        (TASK-073 + TASK-074 + TASK-075)                            │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
                    M4 DONE per srs.md §10.2
```

Critical path: 7 waves. With per-sub-agent compute ~5–15 min, M4 wall-time is roughly 50–80 min vs. ~2.5h for strict serial dispatch. Plus orchestrator merge/verify cycles.

## 4. File ownership per wave

The TDD-friendly file decomposition. Every sub-agent's owned-files set is disjoint from every other's in the same wave; cross-wave shared files (only the M0 stubs the orchestrator seeded) are EXTENDED, not RESHAPED.

### Wave 1 — encoding primitives + FOM model + codegen

| Sub-agent | Tasks | Owned files | Spec tests turned green |
|---|---|---|---|
| **W1A** integer codecs | TASK-050 | `pysdk/rti1516e/encoding/integer.py` (extend body of 6 codec classes) | conformance vectors with `type` ∈ {HLAinteger16BE/LE, HLAinteger32BE/LE, HLAinteger64BE/LE} — partial GREEN of `test_spec_m4_vector_round_trip` (full pass blocked on W3's dispatcher) |
| **W1B** float codecs | TASK-051 | `pysdk/rti1516e/encoding/float_codec.py` | float vectors |
| **W1C** byte codecs | TASK-052 | `pysdk/rti1516e/encoding/byte_codec.py` | octet/octetPair/boolean/ASCIIchar/unicodeChar vectors |
| **W1D** opaque codec | TASK-058 | `pysdk/rti1516e/encoding/opaque.py` | HLAopaqueData vectors |
| **W1E** FOM model + codegen | TASK-060 + TASK-062 | `pysdk/rti1516e/fom/model.py` (extend), `pysdk/rti1516e/_proto.py` (NEW), `pysdk/Makefile.codegen` (NEW). Also: regenerate `pysdk/rti1516e/_generated/` (gitignored output, not committed) | none yet — these unblock W2 + W4 |

Five sub-agents in parallel. Zero file collisions.

**TASK-050 amendment (orchestrator note)**: TASK-050's "Scope (in)" originally bundled package bootstrap (pyproject.toml, README, dirs, `__init__.py`s, `dispatch.py` skeleton) with the integer codec implementation. The orchestrator pre-work has done the bootstrap; W1A's scope is purely the integer codec body.

### Wave 2 — strings + composites + FOM parser

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W2A** strings | TASK-053 | `pysdk/rti1516e/encoding/string_codec.py` | W1C (byte codecs share encoding patterns) |
| **W2B** array composites | TASK-054 + TASK-055 | `pysdk/rti1516e/encoding/fixed_array.py`, `variable_array.py` | W1A..C (element codecs) |
| **W2C** record composites | TASK-056 + TASK-057 | `pysdk/rti1516e/encoding/fixed_record.py`, `variant_record.py` | W1A..C (field codecs) |
| **W2D** FOM parser | TASK-061 | `pysdk/rti1516e/fom/parser.py` | W1E (FOM dataclass model) |

Four sub-agents in parallel. Zero file collisions.

### Wave 3 — encoding gate

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W3** dispatcher + conformance gate | TASK-059 | `pysdk/rti1516e/encoding/dispatch.py` |

Single sub-agent. **Unblocks the entire SDK + bridge.** This is M4's first internal gate: when `pytest pysdk/tests/spec/m4/test_spec_m4_encoding_conformance.py` is fully GREEN, encoding is done.

### Wave 4 — SDK Layer 1 + 2

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W4** SDK | TASK-063 + 064 + 065 + 066 + 067 + 068 | `pysdk/rti1516e/connection.py` (extend), `declaration.py`, `object.py`, `interaction.py`, `events.py` (extend), `standard.py` (extend), `errors.py` (extend if needed for typed-exception mapping) |

Single sub-agent for the bundled SDK layers. The decomposition into 6 tasks reflects review-time logical grouping; one sub-agent implements the cohesive layer because the public surface (Federate class) is shared across all of them. **The agent SHOULD commit once per task** to keep PR diffs reviewable, even though it's a single sub-agent dispatch.

Rationale for bundling vs. parallel: connection.py defines the Federate class; declaration/object/interaction/events live as methods on Federate. Splitting them across sub-agents would force a mock-on-mock dance for early dispatch — net slower than serializing them within one sub-agent.

Spec tests turned green: `test_spec_m4_connection_lifecycle.py`, `test_spec_m4_declaration.py`, `test_spec_m4_object.py`, `test_spec_m4_interaction.py`, `test_spec_m4_events_stream.py`, `test_spec_m4_standard_ambassador.py`.

### Wave 5 — bridge basics + smoke

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W5A** port mapping | TASK-069 | `pysdk/pyjevsim_bridge/port_mapping.py` (extend) |
| **W5B** pyjevsim smoke | TASK-072 | `pysdk/pyproject.toml` (set the pyjevsim version pin in the `pyjevsim` extra). NOTE: W5B also installs pyjevsim into the test environment so `test_spec_m4_pyjevsim_smoke.py` actually runs (instead of skip). |

Two sub-agents in parallel. Zero file collisions (W5B touches only `pyproject.toml`'s `pyjevsim` extra, an isolated section).

### Wave 6 — bridge core

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W6** time advance + select | TASK-070 + TASK-071 | `pysdk/pyjevsim_bridge/time_advance.py` (extend), `select_preserve.py` (extend) |

Single sub-agent because the time-advance loop directly invokes the select-preserve helper. Tests for both share the StubCoupledModel fixture.

Spec tests turned green: `test_spec_m4_time_advance.py`, `test_spec_m4_select_preserve.py`.

### Wave 7 — examples + harnesses + M4 gate

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W7** examples + gate | TASK-073 + TASK-074 + TASK-075 | `examples/pyjevsim/runner.py`, `producer.py`, `consumer.py`, `examples/pyjevsim/determinism_test.py`, `pysdk/tests/test_lint_strict.py`. ALSO unskips `pysdk/tests/spec/m4/test_spec_m4_determinism.py` and `test_spec_m4_replay.py`. |

Single sub-agent for sequential integration: build example → run determinism harness → run lint/coverage gate. Mirrors M3's W4 shape exactly.

## 5. Spec test mapping

The orchestrator-frozen spec tests under `pysdk/tests/spec/m4/`. Each wave's sub-agent verifies their work by turning specific tests green:

| Spec test file | Turns green at end of |
|---|---|
| `test_spec_m4_encoding_conformance.py` (per-vector subtests, 94 vectors) | Partially Wave 1+2; fully Wave 3 (W3 dispatcher) |
| `test_spec_m4_fom_diagnostics.py` (10 bad fixtures) + `test_spec_m4_fom_acceptance.py` (good FOMs) | Wave 2 (W2D) |
| `test_spec_m4_connection_lifecycle.py` | Wave 4 (W4) |
| `test_spec_m4_declaration.py` | Wave 4 (W4) |
| `test_spec_m4_object.py` | Wave 4 (W4) |
| `test_spec_m4_interaction.py` | Wave 4 (W4) |
| `test_spec_m4_events_stream.py` | Wave 4 (W4) |
| `test_spec_m4_standard_ambassador.py` | Wave 4 (W4) |
| `test_spec_m4_port_mapping.py` | Wave 5 (W5A) |
| `test_spec_m4_pyjevsim_smoke.py` | Wave 5 (W5B) — pyjevsim must be installed |
| `test_spec_m4_time_advance.py` | Wave 6 (W6) |
| `test_spec_m4_select_preserve.py` | Wave 6 (W6) |
| `test_spec_m4_determinism.py` (currently skip-scaffold) | Wave 7 (W7 unskips) |
| `test_spec_m4_replay.py` (currently skip-scaffold) | Wave 7 (W7 unskips) |

All spec tests live in `pysdk/tests/spec/m4/` (lowercase, Python convention). The frozen-paths check now blocks agent writes to this directory.

## 6. Hard rules per wave

These apply per `docs/DISPATCH.md` §4 (no self-selection, no multi-task PRs without orchestrator nod, etc.) plus M4-specific:

1. **Stub signature freeze**. Every public class/method declared in the orchestrator-seeded stubs (`Codec` ABC, `RtiConnection.connect`, `Federate.publish_object_class`, `HLAFederate.run`, etc.) is part of the M4 contract. Sub-agents may add private helpers and dataclass fields with defaults but must NOT change exported signatures or `__all__` lists without a `contract-change-request:` issue per `docs/WORKFLOW.md` §4.4.

2. **No spec-test edits**. `pysdk/tests/spec/m4/*.py` is orchestrator-frozen. Agent C may ADD new tests under `pysdk/tests/test_*.py` (note: not `tests/spec/m4/`) for internal helpers, but must NEVER weaken or delete existing spec tests.

3. **Encoding byte-identicality is non-negotiable**. Every value in `tests/conformance/encoding_vectors.json` MUST encode to the exact bytes the JSON specifies. Decode MUST round-trip to a value `==` the JSON value (Python equality semantics; numbers compare exactly). If any vector cannot be made to pass, file a `verification:M1` cross-language issue against Agent B BEFORE working around it.

4. **mypy --strict from day one**. `make py-typecheck` must stay green throughout. If a sub-agent introduces a type that mypy can't infer, they add a precise annotation — do NOT use `# type: ignore` to silence it (except for unavoidable third-party gaps in `_generated/` which is already excluded).

5. **No real network in spec tests**. The FakeRtiServer is the test boundary. Agent C wires the SDK so its transport layer is injectable (constructor argument, attribute, factory) — when this is missing, spec tests fail with AttributeError, signaling a design issue.

6. **No real pyjevsim in bridge spec tests**. StubCoupledModel covers the bridge's contract surface. The ONE test that imports real pyjevsim is `test_spec_m4_pyjevsim_smoke.py` (which skips if pyjevsim isn't installed). Reason: keeps spec tests fast and dependency-light; makes pyjevsim drift detection explicit.

7. **Determinism: no dict iteration in encode/decode hot paths**. Use `sorted(dict.items())` or pass-through structured types where order matters. Spec tests don't catch this directly (Python dict iteration is insertion-order since 3.7) but `verification:M3` audit-style checks will flag it.

8. **Sentinels per task**. Each task gets its own `docs/tasks/signals/TASK-NNN.done`. Bundled sub-agent work (e.g. W4 covering TASK-063..068) produces ALL bundled sentinels in the final commit.

## 7. Verification activities (gate-time, not dispatched as TASK-NNN)

Per `docs/AGENTS.md` §6.2 + `docs/agent-{a,b}-*.md` §5:

- **Agent A at M4 gate**: write a "naughty Python federate" test using the now-real Python SDK (parallel of Agent C's M2 verification, but reversed). Confirm Agent A's RTI handles malformed inputs gracefully when the federate is Python. File `verification:M4` issue.
- **Agent B at M4 gate**: extend the determinism audit to Python encoders. Audit: any `dict.values()` / `dict.items()` iteration in encode paths; any reliance on Python set ordering; any UTF-16 endianness slips. File `verification:M4` issue.

These run after Wave 7 completes; they are not part of the wave model.

## 8. Dispatch order checklist (orchestrator's runbook)

Step-by-step the orchestrator follows for M4:

1. **Pre-work confirmation** (DONE as of this commit):
   - `pysdk/` package skeleton + frozen-shape stubs on `main`
   - `pysdk/tests/spec/m4/*.py` spec tests on `main`, RED for the right reason (`NotImplementedError` from time-package stubs)
   - `scripts/check-milestones.sh` updated to probe `pysdk/tests/spec/m4/`
   - `scripts/check-frozen-paths.sh` updated to block agent writes to `pysdk/tests/spec/`
   - `Makefile` extended with `py-codegen`, `py-test`, `py-lint`, `py-typecheck` targets
2. **Wave 1**: spawn W1A + W1B + W1C + W1D + W1E in one parallel `Agent` tool call (5 in parallel). Wait for all to push branches. Review + merge in this order: encoding primitives first (W1A→B→C→D), then FOM model + codegen (W1E).
3. **Wave 2**: spawn W2A + W2B + W2C + W2D in parallel. Merge in any order (disjoint files).
4. **Wave 3**: spawn W3 alone. Closes the encoding gate. Validate: `pytest pysdk/tests/spec/m4/test_spec_m4_encoding_conformance.py` is 100% GREEN.
5. **Wave 4**: spawn W4 alone. Bundled SDK Layer 1+2 implementation; one commit per task within the sub-agent's branch.
6. **Wave 5**: spawn W5A + W5B in parallel.
7. **Wave 6**: spawn W6 alone.
8. **Wave 7**: spawn W7 alone. Closes M4. **This is the M4 milestone gate sub-agent.**
9. **M4 gate**: re-run `scripts/check-milestones.sh`. Should report `M0..M4: DONE`. Push tag `m4` (optional).

## 9. Risk mitigations

| Risk | Mitigation |
|---|---|
| Agent C reshapes `Codec` ABC to add a method | Pre-commit `check-frozen-paths.sh` catches edits to `pysdk/tests/spec/`; orchestrator-review catches signature changes in `_base.py` (which is FROZEN-shape but technically agent-writable) |
| Cross-language byte mismatch on `HLAfloat32LE` of subnormals | `tests/conformance/encoding_vectors.json` only includes exactly-representable doubles (per `docs/agent-b-fom-encoding.md` §7 anti-goal). If a subnormal vector slips in, file Agent B verification issue, do not work around |
| Asyncio test flakes from missing `await` | All spec tests use `pytest-asyncio` in `auto` mode; `pyproject.toml` sets `asyncio_mode = "auto"`. Sub-agent must NOT change this |
| pyjevsim version pin breaks W7 example | TASK-072 (W5B) lands BEFORE W7. If smoke test can't import the pinned pyjevsim, W5B's PR fails review and the version is revised before W6/W7 dispatch |
| `_generated/` proto stubs missing in fresh worktree | Each wave's sub-agent runs `make py-codegen` as the first step. The output is gitignored but reproducible. (Same hassle as Go-side genproto in M3 — known infrastructure quirk.) |
| Spec test rooted at `pysdk/tests/spec/m4/` can't find `tests/conformance/...` | `vector_loader.py` resolves the path via `Path(__file__).resolve().parents[5]` — counted from the file location. If repo layout changes, the resolver breaks loudly with a FileNotFoundError naming the missing file |
| Agent C ships a Federate class without an injectable transport | Spec tests fail with AttributeError. The fail mode is loud and locatable; orchestrator review catches it; fix is to add a `connect_with_transport(srv)` factory or a constructor argument |

## 10. Cross-wave file conflict scan

Pre-dispatch verification — every owned-file set must be disjoint from every other in the same wave AND across the bundled tasks within a sub-agent.

| Wave | Sub-agent | Files (head) |
|---|---|---|
| 1 | W1A | `pysdk/rti1516e/encoding/integer.py` |
| 1 | W1B | `pysdk/rti1516e/encoding/float_codec.py` |
| 1 | W1C | `pysdk/rti1516e/encoding/byte_codec.py` |
| 1 | W1D | `pysdk/rti1516e/encoding/opaque.py` |
| 1 | W1E | `pysdk/rti1516e/fom/model.py`, `pysdk/rti1516e/_proto.py`, `pysdk/Makefile.codegen` |
| 2 | W2A | `pysdk/rti1516e/encoding/string_codec.py` |
| 2 | W2B | `pysdk/rti1516e/encoding/fixed_array.py`, `variable_array.py` |
| 2 | W2C | `pysdk/rti1516e/encoding/fixed_record.py`, `variant_record.py` |
| 2 | W2D | `pysdk/rti1516e/fom/parser.py` |
| 3 | W3 | `pysdk/rti1516e/encoding/dispatch.py` |
| 4 | W4 | `pysdk/rti1516e/connection.py`, `declaration.py`, `object.py`, `interaction.py`, `events.py`, `standard.py`, `errors.py` (extend if needed) |
| 5 | W5A | `pysdk/pyjevsim_bridge/port_mapping.py` |
| 5 | W5B | `pysdk/pyproject.toml` (pyjevsim extra only) |
| 6 | W6 | `pysdk/pyjevsim_bridge/time_advance.py`, `select_preserve.py` |
| 7 | W7 | `examples/pyjevsim/*.py`, `pysdk/tests/spec/m4/test_spec_m4_determinism.py` (UNSKIP), `pysdk/tests/spec/m4/test_spec_m4_replay.py` (UNSKIP), `pysdk/tests/test_lint_strict.py` |

No file appears in more than one cell. The `pysdk/pyproject.toml` cell is split: orchestrator owns initial bootstrap; W5B is the ONLY sub-agent that re-edits it (and only the `pyjevsim` extra section). `pysdk/tests/spec/m4/test_spec_m4_{determinism,replay}.py` are unskipped (body replaced) in Wave 7 only — orchestrator authorizes this as the spec-scaffold-flip pattern (mirrors M3 W4's edits to `rti/spec/M3/replay_test.go`).

Cross-agent reads (allowed):
- All Agent C waves read `proto/rti/v1/*.proto` (codegen input)
- W3 + W7 read `tests/conformance/encoding_vectors.json` (Agent B's domain)
- W2D + W7 read `tests/conformance/foms/{good,bad}/*.xml` (Agent B's domain)

No agent writes outside `pysdk/`, `examples/pyjevsim/`, or `docs/tasks/signals/`.
