# DLC Compliance Program — IEEE 1516.1-2010 strict C++ federate API

**Status:** DRAFT. Owner: orchestrator. Track parent for M31..M35.

How gorti's `cppsdk` becomes a strict implementation of the IEEE 1516.1-2010 **DLC** (Dynamic Link Compatible) C++ federate API — the surface every conformant 1516e RTI must expose so federate source compiles unchanged against any of them (Pitch pRTI, Portico, MAK RTI, gorti).

Companions: `docs/srs.md` (new §5.14 + §7.4), `docs/idd.md` (new §1.8), `docs/PITCH_PARITY.md` (every "Compatible" / "Diverging" row in the C++ column is closed by this track), `docs/RTI_CONFORMANCE_AUDIT.md` (sister track — RTI-side behavioral conformance), `docs/M28_DISPATCH_PLAN.md` and `docs/M29_DISPATCH_PLAN.md` (predecessor pysdk-side parity track), `docs/agent-c-pysdk.md` (Python SDK brief — companion for the new C++ brief `docs/agent-d-cppsdk.md`).

> **Scope note.** This program covers the **federate-side C++ API surface**. The
> **RTI-side behavioral semantics** (does the manager compute GALT correctly?
> does it run the ownership two-phase protocol per §7.3-7.6?) are tracked
> separately in `docs/RTI_CONFORMANCE_AUDIT.md` and run **in parallel** with
> M31-M35. Both tracks land together to claim "gorti is IEEE 1516.1-2010
> compliant" — neither alone is sufficient.

---

## 1. Why this program exists

A parity check on 2026-06-30 (see `/tmp/rti-parity/` artifacts) ran the same logical federate against gorti rtid and Pitch pRTI Free 5.5.10 and confirmed **byte-identical subscriber callback sequences** — semantically gorti behaves correctly. But the test had to use **two C++ source files**, not one. The reason: gorti's `cppsdk` ergonomically diverges from the IEEE 1516.1-2010 DLC C++ API at the construction site, the callback overload set, and the string-type / time-type / exception-throw surfaces.

Concretely the 2026-06-30 run had to bridge:

| # | Divergence | Spec | gorti today | Pitch (spec) |
|---|---|---|---|---|
| 1 | Ambassador construction | §10.6.1 | `rti1516e::RTIambassador amb; amb.connect(url)` | `auto_ptr<RTIambassador> amb = RTIambassadorFactory().createRTIambassador(); amb->connect(fed, HLA_IMMEDIATE, conn)` |
| 2 | Name string type | §10.6 | `std::string` | `std::wstring` |
| 3 | Callback overload count | §6, §7, §8 | 1 per callback | 3 per reflect/receive (no-time, with-time, with-retraction) |
| 4 | Time | §8 | `std::optional<double>` parameter | `LogicalTime const&` reference + factory |
| 5 | Exceptions | C.4 | `std::exception`-derived | `rti1516e::Exception` hierarchy + `RTI_THROW` exception spec macro |
| 6 | Headers / namespace | C.3 | `rti1516e/RtiAmbassador.h` (gorti) | `RTI/RTIambassador.h` (spec) |
| 7 | Encoding helpers | §10.5 | gorti hand-written `encode_uint64_be` | `HLAfloat64BE`, `HLAunicodeString`, `HLAfixedRecord`, `HLAvariableArray`, … |
| 8 | Time types | §8 | implicit doubles | `HLAfloat64Time`, `HLAinteger64Time` + intervals + factories |
| 9 | Resign action default | §4.18 | accepts `NO_ACTION` cleanly | strict `CANCEL_THEN_DELETE_THEN_DIVEST` required |

(Full row-by-row divergence catalogue: **`docs/DLC_DIVERGENCE_CATALOGUE.md`** — 153 rows. The expanded table above in §2 indexes by category and milestone.)

Total Pitch DLC headers: **30 files** (15 top-level `RTI/` + 9 `RTI/encoding/` + 6 `RTI/time/`), ~6100 lines. gorti today: 5 files, ~1440 lines. Surface gap ≈ 4–5×.

**Why this matters now:** the Python source-compat track (M25–M29) closed pysdk's Pitch-port surface — federates written against pysdk's `Rti1516eAmbassador` recompile against gorti. The C++ track is the analog and has higher stakes because the C++ federate API is **the** standardized API (1516.1 ships as a C++ DLC + Java HSL; Python is non-standard). Strict DLC compliance is what makes "gorti is a 1516-2010 RTI" actually true at the API surface, not just the service surface.

---

## 2. Catalogue of divergences

Full row-by-row catalogue: **`docs/DLC_DIVERGENCE_CATALOGUE.md`** (~340 lines, 153 divergence rows). Surveyed 2026-06-30 against Pitch pRTI Free 5.5.10 (`~/prti1516e/api/cpp/HLA_1516-2010/RTI/`).

| Category | Rows | BLOCKING | Spec § | Drives milestone |
|---|---|---|---|---|
| 1. Header layout & namespace | 7 | 4 | Annex A | M31 (stubs) → M32 (real) |
| 2. Construction & factory | 5 | 4 | §10 / Annex A | M32 |
| 3. Connect / federation lifecycle | 13 | 11 | §4 | M32 + M33 (save/restore) |
| 4. FederateAmbassador callbacks | 31 | 8 | §6, §7, §8 | M33 |
| 5. Enums | 10 | 5 | Enums.h | M31 + M32 |
| 6. Exceptions | 4 (covers ~120 classes) | 4 | Annex C | M33 |
| 7. Typed handles | 7 | 3 | §10.5 | M32 |
| 8. VariableLengthData | 3 | 1 | Annex A | M32 |
| 9. Time | 13 | 11 | §8 | M34 |
| 10. DDM | 8 | 7 | §9 | M33 |
| 11. Object management | 10 | 5 | §6 | M33 |
| 12. Ownership | 7 | 3 | §7 | M33 |
| 13. Support services | 15 | 5 | §10 | M33 |
| 14. Encoding helpers | 10 | 7 | Annex B | M34 |
| 15. Misc typedefs | 7 | 2 | Typedefs.h | M32 |
| 16. MOM | 1 | 0 | §11 | M35 |
| 17. Tag / threading | 2 | 1 | §6, §10 | M33 |
| **TOTAL** | **153** | **81** | | |

Severity split: 81 BLOCKING (federate source won't compile), 63 MAJOR (compiles but behavior diverges), 2 MINOR (edge cases), 7 COSMETIC (style only).

**Lockfile assertion count estimate: ~200.** Composite rows (e.g. 4.20 "3 overloads of reflectAttributeValues") each drive ≥3 assertions. The exact count is M31's deliverable — committed to `cppsdk/tests/dlc/lockfile/expected_red_count.txt`.

---

## 3. SRS deltas

### 3.1.0 C++ standard target (foundational decision)

**Target: C++17.** gorti's existing `cppsdk/CMakeLists.txt:29-31` already sets `CMAKE_CXX_STANDARD 17`. The strict DLC API ships under C++17 too.

**Consequence — `std::auto_ptr` is unavailable.** IEEE 1516.1-2010 was written in C++03 and the spec text returns `std::auto_ptr<T>` from `RTIambassadorFactory::createRTIambassador()`, `getTimeFactory()`, and `DataElement::clone()`. `auto_ptr` was deprecated in C++11 and **removed** in C++17 (`<memory>` no longer declares it). gorti cannot literally implement the spec text.

**Resolution:** `RTI/SpecificConfig.h` provides a transparent alias:
```cpp
namespace rti1516e {
#if defined(GORTI_DLC_USE_REAL_AUTO_PTR) && __cplusplus < 201703L
  template <typename T> using auto_ptr = std::auto_ptr<T>;
#else
  template <typename T> using auto_ptr = std::unique_ptr<T>;
#endif
}
```
Every factory signature returns `rti1516e::auto_ptr<T>`. Federates ported from Pitch that literally wrote `std::auto_ptr<RTIambassador>` in their source need a one-line `using std::auto_ptr = rti1516e::auto_ptr` adapter or to switch to `auto`. Federates that used `auto amb = factory.createRTIambassador()` (idiomatic) work unchanged.

This is documented as a **deliberate, opt-in deviation from the literal spec text** in `docs/PITCH_PARITY.md` "Pitch deviations from spec / C++17 forced deviations" section. It is not a compliance failure — the spec text references C++03 facilities that no longer exist in modern toolchains.

### 3.1 New functional-requirement group: §5.14 C++ Federate API (FR-DLC-*)

Append to `docs/srs.md` §5 (Functional Requirements):

```markdown
### 5.14 C++ Federate API DLC compliance (FR-DLC-*) — cut 4

- **FR-DLC-1** — Header layout matches IEEE 1516.1-2010 Annex A: every spec-mandated header
  exists under `RTI/`, `RTI/encoding/`, `RTI/time/` with the spec filenames (capital RTI).
  The legacy `rti1516e/` gorti path remains as a re-export shim for M17-era callers.
- **FR-DLC-2** — `RTIambassador` is pure-abstract per §10. `RTIambassadorFactory::createRTIambassador()`
  is the only legal construction path; direct construction yields a compile error. The factory returns
  `rti1516e::auto_ptr<RTIambassador>` (a `using auto_ptr<T> = std::unique_ptr<T>` alias defined in
  `RTI/SpecificConfig.h`, since `std::auto_ptr` was removed in C++17; spec-literal `std::auto_ptr`
  optionally re-aliased via `-DGORTI_DLC_USE_REAL_AUTO_PTR` under C++14). See §3.1.0 for the C++
  standard decision rationale.
- **FR-DLC-3** — `RTIambassador::connect` matches §4.2 signature exactly:
  `void connect(FederateAmbassador&, CallbackModel, std::wstring const& localSettingsDesignator)`,
  with the spec-mandated exception set (`ConnectionFailed`, `InvalidLocalSettingsDesignator`,
  `UnsupportedCallbackModel`, `AlreadyConnected`, `CallNotAllowedFromWithinCallback`,
  `RTIinternalError`).
- **FR-DLC-4** — All federate-facing strings use `std::wstring` per Annex A; the M17-era
  `std::string` API remains via a thin shim in `rti1516e/` but the spec-strict surface is
  wstring-only.
- **FR-DLC-5** — Every FederateAmbassador callback exposes all spec-defined overloads. Specifically:
  `discoverObjectInstance` 2 overloads, `reflectAttributeValues` 3 overloads (RO / TSO /
  TSO+retract), `receiveInteraction` 3 overloads, `removeObjectInstance` 3 overloads.
- **FR-DLC-6** — Every exception class enumerated in Annex C (~120 classes) is defined as a
  leaf of the `rti1516e::Exception` abstract base. The Pitch-style `RTI_EXCEPTION(Name)`
  macro is reused. `RTIinternalError` becomes a leaf, not the base.
- **FR-DLC-7** — Encoding helpers under `RTI/encoding/` cover the IEEE 1516.2 Annex B basic
  data types as `DataElement`-derived classes (not free functions): the 19 basic types
  enumerated in `BasicDataElements.h` plus the 5 composite encoders (`HLAfixedArray`,
  `HLAvariableArray`, `HLAfixedRecord`, `HLAvariantRecord`, `HLAopaqueData`).
- **FR-DLC-8** — Time types under `RTI/time/` cover `HLAfloat64Time/Interval/Factory` and
  `HLAinteger64Time/Interval/Factory`; abstract bases `LogicalTime`, `LogicalTimeInterval`,
  `LogicalTimeFactory` ship under `RTI/`. Federate calls `getTimeFactory()` (returns
  `rti1516e::auto_ptr<LogicalTimeFactory>`) or the static `LogicalTimeFactoryFactory::makeLogicalTimeFactory(implementationName)` to obtain a factory by name; the loaded factory then yields `LogicalTime` instances.
- **FR-DLC-9** — `RTI_THROW(...)` macro defined per `SpecificConfig.h`; every public method
  uses it for its exception specification.
- **FR-DLC-10** — All 6 spec `ResignAction` enum values accepted; `resignFederationExecution`
  requires a mandatory `ResignAction` argument (no default).
- **FR-DLC-11** — `CallbackModel { HLA_IMMEDIATE, HLA_EVOKED }` enum exposed per §4.2.
  Default behavior matches M29 (HLA_IMMEDIATE).
- **FR-DLC-12** — Time-management calls (`enableTimeRegulation`, `enableTimeConstrained`,
  `timeAdvanceRequest`, etc.) are **asynchronous** per §8; acks arrive on
  `timeRegulationEnabled` / `timeConstrainedEnabled` / `timeAdvanceGrant` callbacks.
  The M17-era synchronous-return semantics is dropped on the spec surface.
- **FR-DLC-13** — Object-management calls take a MANDATORY `VariableLengthData const& tag`
  per §6.10, §6.12, §6.14, §7.8 etc. (no default).
- **FR-DLC-14** — `CallNotAllowedFromWithinCallback` exception thrown when a federate
  re-enters the ambassador from within a callback.
- **FR-DLC-15** — `RoutingSpaceHandle` removed from the public DLC surface (HLA Evolved
  dropped routing spaces); gorti-internal uses move to a non-spec namespace.
- **FR-DLC-16** — `CallbackModel` is an **unscoped enum** per `Enums.h:21-25` (`enum CallbackModel { HLA_IMMEDIATE, HLA_EVOKED };`). The lockfile test must reject `enum class` (the scoped form changes federate access syntax from `HLA_IMMEDIATE` to `CallbackModel::HLA_IMMEDIATE` and breaks source-compat). All gorti enums in `RTI/Enums.h` match: `OrderType`, `TransportationType`, `ResignAction`, `SaveStatus`, `RestoreStatus`, `SaveFailureReason`, `RestoreFailureReason`, `ServiceGroup`, `SynchronizationPointFailureReason`.
- **FR-DLC-17** — ABI / SO-name versioning: shared libraries carry SO-version `librti1516e.so.N` matching `HLA_API_MAJOR_VERSION`; `RTI_EXPORT` macro from `RTI/SpecificConfig.h` controls visibility (`__declspec(dllexport)` Windows, `__attribute__((visibility("default")))` ELF). Cross-RTI binary swaps (gorti `.so` ↔ Pitch `.so`) work for any federate that compiled against either header set.
- **FR-DLC-18** — `std::wstring` encoding: per Annex A the spec is silent; gorti **normalizes to UTF-16** on the wire and treats `wchar_t` as host-defined (Linux/macOS = 32-bit UCS-4; Windows = 16-bit UCS-2). A `RTI/SpecificConfig.h` static_assert + runtime decoder ensures FOM names round-trip identically across hosts. This is a gorti commitment, not a spec mandate; documented as a Pitch-equivalent choice.
```

### 3.2 New external-interface requirement: §7.4 C++ API Shape (IR-CPPAPI-*)

```markdown
### 7.4 C++ Federate API Shape (IR-CPPAPI-*)

- **IR-CPPAPI-1** — Source compatibility: a federate written against IEEE 1516.1-2010 DLC C++
  headers (Pitch pRTI, Portico, MAK) compiles against gorti's cppsdk with NO source changes
  beyond the connect-string argument value (vendor connect syntax is implementation-defined).
- **IR-CPPAPI-2** — Header tree matches Annex C: `RTI/`, `RTI/encoding/`, `RTI/time/` plus
  the gorti-namespaced `rti1516e/` aliases for back-compat with M17 callers.
- **IR-CPPAPI-3** — Distribution: CMake `find_package(rti1516e)`, pkg-config, and a header-only
  reference distribution for federate builds that don't want to link the gorti gRPC runtime.
- **IR-CPPAPI-4** — Wire-protocol independence: the DLC API surface MUST NOT leak gRPC,
  protobuf, or absl types. Only spec-defined types and STL types appear in public headers.
```

### 3.3 New milestone rows in §10 (verification & exit criteria)

Append to §10.4 (Cut 3) or new §10.6 (Cut 4):

```markdown
| **M31** | Agent D (C++) | DLC lockfile tests (RED) — surface frozen, no impl | All ~200 lockfile assertions compile-fail or link-fail as designed; CMake test target `dlc_lockfile_red` exercises the failure mode; SRS §5.14 + §7.4 land; `docs/DLC_DIVERGENCE_CATALOGUE.md` committed |
| **M32** | Agent D | DLC headers, construction, handles, collections, VLD (~50/200 GREEN) | Catalogue sections 1, 2, 5, 7, 8, 15 flip RED→GREEN; Pitch chat sample's first 200 lines compile against gorti cppsdk |
| **M33** | Agent D | DLC callbacks + exceptions + ownership + DDM + obj-mgmt (~100/200 GREEN) | Catalogue sections 3, 4, 6 (~120 exception classes), 10, 11, 12, 13, 17 flip GREEN; cross-RTI parity test (`tests/parity/`) extends from 1 federate to 5 |
| **M34** | Agent D | Encoding helpers + time types (~40/200 GREEN) | Catalogue sections 9, 14 flip GREEN; encoding-vector tests pass with `HLA*` helpers replacing hand-encoders |
| **M35** | Agent D | MOM + back-compat shim deprecation + **IVCT-derived conformance subset** (last ~10/200 GREEN + conformance gate) | Catalogue section 16 flips GREEN; M17-era `rti1516e/RtiAmbassador.h` headers marked `[[deprecated]]`; Pitch chat sample compiles + runs end-to-end against gorti; **gorti rtid passes an IVCT-derived conformance subset** (specific test catalog selected during M35 W2 — see `docs/RTI_CONFORMANCE_AUDIT.md` §6 for honest scoping of IVCT integration options); `docs/dlc-spec-coverage.md` reports 100% spec-section coverage; `docs/MIGRATION_M17_TO_DLC.md` published |
```

---

## 4. IDD deltas — new §1.8 C++ DLC API

Append to `docs/idd.md`:

```markdown
### 1.8 C++ Federate Public API (`cppsdk/include/rti1516e/`, `cppsdk/include/RTI/`)

Per IR-CPPAPI-1..4 and FR-DLC-1..10. Strict implementation of IEEE 1516.1-2010 Annex C.

#### 1.8.1 Header tree

  RTI/                          [spec-mandated path]
    RTIambassador.h
    RTIambassadorFactory.h
    FederateAmbassador.h
    NullFederateAmbassador.h
    Exception.h
    Handle.h
    Typedefs.h
    VariableLengthData.h
    LogicalTime.h
    LogicalTimeFactory.h
    LogicalTimeInterval.h
    RangeBounds.h
    Enums.h
    SpecificConfig.h
    RTI1516.h                   [aggregate include]
    encoding/
      BasicDataElements.h       [HLAfloat64BE, HLAinteger32BE, HLAunicodeString, ...]
      DataElement.h             [abstract base]
      EncodingConfig.h
      EncodingExceptions.h
      HLAfixedArray.h
      HLAfixedRecord.h
      HLAopaqueData.h
      HLAvariableArray.h
      HLAvariantRecord.h
    time/
      HLAfloat64Time.h
      HLAfloat64TimeFactory.h
      HLAfloat64Interval.h
      HLAinteger64Time.h
      HLAinteger64TimeFactory.h
      HLAinteger64Interval.h
  rti1516e/                     [gorti back-compat aliases — re-export from RTI/]
    (M17 Cut-1..4 headers continue to work; new code prefers RTI/)

#### 1.8.2 Construction pattern (spec-exact)

(see RTIambassadorFactory section in M32 dispatch plan)

#### 1.8.3 Callback overload set

Each of `discoverObjectInstance`, `reflectAttributeValues`, `receiveInteraction`,
`removeObjectInstance` exposes all spec-mandated overloads. The full enumeration
lives in `docs/M32_DISPATCH_PLAN.md` §2.

#### 1.8.4 Exception hierarchy

(see Exception.h walkthrough in M33 dispatch plan)

#### 1.8.5 Encoding helpers

(see BasicDataElements.h walkthrough in M34 dispatch plan)

#### 1.8.6 Time types

(see HLAfloat64Time walkthrough in M34 dispatch plan)
```

---

## 5. Test-first plan (per user direction)

Three layers, each implemented in M31 as RED tests:

### 5.1 Signature lockfile tests (compile-only)

`cppsdk/tests/dlc/lockfile/test_<surface>_signatures.cpp` — each test file uses
`static_assert` + `decltype` + `std::is_same_v` to assert the exact symbols, types,
and signatures the spec mandates. Compile failure = lockfile broken.

Example skeleton:

```cpp
#include <RTI/RTIambassador.h>
#include <type_traits>

static_assert(std::is_class_v<rti1516e::RTIambassador>);
static_assert(std::is_polymorphic_v<rti1516e::RTIambassador>);

// §4.2 connect signature
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::RTIambassador&>().connect(
        std::declval<rti1516e::FederateAmbassador&>(),
        std::declval<rti1516e::CallbackModel>(),
        std::declval<std::wstring const&>())),
    void>);

// §10 — RTIambassadorFactory exists, returns rti1516e::auto_ptr<RTIambassador>
// (rti1516e::auto_ptr = std::unique_ptr under C++17 per §3.1.0)
static_assert(std::is_same_v<
    decltype(std::declval<rti1516e::RTIambassadorFactory&>().createRTIambassador()),
    rti1516e::auto_ptr<rti1516e::RTIambassador>>);

// FR-DLC-16 — CallbackModel is UNSCOPED (no `enum class`); access syntax is bare
static_assert(std::is_same_v<decltype(rti1516e::HLA_IMMEDIATE), rti1516e::CallbackModel>);
```

**Counting contract** (revised per second-review): the assertion-grep approach is fragile
across compilers (clang/GCC/MSVC produce different `error:` line counts for the same
`static_assert`; follow-on errors inflate the count). Instead, the lockfile is structured
as **one TU per assertion**, each registered as a CTest target with the
`WILL_FAIL TRUE` property. Counting failing TUs (not error lines) is portable and exact.
The lockfile passes when `ctest -L lockfile` reports `M of N tests passed (M=0 in RED,
M=N in fully-GREEN)`.

Count target: ~200 TUs total (one per `docs/DLC_DIVERGENCE_CATALOGUE.md` row, with
composite rows multiplied). Per-header sub-directories under `cppsdk/tests/dlc/lockfile/`
group them logically; CMake fans out via `file(GLOB)` so adding a TU doesn't require a
CMakeLists edit.

**Stub strategy** (revised per second-review): empty namespace stubs would cause
`'RTIambassador' is not a member of 'rti1516e'` (one error per TU, stopping the
compiler before reaching the `static_assert`), defeating the per-TU failure signal.
Stubs are **forward declarations** instead:

```cpp
// cppsdk/include/RTI/RTIambassador.h (M31 stub form)
#pragma once
namespace rti1516e {
class FederateAmbassador;
class RTIambassador {
  // intentionally empty — M32 adds the methods
};
class RTIambassadorFactory {};
}  // namespace rti1516e
```

This way each lockfile TU compiles past the include and the `static_assert(is_same_v<...>)`
actually fires on the type mismatch (which is the lockfile signal we want).

### 5.2 Conformance + parity (single layer, two run modes)

(Restructured per second-review — the previous §5.2/§5.3 split was artificial:
parity is "fixture run + a second-leg Pitch run", not a separate layer.)

`cppsdk/tests/dlc/conformance/` houses 25 federate fixtures covering §4-§11 (full
catalog in `docs/M31_DISPATCH_PLAN.md §2.2`). Each fixture has:
- `federate*.cpp` — federate source (single file, no `#ifdef`)
- `federation.fom.xml` — spec-strict FOM
- `expected.*.log` — canonical event-sequence golden(s)
- `test_<name>.cpp` — gtest driver
- `README.md` — one-paragraph scenario + spec § citation

The driver runs in one of two modes:

| Mode | Trigger | Build | Behavior |
|---|---|---|---|
| **gorti-only** | always | links against gorti's `librti1516e.a` | starts `bin/rtid`, runs fixture, diffs vs golden |
| **gorti+Pitch (parity)** | `PRTI_HOME` set + `ctest -L parity` | additionally compiles the same `federate*.cpp` against `~/prti1516e/api/cpp/HLA_1516-2010/` headers; links `librti1516e64.so` | starts Pitch CRC, runs fixture, captures Pitch log, canonicalizes both, diffs `gorti_log == pitch_log` |

Parity is a *second leg* of the same fixture run, not a separate test directory.
The previous `tests/parity/` top-level directory collapses into per-fixture `parity/`
subdirs under each conformance fixture.

#### 5.2.1 Canonicalization rules (for the diff step)

Raw subscriber logs differ across RTIs in places that are NOT bugs:

| Source of difference | Canonicalization |
|---|---|
| Handle integers | Replace `handle=<N>` with `handle=<H>` (the 2026-06-30 smoke proved this works) |
| Wall-clock timestamps | Strip; not in spec-mandated callback args anyway |
| RO event interleaving within the same logical time | Sort within each LBTS bucket. Spec §6 only mandates causal order for RO — within-bucket order is RTI-defined |
| TSO event order within the same logical time | DO NOT sort. Spec §8 mandates strict TSO order; mismatch here is a bug |

The canonicalization function lives in `cppsdk/tests/dlc/conformance/_harness/normalize.py` and is shared between gorti-only and parity modes. Test PASS iff `canonicalize(gorti_log) == canonicalize(pitch_log)` (parity) or `canonicalize(gorti_log) == load_golden()` (gorti-only).

#### 5.2.2 Tie-breaker: spec wins over Pitch

Pitch is a *vendor*, not the spec. Two worked examples:

- **Pitch deviation example:** Pitch's `evokeCallback(0.0, 0.0)` blocks ~10ms (its scheduler quantum) before returning. IEEE 1516.1 §10.41 mandates "approximate minimum time" = 0 means "return as soon as a callback fires or immediately". gorti returns immediately per spec. Parity test for evoke-call timing would fail if it expected Pitch's behavior — the test is marked `SkipIfPitchDeviation("Pitch §10.41 timing quirk: blocks scheduler quantum even on 0.0")` in the parity driver, with a `// §10.41` cite.
- **gorti bug example:** If gorti emits `discoverObjectInstance` before `synchronizationPointAchieved` returns but Pitch emits it after, IEEE 1516.1 §4.13 + §6.9 say discover ordering relative to sync is RTI-defined (callback queue order) — but if gorti's wall-clock timing diverges from determinism guarantees (NFR-DET-1), that's a gorti bug, not a spec-permitted divergence. Parity test fails; M31-M35 fixes gorti.

**Enforcement (spec-traceability lint).** Every fixture's `expected.*.log` must carry, in
its README, the spec sentences each line of the golden enforces. Goldens authored from a
one-shot Pitch run are inherently Pitch-shaped — without a spec citation per non-obvious
event the reviewer can't tell whether a given line is "what the spec says" or "what Pitch
happened to do". `scripts/check-spec-traceability.sh` is a CI lint: greps each
`README.md` for `// §N.M` markers covering every event in `expected.*.log`. CI fails if
any event lacks a spec cite. This lint is **mandatory in M31, not optional**.

Documented Pitch deviations live in `docs/PITCH_PARITY.md` "Pitch deviations from spec"
section.

### 5.3 Spec-coverage matrix (auto-generated)

`docs/dlc-spec-coverage.md` (auto-generated by `scripts/gen-spec-coverage.sh`).
Rows: IEEE 1516.1 §-sections (§4.2, §4.3, ..., §11.X). Columns: covered-by-lockfile,
covered-by-fixture (gorti mode), covered-by-fixture (parity mode), covered-by-audit. Each
cell links to the test files. Regenerated on every push; CI fails if any §-row has zero
coverage across all four columns. Catches "we didn't notice §6.16 had any tests" cases.

Cells populated from in-source `// §N.M` tags grep'd across all test files plus
`tests/conformance/rti/` audit probes. `scripts/gen-spec-coverage.sh` is **mandatory in M31**, not in optional/polish bucket.

### 5.4 Test-development order (the "tests first" deliverable)

M31 ships THREE LAYERS as RED tests:
1. **Lockfile** — ~200 per-TU `static_assert` files (W1)
2. **Conformance fixtures + parity** — 25 fixtures with 2-mode driver (W2)
3. **RTI audit** state-machine trace harness (W3 hook into `RTI_CONFORMANCE_AUDIT.md`)

No implementation code lands in M31.

Pseudocode for M31 acceptance:

```bash
# Lockfile: all TUs must FAIL (one TU per assertion, WILL_FAIL=TRUE).
# `M of N tests passed` where M=0 in pure RED, growing as M32+ lands impl.
$ ctest -L lockfile --output-on-failure
0 of 200 tests passed   # M31 RED baseline (all failing is success)

# Conformance fixtures fail-to-link in gorti-only mode (no impl symbols yet).
$ ctest -L conformance --output-on-failure 2>&1 | grep "undefined reference" | wc -l
~80

# Parity mode: SKIPS (no PRTI_HOME) or BUILD_FAILED (gorti DLC headers stubbed).
$ ctest -L parity --output-on-failure
conformance::helloworld_pubsub::parity: SKIPPED (PRTI_HOME not set)
```

Per the phasing table above, M32 turns ~50 assertions GREEN, M33 another ~100,
M34 ~40, M35 the residual ~10. Every milestone after M31 measures progress as
`(GREEN_count / TOTAL)` reported by `scripts/check-milestones.sh check_dlc`.

### 5.5 M17 → DLC migration recipe

Every federate built against M17's `cppsdk/include/rti1516e/` headers needs a
~4-line migration when M35 lands and the strict surface deprecates the M17 shim.

**Before (M17-era):**
```cpp
#include "rti1516e/RtiAmbassador.h"
#include "rti1516e/FederateAmbassador.h"

rti1516e::RTIambassador amb;
amb.connect("grpc://127.0.0.1:8080");
amb.joinFederationExecution("alice", "demo");
auto h = amb.getObjectClassHandle("Vehicle");
amb.resignFederationExecution();
```

**After (DLC-strict):**
```cpp
#include <RTI/RTIambassadorFactory.h>
#include <RTI/NullFederateAmbassador.h>
#include <RTI/Enums.h>

class MyFed : public rti1516e::NullFederateAmbassador { /* overrides */ };

auto factory = rti1516e::RTIambassadorFactory();
auto amb = factory.createRTIambassador();          // rti1516e::auto_ptr
MyFed fed;
amb->connect(fed, rti1516e::HLA_IMMEDIATE,
             L"crcAddress=127.0.0.1:8989");        // wstring, address vector
amb->joinFederationExecution(L"alice", L"demo");   // wstring
auto h = amb->getObjectClassHandle(L"Vehicle");    // wstring; returns ObjectClassHandle class
amb->resignFederationExecution(rti1516e::CANCEL_THEN_DELETE_THEN_DIVEST);
```

Documented as a single page `docs/MIGRATION_M17_TO_DLC.md` (drafted in M35 W4)
with sed-style hints for the 5 most-common patterns. Existing examples
(`examples/cpp-pitch-smoke/publisher.cpp`, all `cppsdk/tests/`) get a `-old` suffix
and stay buildable; `-strict` variants compile against the DLC surface as
demonstration.

### 5.6 Pitch licensing for golden files

Goldens are generated by running fixtures against a Pitch CRC. The Pitch pRTI Free
EULA (clickwrap-installed; full text at `~/prti1516e/.install4j/EULA.txt`) restricts
**use of Pitch software** but typically does not restrict **output produced by
federate code linked against Pitch headers**. Captured event log lines like
`SUB: DISCOVER class=Vehicle name=car-1` are federate-program output, not Pitch
software output.

**Risk mitigation:**
- Audit the EULA's "output ownership" clause before M31 W2 starts (TASK-350 prereq).
- Goldens are scrubbed of any Pitch-banner / copyright lines that Pitch's federate
  framework may inject. The 2026-06-30 smoke's `grep "^SUB:" pitch.sub.log` pattern
  is the template.
- If a EULA review finds restrictions: switch goldens to **hand-authored from spec
  text only** (slower, less accurate, but unambiguously gorti-owned).

This is a real legal question; treat as a **prerequisite for TASK-350** with explicit
sign-off, not an afterthought.

### 5.7 Out-of-scope: Go federate SDK

gorti ships a Go federate SDK (`rti/pkg/federate/`, exercised by M14 and M23 Go
SDK tests). IEEE 1516.1-2010 standardizes C++ and Java DLC APIs only; Go is not
in the standard. The DLC Compliance Program explicitly **does not** cover Go SDK
compliance. Go federates continue to work via gorti's gRPC wire protocol and
gorti's Go-idiomatic API, which is not 1516.1 DLC and not claimed as such.

If Go DLC equivalence becomes a goal later, that's a sibling track (call it the
"Go SDK consistency program") that's outside this program's scope.

### 5.8 C-1 (lean MVP / solo developer) scope acknowledgment

SRS §8 C-1 reads: *"Solo developer; lean MVP approach."* This program's total
estimated effort is ~3 months single-owner for the C++ track + ~10 weeks parallel
for the RTI audit. That is **not lean** by any honest accounting.

The justification: strict 1516.1 DLC compliance is an inherently large surface
(spec is ~500 pages, ~30 headers, ~120 exception classes, ~150 methods). There
is no "MVP DLC" — partial compliance breaks the federate-source-portability claim.
The right scope-reduction options are:

1. **Stop at the API surface** (M31-M33), accept that M34 (encoding/time) and M35
   (MOM + IVCT) are deferrable. Federates that don't use HLA-encoding helpers or
   strict time types still port cleanly. Realistic shorter horizon.
2. **Drop the parity gate** to advisory. Compliance is measured against the spec
   directly via lockfile + goldens, not via Pitch agreement. Saves the
   golden-authoring time but loses the bake-off evidence.
3. **Accept that this program is multi-quarter and not C-1-compatible.** Treat
   it as a deliberate cut-4 commitment.

Recommend option (3) with explicit re-baselining of C-1 to "lean MVP for the RTI
core; compliance work is deliberately heavy". This is a scope question, not a
technical question — decided at program kickoff.

---

## 6. Milestone phasing

```
                    DLC Compliance Program
                            │
   ┌────────────────────────┼────────────────────────┐
   │                                                 │
   M31  LOCKFILE (RED)                               │
        Tests scaffold; no impl                      │
        Surface FROZEN at spec                       │
                            │                        │
   M32  Construction + Handles + Collections (GREEN) │
        RTIambassadorFactory + auto_ptr              │
        std::wstring throughout                      │
        Handle.h, Typedefs.h, RangeBounds.h          │
        Enums.h                                      │
                            │                        │
   M33  Callbacks + Exceptions (GREEN)               │
        NullFederateAmbassador with all overloads    │
        Exception hierarchy (~50 classes)            │
        RTI_THROW macro                              │
                            │                        │
   M34  Encoding + Time (GREEN)                      │
        encoding/BasicDataElements.h                 │
        encoding/HLAfixedRecord, variableArray, ...  │
        time/HLAfloat64Time, integer64Time, …        │
                            │                        │
   M35  Resign + Save/Restore + MOM (GREEN)          │
        Strict ResignAction semantics                │
        §4.8-4.14 save/restore polish                │
        §11 MOM service group ambassador            │
                            ▼
                    DLC compliance DONE
                    All ~200 assertion / ~140 lockfile assertions GREEN
                    Pitch chat sample compiles against gorti
                    `tests/parity/` regression suite GREEN
```

W1 (M32) → W2 (M33) → W3 (M34) → W4 (M35) are mostly **sequential** because each
milestone GREENs a slice of M31's lockfile. Within a milestone, tasks parallelize
across header files.

Estimated effort (single C++ owner):
- M31: ~2 weeks (test scaffold is large but mechanical)
- M32: ~3 weeks (handles + factory + std::wstring touches everything)
- M33: ~2 weeks (callbacks are mechanical once the overload pattern is established)
- M34: ~3 weeks (encoding helpers are the biggest unwritten LOC)
- M35: ~2 weeks (mostly tightening existing surfaces)

**Total: ~3 months single-owner, ~6 weeks with two C++ owners.**

---

## 7. Agent ownership

This program introduces a new agent brief: `docs/agent-d-cppsdk.md` (skeleton in
M31 W3). The brief mirrors `docs/agent-c-pysdk.md`:

- **Owned paths**: `cppsdk/include/RTI/`, `cppsdk/include/rti1516e/` (back-compat
  aliases), `cppsdk/src/dlc/`, `cppsdk/tests/dlc/`, `tests/parity/`.
- **Forbidden paths**: `proto/`, `rti/`, `pysdk/`.
- **Conformance**: same encoding vectors as Python (FR-ENC-2).
- **Pre-reading**: this program doc + `docs/idd.md §1.8` + IEEE 1516.1-2010 §C.

---

## 8. Scoping decisions

Decided 2026-06-30:

1. **Milestone numbering** — DLC track is **M31-M35**. Pysdk-M30 (MOM ambassador / getUpdateRate, pencilled in `docs/M28_DISPATCH_PLAN.md` §1.2 non-goals) ships first as M30; DLC track follows.
2. **Pitch CI integration** — `tests/parity/` runs **local-only**, gated on `PRTI_HOME`. GitHub Actions skips the Pitch leg. Developers run locally before merge. Avoids Pitch license-in-CI question entirely.

Still open (deferrable past M31):

3. **Java SDK forking.** The Java DLC API (`hla.rti1516e.*`) is the other standardized federate API. Do we plan a sister "Java DLC compliance" track or stay C++-only? Recommend defer until C++ track ships.
4. **Back-compat horizon for M17 shim.** When the strict DLC surface lands, gorti's M17-era `rti1516e::RTIambassador amb; amb.connect(string)` direct-construct form stays as a re-export but is marked deprecated. Cut-off date? Recommend: `[[deprecated]]` at M35, remove at the next major (v2.0).
