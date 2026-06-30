# Agent D Brief — C++ SDK + DLC compliance track (cppsdk)

**Pre-reading required**: `docs/AGENTS.md`, `docs/srs.md` (especially §5.14 and §7.4), `docs/DLC_COMPLIANCE_PROGRAM.md`, `docs/DLC_DIVERGENCE_CATALOGUE.md`. Do not start work until you've read these.

Read also: the IEEE 1516.1-2010 spec text (Annex A, B, C) at `~/prti1516e/api/cpp/HLA_1516-2010/RTI/` — Pitch's pRTI Free 5.5.10 ships the spec headers verbatim and is the reference your work bake-offs against. You are implementing a strict DLC C++ federate API; you must understand the spec surface in detail before writing any impl code.

---

## 1. Your Role

You own the **C++ federate SDK** and the **IEEE 1516.1-2010 DLC compliance track** (M31-M35). Your code lets a federate written against any 1516e DLC C++ vendor (Pitch, MAK, Portico) compile **unchanged** against gorti's `cppsdk`, modulo the connect-string argument value (vendor connect syntax is implementation-defined).

Your headers must be byte-shape-identical to Pitch's spec reprint at the API surface — verified by the M31 lockfile (`cppsdk/tests/dlc/lockfile/`, ~200 `static_assert`-based assertions, one per row in `docs/DLC_DIVERGENCE_CATALOGUE.md`).

The conformance suite (`cppsdk/tests/dlc/conformance/`, 27 fixtures) verifies that gorti's *behavior* matches the spec's mandated event sequences. The parity-mode leg of each fixture additionally bake-offs against Pitch pRTI; when gorti and Pitch disagree, the tie-breaker is the spec text (`docs/DLC_COMPLIANCE_PROGRAM.md §5.2.2`).

## 2. Owned Paths (you may write here)

- `cppsdk/include/RTI/` — the spec-mandated public header tree (30 files: 15 top-level + 9 `encoding/` + 6 `time/`).
- `cppsdk/include/rti1516e/` — back-compat re-export shims from `RTI/*.h` for M17-era callers.
- `cppsdk/src/dlc/` — DLC-strict implementation (lands M32-M35; nothing here in M31).
- `cppsdk/src/` — M17-era impl (frozen-ish; deprecated-marked at M35).
- `cppsdk/tests/dlc/` — DLC test suite (lockfile + conformance fixtures + parity legs).
- `cppsdk/tests/dlc/lockfile/` — ~140 per-TU compile-only `static_assert` files.
- `cppsdk/tests/dlc/conformance/<fixture>/` — 27 spec-section fixtures.
- `cppsdk/tests/dlc/conformance/<fixture>/parity/` — per-fixture parity-mode wiring (collapses the old top-level `tests/parity/` into per-fixture subdirs per `docs/DLC_COMPLIANCE_PROGRAM.md §5.2`).
- `cppsdk/CMakeLists.txt` + `cppsdk/tests/dlc/lockfile/CMakeLists.txt` + `cppsdk/tests/dlc/conformance/CMakeLists.txt` — build wiring.

## 3. Forbidden Paths

You may **read** but never **write**:

- `proto/**` (frozen — but the wire surface is gRPC, not yours)
- `rti/**` (Agent A and B own this; the gorti rtid binary)
- `pysdk/**` (Agent C owns this)
- `docs/srs.md` §5.14 + §7.4 (orchestrator-frozen after M31 lands; the orchestrator updates §10.6 milestone rows on each milestone close)
- `docs/idd.md` §1.8 (orchestrator-frozen)
- `docs/DLC_COMPLIANCE_PROGRAM.md`, `docs/DLC_DIVERGENCE_CATALOGUE.md`, `docs/M31_DISPATCH_PLAN.md` (orchestrator-frozen)
- `docs/AGENTS.md`, `docs/PITCH_PARITY.md`, `docs/RTI_CONFORMANCE_AUDIT.md`, `docs/PITCH_GOLDEN_LICENSING.md`, `.github/**`
- `tests/conformance/encoding_vectors.json` (Agent B owns; you consume it for cross-language verification)

You **may** propose changes to the frozen docs via PR comment when a divergence-catalogue row needs revision; the orchestrator decides whether to apply.

## 4. Milestone Deliverables

### M31 — DLC lockfile (RED test scaffold)

Implements: **FR-DLC-1..18 SCAFFOLD (no impl), IR-CPPAPI-1..4 SCAFFOLD.**

See `docs/M31_DISPATCH_PLAN.md` for the full task list. M31 is **test-only** — no impl code lands; every lockfile and fixture is RED by design.

#### `cppsdk/include/RTI/` — 30 forward-declaration stubs

Per `docs/M31_DISPATCH_PLAN.md §2.5` and the M31 orchestrator output: each stub declares the spec-mandated types as classes with the right shape (members, signatures, base hierarchy) so the lockfile `static_assert` checks fire on type mismatch, not "not a member". Bodies land M32-M35.

#### `cppsdk/tests/dlc/lockfile/` — ~140 per-TU files / ~200 assertions

Layout per `docs/M31_DISPATCH_PLAN.md §2.1`. Each `.cpp` is a compile-only translation unit with ~10-30 `static_assert` lines per file. Tag every assertion with the spec section it enforces (`// §4.2`, `// §10.5`). One TU per assertion; CTest registered with `WILL_FAIL TRUE`.

Example skeleton (`test_rtiambassador_connect.cpp`):

```cpp
#include <RTI/RTIambassador.h>
#include <RTI/FederateAmbassador.h>
#include <RTI/Enums.h>
#include <type_traits>
#include <string>

using rti1516e::CallbackModel;
using rti1516e::FederateAmbassador;
using rti1516e::RTIambassador;
using rti1516e::HLA_IMMEDIATE;        // CRITICAL: unscoped enumerator per FR-DLC-16

// §4.2 — connect(FederateAmbassador&, CallbackModel, wstring const& localSettings)
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().connect(
        std::declval<FederateAmbassador&>(),
        HLA_IMMEDIATE,
        std::declval<std::wstring const&>())),
    void>);
```

#### `cppsdk/tests/dlc/conformance/<fixture>/` — 27 fixtures

Per `docs/M31_DISPATCH_PLAN.md §2.2` (full catalog). Each fixture:

- `federate*.cpp` — federate source using ONLY spec-defined APIs (single file, no `#ifdef`)
- `federation.fom.xml` — spec-strict FOM
- `expected.*.log` — canonical event-sequence golden(s); skeletons with `// TBD-pitch-capture` markers in M31, real captures gated on Pitch EULA review
- `test_<name>.cpp` — gtest driver
- `README.md` — one-paragraph scenario + spec § citation (every line of the golden must carry a `// §N.M` cite — enforced by `scripts/check-spec-traceability.sh`)

#### Conformance harness

- `_harness/rtid_runner.h` / `log_diff.h` / `golden_loader.h` — yours (Agent C of the M31 fan-out wrote the C++ headers; this brief is for Agent D as the overall C++ SDK owner across M32+).
- `_harness/normalize.py` / `pitch_build.sh` / `pitch_run.sh` — landed in M31 by the parity-mode fan-out sub-agent. You consume these from M32 onward.

**M31 exit criteria** (objective, testable):

1. All ~200 lockfile assertions RED (compile-fail) — count matches `cppsdk/tests/dlc/lockfile/expected_red_count.txt`.
2. All 27 conformance fixtures fail-to-link with `undefined reference to rti1516e::*` errors.
3. Parity harness skips cleanly without `PRTI_HOME`.
4. 30 RTI header stubs parse standalone under `g++ -std=c++17`.
5. `scripts/check-milestones.sh check_m31` reports `DONE (11/11)`.
6. M17 cppsdk tests (`cppsdk/tests/test_callbacks_integration.cpp` etc.) stay GREEN.
7. `docs/dlc-spec-coverage.md` auto-generates and lists ≥40 §-sections covered.

### M32 — Headers + construction + handles + collections + VLD (~50/200 GREEN)

Implements: catalogue sections 1, 2, 5, 7, 8, 15. Federate-side type-shape — `RTIambassadorFactory`, abstract `RTIambassador`, `std::wstring` everywhere, typed handle classes with `_impl` pimpl, `VariableLengthData` class (copy / borrow / take modes), unscoped enums, range bounds, supplemental info structs.

Exit: Pitch chat sample's first 200 lines compile against gorti `cppsdk`; ~50 lockfile assertions flip GREEN.

### M33 — Callbacks + exceptions + ownership + DDM + obj-mgmt (~100/200 GREEN)

Implements: catalogue sections 3, 4, 6, 10, 11, 12, 13, 17. The 3x reflect/receive/remove overload set per FR-DLC-5, the ~120 exception classes per FR-DLC-6, two-phase ownership protocol, DDM with regions per §9, mandatory `tag` per §6.10 / §7.8, `CallNotAllowedFromWithinCallback` runtime check.

Exit: cross-RTI parity test extends from 1 federate to 5 (`cppsdk/tests/dlc/conformance/<>/parity/` legs pass against Pitch under PRTI_HOME).

### M34 — Encoding + time types (~40/200 GREEN)

Implements: catalogue sections 9, 14. `DataElement` abstract base + 19 basic encoders + 5 composite encoders. `HLAfloat64Time/Interval/Factory` + `HLAinteger64Time/Interval/Factory`. Federation chooses logical-time impl via `createFederationExecution(..., logicalTimeImplementationName=L"HLAfloat64Time")`.

Exit: encoding-vector tests pass with `HLA*` helpers replacing hand-encoders.

### M35 — MOM + back-compat shim deprecation + IVCT subset (~10/200 GREEN + conformance gate)

Implements: catalogue section 16. MOM via standard pub/sub to `HLAobjectRoot.HLAmanager.*`. `[[deprecated]]` annotations on M17-era `rti1516e/*.h` headers. `docs/MIGRATION_M17_TO_DLC.md` published. **gorti rtid passes an IVCT-derived conformance subset** (catalog selected during M35 W2; see `docs/RTI_CONFORMANCE_AUDIT.md §6`).

Exit: Pitch chat sample compiles + runs end-to-end against gorti. `docs/dlc-spec-coverage.md` reports 100% spec-section coverage.

## 5. Verification Responsibilities (at OTHER agents' gates)

### At every milestone gate

- Run `scripts/check-milestones.sh check_m31` (and check_m32..m35 once they land) on the candidate merge commit; capture output in your status report.
- Compile-verify the 30 stubs parse standalone via the parse-test loop in `docs/M31_DISPATCH_PLAN.md §3`.
- Run `ctest -L lockfile --output-on-failure` and assert the failing-TU count matches `cppsdk/tests/dlc/lockfile/expected_red_count.txt`.
- Verify M17 cppsdk tests (`cppsdk/tests/test_callbacks_integration.cpp` etc.) stay GREEN.

### At Agent C's gates (pysdk milestones)

- Compile-verify any new typed-handle / VLD-shape changes don't break the C++ cross-language parity fixture (`cppsdk/tests/dlc/conformance/xlang_python_cpp_pubsub/`).
- Confirm Python and C++ encoders produce byte-identical output for every entry in `tests/conformance/encoding_vectors.json` (FR-ENC-2 cross-lang).

## 5.5 TDD Patterns for Your Domain

Read `docs/TDD.md` first.

### Lockfile-first (M31's main pattern)

Drive the C++ surface directly from the spec — every catalogue row in `docs/DLC_DIVERGENCE_CATALOGUE.md` is a lockfile assertion:

```cpp
// cppsdk/tests/dlc/lockfile/types/test_handle.cpp — §10.5
#include <RTI/Handle.h>
#include <type_traits>

static_assert(std::is_class_v<rti1516e::ObjectClassHandle>);
static_assert(!std::is_assignable_v<rti1516e::ObjectClassHandle&,
                                    rti1516e::AttributeHandle>);  // type-safe
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::ObjectClassHandle const&>().encode()),
    rti1516e::VariableLengthData>);
```

When a `static_assert` fires, the build fails with the assertion's location and the surface-drift cause is unambiguous. M31 lands all of these RED; M32+ flips them GREEN as impl lands.

### Conformance fixtures (M31's secondary pattern)

For each spec §-section that has observable callback behavior, write one fixture that exercises it and one canonical golden:

```cpp
// cppsdk/tests/dlc/conformance/om_helloworld_pubsub/federate.cpp
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
// ...
```

Driver normalizes the captured log via `_harness/normalize.py` and diffs vs the golden. Test PASS iff diff = 0.

### Parity-mode bake-off (M32+ as parity goldens land)

Each fixture also has a parity leg that compiles the SAME `federate*.cpp` against Pitch headers, runs it against Pitch CRC, normalizes, and diffs gorti's run against Pitch's run. Diverging diffs raise the spec-vs-Pitch tie-breaker (`docs/DLC_COMPLIANCE_PROGRAM.md §5.2.2`).

### Cross-language smoke

`cppsdk/tests/dlc/conformance/xlang_python_cpp_pubsub/` — Python publisher (via pysdk M28 typed-handle path) + C++ subscriber (via cppsdk DLC path) in the same gorti federation. Verifies that the DLC C++ surface and the pysdk surface produce identical wire behavior.

## 6. Spec Pointers (IEEE 1516)

- DLC C++ federate API headers — IEEE 1516.1-2010 Annex A + Annex C; Pitch's reprint at `~/prti1516e/api/cpp/HLA_1516-2010/RTI/`.
- Encoding rules — IEEE 1516.2-2010 §4 + Annex B.
- Service group semantics — IEEE 1516.1-2010 §4 (federation), §5 (declaration), §6 (object), §7 (ownership), §8 (time), §9 (DDM), §10 (support), §11 (MOM).
- Exception hierarchy — IEEE 1516.1-2010 Annex C.

## 7. Anti-Goals (Specific to You)

- **Do not write impl code in M31.** Every `.cpp` under `cppsdk/src/dlc/` is M32+ work. M31 is pure test scaffolding.
- **Do not delete or rewrite M17 cppsdk headers.** `cppsdk/include/rti1516e/*.h` stays working through M34; M35 adds `[[deprecated]]`. M32+ adds the strict surface ALONGSIDE.
- **Do not break M17 cppsdk tests.** Every C++ commit verifies `cppsdk/tests/test_callbacks_integration.cpp` etc. still pass.
- **Do not break pysdk.** No edits under `pysdk/**` from C++ work. Cross-language fixtures consume pysdk; they don't modify it.
- **Do not invent enum scoping.** Per FR-DLC-16, every enum in `RTI/Enums.h` is unscoped — `enum X { ... }`, not `enum class X { ... }`. The scoped form changes federate access syntax and breaks source-compat.
- **Do not use `std::auto_ptr` literally.** It's removed in C++17. Use `rti1516e::auto_ptr<T>` (the alias defined in `RTI/SpecificConfig.h`); under default C++17 it's `std::unique_ptr<T>`.
- **Do not capture Pitch goldens until the EULA review clears.** See `docs/PITCH_GOLDEN_LICENSING.md`; M31 ships `expected.*.log` skeletons with `// TBD-pitch-capture` markers, marks the fixture's `WILL_FAIL TRUE`, and the real captures land in M32+ once licensing is signed off.
- **Do not pretend "the spec says" without a cite.** Every assertion has a `// §N.M` tag. Every golden line has a corresponding cite in the fixture's README. `scripts/check-spec-traceability.sh` enforces this in CI.

## 8. When to Stop and Ask

- Any time a spec sentence is ambiguous and Pitch's implementation reveals a hidden interpretation — flag via PR comment; the orchestrator decides whether to adopt Pitch's reading or override per spec (§5.2.2 tie-breaker).
- Any time `docs/DLC_DIVERGENCE_CATALOGUE.md` looks wrong on a row you're locking — propose a revision; orchestrator updates the catalogue.
- Any time a lockfile assertion's expected RED-ness is unclear (some catalogue rows are MAJOR but compile cleanly today — the lockfile signal should be precise about which axis it locks).
- Any time the `RTI/` and `rti1516e/` header trees disagree about a name (the spec path is canonical; the back-compat shim re-exports from spec path).
