# Agent C Brief — Python SDK + pyjevsim Bridge (gemini-sandbox)

**Pre-reading required**: `docs/AGENTS.md`, `docs/srs.md`. Do not start work until you've read both.

Read also: pyjevsim's source (latest version) — particularly its `CoupledModel`, `AtomicModel`, time advance (`ta`), output (`output_handler`), external transition, and `select()` semantics. You are bridging DEVS to HLA; you must understand both sides.

---

## 1. Your Role

You own the **Python federate SDK** and the **pyjevsim → HLA bridge**. Your code lets pyjevsim users wrap a coupled model in `HLAFederate(coupled_model)` and have it participate in an HLA federation served by Agent A's Go RTI.

Your encoder must produce byte-identical output to Agent B's Go encoder. The conformance suite enforces this — non-negotiable.

## 2. Owned Paths (you may write here)

- `pysdk/rti1516e/` — generated gRPC client + idiomatic Python API + standard-shaped adapter.
- `pysdk/rti1516e/encoding/` — Python implementation of HLA Evolved encoding rules.
- `pysdk/rti1516e/fom/` — Python FOM model + parser (Python-side mirror of Agent B's Go model).
- `pysdk/pyjevsim_bridge/` — `HLAFederate` adapter, port mapping, time advance bridge.
- `pysdk/tests/`
- `examples/pyjevsim/` — runnable example with two coupled models, one RTI.

## 3. Forbidden Paths

You may **read** but never **write**:

- `proto/**` (frozen — but you generate clients from it)
- `rti/**` (Agent A and B own this)
- `tests/conformance/encoding_vectors.json` (Agent B owns; you consume it)
- `docs/**`, `.github/**`

Generated gRPC code lives in `pysdk/rti1516e/_generated/`. Regenerate via the Makefile target; do not hand-edit.

## 4. Milestone Deliverables

### M4 — Python SDK + pyjevsim Bridge

Implements: **FR-PYJ-1..4, FR-ENC-2 (Python side), IR-PYAPI-1, NFR-DET-1..2 (federate-side), NFR-OPS-1.**

#### `pysdk/rti1516e/encoding/`

- Python encoders/decoders for every HLA Evolved type, mirroring Agent B's Go implementation.
- **MUST produce byte-identical output to Go encoder** for every entry in `tests/conformance/encoding_vectors.json`.
- Use `struct` module for primitives; manual byte-buffer for composite types.
- Type mapping suggestion: `HLAfixedRecord` ↔ `dataclass`, `HLAvariantRecord` ↔ `Union[...]` with discriminator field.
- Tests (`pysdk/tests/test_encoding.py`) parametrize over the JSON vectors.

#### `pysdk/rti1516e/fom/`

- Python FOM model: `dataclass` mirrors of Agent B's Go model.
- Parser: same XML, same strict validation. Same error codes (`FOM-001`, etc.) so users see consistent diagnostics from either side.
- Optional: if duplication with Agent B becomes painful, propose to orchestrator a shared schema-driven codegen approach (don't implement unilaterally).

#### `pysdk/rti1516e/` (the SDK)

Two-layer API per IR-PYAPI-1:

**Layer 1: idiomatic Python (primary)**
```python
from rti1516e import RtiConnection, FederationSpec

async with RtiConnection.connect(url="grpc://localhost:8442") as rti:
    async with rti.join_federation(
        FederationSpec(name="demo", fom_modules=["./demo.fom.xml"]),
        federate_name="alice",
    ) as fed:
        await fed.publish_object_class("Vehicle", attributes=["position", "speed"])
        async for event in fed.events():
            match event:
                case ReflectAttributeValues(obj, values, ts): ...
                case ReceiveInteraction(cls, params, ts): ...
                case TimeAdvanceGrant(t): ...
```

**Layer 2: standard-shaped adapter (`Rti1516eAmbassador`)**
- A thin class that mirrors the 1516-2010 Java/C++ ambassador API (callback methods).
- Wraps Layer 1 internally; intended for users porting from existing RTIs.
- Lives in `pysdk/rti1516e/standard.py`.

#### `pysdk/pyjevsim_bridge/`

- `HLAFederate(coupled_model: CoupledModel, federation: FederationSpec, federate_name: str, port_mapping: PortMapping)`.
- **Port mapping** (FR-PYJ-2): each named port on the coupled model maps to a FOM interaction class. Provided explicitly via `PortMapping` dict; auto-generation can come later.
- **Time advance** (FR-PYJ-3):
  - Bridge calls `coupled_model.time_advance()` → gets DEVS `ta` value.
  - Issues `nextMessageRequest(now + ta)` to RTI.
  - On `TimeAdvanceGrant(t)`:
    - If grant time = now + ta (no external event arrived earlier): run internal cycle (`output_handler` → `internal_transition`).
    - If grant time < now + ta (external event arrived first): deliver via `external_transition`, no internal cycle.
  - Drain any output ports → send as interactions.
  - Loop.
- **`select()` preservation** (FR-PYJ-4): when multiple events have identical timestamps, sort by pyjevsim's `select()` order *before* handing to HLA — the HLA tie-break (federate→object→attribute handle) sees them in the order pyjevsim would have chosen.
- pyjevsim version pin: latest stable; record exact version in `pyproject.toml` and add a smoke test that fails loudly if API drift breaks the bridge.

#### `examples/pyjevsim/`

- Two minimal pyjevsim coupled models (e.g. `Producer` and `Consumer`).
- One FOM (`pyjevsim-bridge.fom.xml`, also used by Agent B's parser tests).
- A runner script that starts the RTI, joins both federates, runs N ticks, prints final state, and asserts deterministic event log.

**M4 exit criteria** (objective, testable):

1. `examples/pyjevsim/` runs end-to-end against Agent A's live RTI.
2. Run example **10 consecutive times**, same seed; event logs from RTI side byte-identical (`sha256sum`).
3. Python encoder passes 100% of `tests/conformance/encoding_vectors.json`.
4. `mypy --strict pysdk/` clean.
5. `ruff check pysdk/` clean.
6. `pytest pysdk/` green; coverage ≥80% on owned packages.
7. SDK importable from a fresh venv with no missing deps.

### M5 — End-to-end (you contribute)

- Cross-language smoke test: one pyjevsim federate + one Go test federate (Agent A provides) joined to the same federation; exchange interactions; both observe consistent state.
- Verify both `--mode=verbose` and `--mode=best-effort` from the federate side (declare a best-effort attribute in the FOM, confirm it's transmitted with RO semantics).

## 5. Verification Responsibilities (at OTHER agents' gates)

### At M1 gate (Agent B's milestone)

- Write a Python decoder against the golden vectors **in parallel** with B's Go encoder. This both verifies B's vectors are well-formed and bootstraps your M4 work.
- File `verification:M1` issue listing any vectors where Python decoder cannot interpret the bytes (likely indicates B's spec interpretation needs review).

### At M2 gate (Agent A's milestone)

- Write a "naughty federate" in Go (yes, Go — borrow patterns from Agent A's examples; your Python SDK isn't ready yet) that:
  - Sends messages out of order.
  - Joins the same federation twice with the same name.
  - Resigns mid-update.
  - Sends interactions of unsubscribed classes.
- Confirm Agent A's RTI handles each gracefully (proper error codes, no crash, federation continues for well-behaved federates).
- File `verification:M2` issue with the test program and outcomes.

### At M3 gate (Agent A's milestone)

- Write a "slow federate" test: deliberately delay `nextMessageRequest` responses; confirm the stall timeout fires within the configured window and the diagnostic identifies your federate by name.
- File `verification:M3` issue with the test and outcome.

## 5.5 TDD Patterns for Your Domain

Read `docs/TDD.md` first.

### Encoding (Python mirror)
Drive the Python codec implementation directly from `tests/conformance/encoding_vectors.json`:

```python
@pytest.mark.parametrize("vec", load_vectors())
def test_conformance_encoding(vec):
    codec = codec_for(parse_type(vec["type"]))
    assert codec.encode(vec["value"]) == bytes.fromhex(vec["bytes"])
    decoded, n = codec.decode(bytes.fromhex(vec["bytes"]))
    assert decoded == vec["value"]
    assert n == len(bytes.fromhex(vec["bytes"]))
```

This is your conformance gate — when this is fully green, M4 encoding is done.

### SDK against fake RTI
Build `FakeRtiServer` — a small in-process double (pure Python or in-proc gRPC) that records calls and emits canned `FederateEvent`s. SDK tests drive scenarios through it:

- Join → Publish → Register → `update_attributes` → assert recorded `UpdateRequest` matches attributes/timestamp.
- Subscribe → fake pushes `ReflectAttributeValues` → assert consumer callback fires with expected values.
- Each error response → assert correct typed exception (e.g. `FederationNotFound`) raised.

### pyjevsim bridge
Build `StubCoupledModel` exposing controllable `ta()`, `output_handler()`, recordable `external_transition()`. Drive `HLAFederate` against it + `FakeRtiServer`:

- `ta=2.0`, no incoming events → assert `next_message_request(now + 2.0)` issued; on grant, `internal_transition` called.
- `ta=5.0`, interaction at `t=1.0` → assert grant arrives at 1.0, `external_transition` called, next ta evaluated.
- Two simultaneous interactions at `t=3.0` → assert delivery order matches pyjevsim `select()`.

### Determinism
Run `examples/pyjevsim/` 10× with same seed; capture event logs from the RTI side; assert identical SHA256.

### pyjevsim API drift smoke
Test that imports specific pyjevsim symbols (`CoupledModel`, `AtomicModel`, `select`) and exercises a small protocol. Pin `pyjevsim==X.Y.Z` in `pyproject.toml`. Future bump that breaks the smoke fails loudly with a diagnostic naming the broken symbol.

The orchestrator pre-writes specification tests for M4 under `tests/spec/M4/` covering bridge time-advance behavior and SDK lifecycle. You cannot weaken them. Your own unit tests fill in detail.

## 6. DEVS ↔ HLA Mapping (Critical Reference)

| pyjevsim concept | Bridge handling | HLA target |
|---|---|---|
| `CoupledModel` | Wrapped 1:1 by `HLAFederate` | One federate |
| `AtomicModel` (internal to coupled) | Native pyjevsim scheduling, in-process | Not exposed to HLA |
| `time_advance()` (`ta`) | Bridge reads after each cycle | `nextMessageRequest(now + ta)` |
| `output_handler()` | Bridge runs after grant when no external event arrived first | Drains output ports → `sendInteraction` |
| `external_transition_handler()` | Bridge calls when grant comes earlier than `ta` due to incoming interaction | `receiveInteraction` callback feeds it |
| `internal_transition_handler()` | Bridge triggers after `output_handler` | No HLA-side action |
| Named output port | Mapped via `PortMapping` | FOM interaction class |
| Named input port | Mapped via `PortMapping` | Subscribed FOM interaction class |
| `select()` (tie-break) | Bridge sorts simultaneous events first | TSO with stable secondary tie-break |

## 7. Spec Pointers (IEEE 1516)

- Federate Interface (especially the bits you wrap) — IEEE 1516.1-2010 §4–§8.
- Time mgmt semantics that drive your bridge — IEEE 1516.1-2010 §8.10–§8.14 (NER), §8.5 (regulating/constrained).
- Encoding rules (Python mirror) — IEEE 1516.2-2010 §4.
- DIF XML for FOM parsing — IEEE 1516.2-2010 Annex A.

## 8. Anti-Goals (Specific to You)

- Do not implement Level 1 (drop-in pyjevsim compatibility). The locked decision is Level 2 (wrapper). SRS FR-PYJ-1.
- Do not map atomic models to federates. Coupled = federate. SRS FR-PYJ-1.
- Do not implement TAR in cut 1. NER only. SRS FR-TM-2.
- Do not invent your own FOM model — mirror Agent B's structure exactly so cross-side debugging is straightforward.
- Do not block the asyncio event loop with synchronous calls into pyjevsim. If pyjevsim's API is sync, isolate it to a worker thread and bridge with `asyncio.to_thread`.
- Do not depend on optional packages without justification — every dep adds friction for users.
- Do not monkey-patch pyjevsim. Your bridge wraps; it does not modify.

## 9. When to Stop and Ask

- Any time pyjevsim's behavior conflicts with HLA's required semantics (likely candidates: simultaneous events, port message ordering).
- Any time the encoding vectors and your interpretation diverge — Agent B is the source of truth, but flag it via verification issue, not silent change.
- Any time the SDK shape needs a new gRPC method or proto field.
