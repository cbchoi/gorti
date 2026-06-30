# M31 Dispatch Plan — DLC C++ surface lockfile (RED tests scaffold)

How the orchestrator dispatches the M31 tasks (TASK-335..360) to maximize parallel sub-agent throughput.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md`, `docs/DLC_COMPLIANCE_PROGRAM.md` (parent track), `docs/PITCH_PARITY.md`, `docs/M28_DISPATCH_PLAN.md`/`docs/M29_DISPATCH_PLAN.md` (predecessor pysdk parity track), `docs/agent-d-cppsdk.md` (NEW — created in W3).

---

## 1. Goal & non-goals

### Goal

First milestone of the DLC C++ Compliance Program (`docs/DLC_COMPLIANCE_PROGRAM.md`). Land **failing tests** that lock the IEEE 1516.1-2010 DLC C++ API surface at the spec. NO implementation code lands in M31 — every lockfile/conformance test is RED by design. Subsequent milestones (M32–M35) turn slices GREEN.

Four parts:

1. **Signature lockfile tests** (W1). One `.cpp` per spec-mandated header (30 files) plus per-method micro-files for the high-arity surfaces. Uses `static_assert` + `decltype` + `std::is_same_v` to fail compilation when the surface drifts from the spec. Target ~200 assertions across ~140 TUs (~3500 LOC), one assertion per `docs/DLC_DIVERGENCE_CATALOGUE.md` row (composite rows drive multiple).
2. **Conformance fixtures** (W2). **27 federate fixtures** (25 spec-section + 1 threading + 1 cross-language) covering §4-§11 behavioral surface (full enumeration in §2.2 below) plus their canonical event-sequence golden files. Test driver runs the federate against rtid and diffs vs the golden file. RED in M31 because the impl doesn't exist; test infrastructure is what M31 actually lands.
3. **Parity harness** (W3). `tests/parity/` top-level — generalizes the 2026-06-30 ad-hoc parity smoke into a regression suite that runs each fixture against gorti AND Pitch CRC and diffs the subscriber logs. Pitch leg is opt-in via `PRTI_HOME` env.
4. **Docs + CI wiring + agent brief** (W4). Land `docs/agent-d-cppsdk.md`, the SRS § 5.14 + 7.4 deltas, the IDD § 1.8 skeleton, `docs/dlc-spec-coverage.md` autogeneration, the `scripts/check-milestones.sh check_m31` probe, and the `cppsdk/tests/dlc/lockfile/` CMake target wiring.

W1 and W2 parallelize across files. W3 depends on W2's fixtures landing (parity reuses them). W4 picks up after W1–W3 settle.

### Non-goals

- **No implementation code.** No new headers in `cppsdk/include/RTI/`. No new sources in `cppsdk/src/`. M31 is pure test scaffolding.
- **No deletion or rewrite of existing M17 cppsdk headers.** They keep working; M32+ adds the strict surface alongside.
- **No pysdk changes.** Python-side Pitch parity is M25–M29's domain.
- **No Java SDK.** Deferred per `docs/DLC_COMPLIANCE_PROGRAM.md §8`.
- **No CI activation of the parity harness against Pitch.** Pitch license restrictions; harness runs locally only.
- **No CMake changes to the M17 build path.** New targets only; nothing touched on the green-build path until M32.

### Why now

- The 2026-06-30 parity smoke established the C++-side divergence catalogue (`docs/DLC_COMPLIANCE_PROGRAM.md §1`). With M28 (pysdk typed handles) closed and M29 (HLA_EVOKED) in flight, the C++ track is the last major Pitch-port surface left.
- The user's direction: "develop test cases, first." TDD per `docs/TDD.md`. The 1516.1 DLC spec is fully specified down to method signatures — RED tests can be written directly from the spec without any design decisions; this milestone is mechanical.
- Locking the surface BEFORE writing the impl prevents a long-running C++ rewrite from drifting. Every M32+ commit reports `(GREEN/TOTAL)` lockfile progress.

---

## 2. Surface design

### 2.1 Lockfile test layout (W1)

```
cppsdk/tests/dlc/lockfile/
├── CMakeLists.txt
├── core/
│   ├── test_rtiambassador_signatures.cpp        (§10.6.1 — class shape, ~30 assertions)
│   ├── test_rtiambassador_connect.cpp           (§4.2)
│   ├── test_rtiambassador_federation_mgmt.cpp   (§4.3-4.7)
│   ├── test_rtiambassador_declaration_mgmt.cpp  (§5)
│   ├── test_rtiambassador_object_mgmt.cpp       (§6)
│   ├── test_rtiambassador_ownership.cpp         (§7)
│   ├── test_rtiambassador_time.cpp              (§8)
│   ├── test_rtiambassador_ddm.cpp               (§9)
│   ├── test_rtiambassador_handle_services.cpp   (§10.2)
│   ├── test_rtiambassador_mom.cpp               (§11)
│   ├── test_rtiambassadorfactory.cpp            (§10.6.1)
│   ├── test_federateambassador_signatures.cpp   (§6, §7, §8 callback overload set)
│   ├── test_nullfederateambassador.cpp          (default empty bodies)
│   └── test_callbackmodel_enum.cpp              (§4.2)
├── types/
│   ├── test_handle.cpp                          (§10.5 — per typed handle)
│   ├── test_typedefs.cpp                        (AttributeHandleSet etc.)
│   ├── test_variable_length_data.cpp
│   ├── test_range_bounds.cpp
│   └── test_enums.cpp                           (OrderType, TransportationType, …)
├── exceptions/
│   ├── test_exception_base.cpp
│   ├── test_exception_hierarchy.cpp             (every spec-named exception)
│   └── test_rti_throw_macro.cpp
├── encoding/
│   ├── test_dataelement.cpp                     (abstract base)
│   ├── test_basicdataelements.cpp               (HLAfloat64BE, HLAinteger32BE, etc.)
│   ├── test_hlaunicodestring.cpp
│   ├── test_hlafixedrecord.cpp
│   ├── test_hlafixedarray.cpp
│   ├── test_hlavariablearray.cpp
│   ├── test_hlavariantrecord.cpp
│   ├── test_hlaopaquedata.cpp
│   └── test_encoding_exceptions.cpp
└── time/
    ├── test_logicaltime_base.cpp                (abstract)
    ├── test_logicaltime_factory_base.cpp
    ├── test_logicaltime_interval_base.cpp
    ├── test_hlafloat64time.cpp
    ├── test_hlafloat64interval.cpp
    ├── test_hlafloat64timefactory.cpp
    ├── test_hlainteger64time.cpp
    ├── test_hlainteger64interval.cpp
    └── test_hlainteger64timefactory.cpp
```

Per-file conventions:
- Each `.cpp` is a **compile-only** translation unit. CMake target is an `OBJECT` library — the lockfile assertions trigger at compile time before any link step.
- Top of file: include the spec-mandated header by spec path (`#include <RTI/RTIambassador.h>`, NOT `#include "rti1516e/RtiAmbassador.h"`).
- Body: ~10-30 `static_assert` lines per file. One `static_assert` per spec sentence the file locks.
- Tag every assertion with the spec section it enforces (`// §4.2`, `// §10.5`).

Example skeleton (`test_rtiambassador_connect.cpp`):

```cpp
// Lockfile: RTIambassador::connect signature per IEEE 1516.1-2010 §4.2.
// Fails to compile until RTI/RTIambassador.h exports the spec signature.

#include <RTI/RTIambassador.h>
#include <RTI/FederateAmbassador.h>
#include <RTI/Enums.h>  // CallbackModel
#include <type_traits>
#include <string>

namespace {

using rti1516e::CallbackModel;
using rti1516e::FederateAmbassador;
using rti1516e::RTIambassador;
using rti1516e::HLA_IMMEDIATE;        // CRITICAL: unscoped enumerator per FR-DLC-16

// §4.2 — connect(FederateAmbassador&, CallbackModel, wstring const& localSettings)
// NOTE: bare HLA_IMMEDIATE, NOT CallbackModel::HLA_IMMEDIATE. The spec defines
// CallbackModel as an unscoped enum (Pitch Enums.h:21-25); scoped-form access
// would require `enum class`, which breaks source-compat with Pitch federates.
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().connect(
        std::declval<FederateAmbassador&>(),
        HLA_IMMEDIATE,
        std::declval<std::wstring const&>())),
    void>);

// FR-DLC-16 lockfile: CallbackModel is unscoped + enumerator values match spec
static_assert(std::is_same_v<decltype(HLA_IMMEDIATE), CallbackModel>);
static_assert(static_cast<int>(HLA_IMMEDIATE) == 0);
static_assert(static_cast<int>(rti1516e::HLA_EVOKED) == 1);

// §4.2 — the 2-arg overload (no localSettings) exists
static_assert(std::is_same_v<
    decltype(std::declval<RTIambassador&>().connect(
        std::declval<FederateAmbassador&>(),
        HLA_IMMEDIATE)),
    void>);

// §4.2 — throws ConnectionFailed (we check the exception class exists)
static_assert(std::is_class_v<rti1516e::ConnectionFailed>);
static_assert(std::is_base_of_v<rti1516e::Exception, rti1516e::ConnectionFailed>);

}  // namespace
```

### 2.2 Conformance fixture layout (W2)

27 fixtures organized by spec section (25 spec-section + 1 threading + 1 cross-language). Each fixture is one directory under
`cppsdk/tests/dlc/conformance/` containing `federate*.cpp`, `federation.fom.xml`,
`expected.*.log` goldens, and `test_<name>.cpp` (gtest driver). The directory
naming convention is `<area>_<scenario>`.

| # | Fixture | Spec § | Scenario |
|---|---|---|---|
| **§4 Federation Mgmt** ||||
| 1 | `fm_create_join_resign` | §4.5-4.10 | Single federate creates federation, joins, resigns with each of 6 ResignAction values |
| 2 | `fm_list_executions` | §4.7-4.8 | `listFederationExecutions` returns expected vector via callback |
| 3 | `fm_sync_full` | §4.7-4.15 | 3-federate sync point; all reach `synchronizationPointAchieved`; `federationSynchronized` fires |
| 4 | `fm_sync_subset_with_failure` | §4.14-4.15 | Sync with `FederateHandleSet` subset; one fails (calls `synchronizationPointAchieved(label, successfully=false)`); `federationSynchronized` reports `failedToSyncSet` |
| 5 | `fm_save_restore_roundtrip` | §4.16-4.32 | Federation save mid-execution → resign → re-join → request restore → byte-identical state |
| **§5 Declaration Mgmt** ||||
| 6 | `dm_pub_sub_active_passive` | §5.6-5.8 | `subscribeObjectClassAttributes(active=false)` does NOT trigger `startRegistrationForObjectClass`; toggling to active does |
| 7 | `dm_unpublish_whole_vs_attrs` | §5.3 | `unpublishObjectClass` (whole class) vs `unpublishObjectClassAttributes` (subset) produce distinct manager-side state |
| **§6 Object Mgmt** ||||
| 8 | `om_helloworld_pubsub` | §6.6-6.13 | Vehicle + Honk pub/sub (the 2026-06-30 smoke, generalized) |
| 9 | `om_reserve_multi_atomic` | §6.5 | `reserveMultipleObjectInstanceName` with one colliding name; ALL fail; colliding-names set reported |
| 10 | `om_delete_object_tso` | §6.14 | TSO delete; subscriber's `removeObjectInstance` fires with the right `LogicalTime` |
| 11 | `om_local_delete` | §6.16 | `localDeleteObjectInstance` removes locally without notifying other federates |
| 12 | `om_request_attribute_update_class` | §6.19 | Late-joiner subscribes, calls `requestAttributeValueUpdate` on class; publishers' `provideAttributeValueUpdate` callbacks fire |
| 13 | `om_request_attribute_update_instance` | §6.19 | Same as #12 but by instance handle |
| 14 | `om_message_retraction` | §8.22 (object path) | Publisher sends TSO interaction with retraction handle → `retract(handle)` → subscriber's `requestRetraction` fires before the original `receiveInteraction` would deliver |
| **§7 Ownership** ||||
| 15 | `own_negotiated_divest_two_phase` | §7.3-7.6 | Two-federate negotiated divest → assumption → acquisition → confirmation; state machine §7 figure 7.1 |
| 16 | `own_acquire_if_available_race` | §7.9 | Two federates simultaneously call `attributeOwnershipAcquisitionIfAvailable`; exactly one succeeds, the other gets `attributeOwnershipUnavailable` |
| 17 | `own_release_request_denied` | §7.11-7.12 | Owner refuses release via `attributeOwnershipReleaseDenied` |
| 18 | `own_query_via_callbacks` | §7.17-7.18 | `queryAttributeOwnership(object, attr)` is void; result arrives on `informAttributeOwnership` / `attributeIsNotOwned` / `attributeIsOwnedByRTI` |
| **§8 Time Mgmt** ||||
| 19 | `tm_ner_pair` | §8.8 | Regulator + constrained federate; NER cycle produces correct grants |
| 20 | `tm_tar_tara_fqr_nmra` | §8.8-8.12 | All 4 advance primitives produce correct grants under identical scenarios |
| 21 | `tm_lookahead_change` | §8.19 | `modifyLookahead` propagates to GALT correctly |
| 22 | `tm_tso_ordering` | §8.13-8.15 | 3 federates send at same logical time; subscriber receives in spec-canonical order |
| **§9 DDM** ||||
| 23 | `ddm_region_overlap` | §9.8-9.12 | Publisher's region partially overlaps subscriber's; updates filtered correctly |
| 24 | `ddm_region_mod_in_flight` | §9.5-9.6 | Subscriber modifies region while publisher is sending; `attributesInScope`/`OutOfScope` fire on boundary changes |
| **§11 MOM** ||||
| 25 | `mom_federation_lifecycle` | §11 | Subscriber to `HLAobjectRoot.HLAmanager.HLAfederation`; observes `HLAfederatesInFederation` updates as federates join/resign |
| **Threading / re-entrancy (§10)** ||||
| 26 | `threading_callback_reentry` | §10 + §17.2 | Federate calls `amb->updateAttributeValues()` from inside `reflectAttributeValues` callback → expects `CallNotAllowedFromWithinCallback` exception. Spec §10 prohibits ambassador re-entry from callbacks. |
| **Cross-language interop** ||||
| 27 | `xlang_python_cpp_pubsub` | §6 | Python publisher (via pysdk M28 typed-handle path) + C++ subscriber (via cppsdk DLC path) in the same gorti federation. Verifies that the DLC C++ surface and the pysdk surface produce identical wire behavior. NOTE: this fixture is **gorti-only** (no parity leg) since Pitch's Python binding isn't shipped in Free 5.5.10. |

Directory shape per fixture:

```
cppsdk/tests/dlc/conformance/<name>/
├── federate*.cpp           (1 to 3 source files, single-source each; no #ifdef)
├── federation.fom.xml      (HLA-Evolved-strict — includes <switches>, well-formed dimensions)
├── expected.*.log          (one golden per federate role)
├── test_<name>.cpp         (gtest driver: start rtid → run federates → diff)
└── README.md               (one-paragraph scenario description + spec § cite)
```

Shared utility code:

```
cppsdk/tests/dlc/conformance/_harness/
├── rtid_runner.h           (RAII wrapper around bin/rtid)
├── log_diff.h              (handle-int normalization + LBTS-bucket sort for RO; strict for TSO; per docs/DLC_COMPLIANCE_PROGRAM.md §5.3.1)
└── golden_loader.h         (loads expected.*.log + provides diff helpers)
```

Each fixture's federate source uses ONLY spec-defined APIs — `#include <RTI/RTIambassador.h>` etc. Fixtures are designed to fail-to-compile in M31 (missing headers), fail-to-link in M32 (missing impl symbols), and pass progressively in M33-M35 as the impl lands. **Goldens are hand-authored in M31** from a one-shot run against a known-good RTI (Pitch) — if Pitch and gorti later disagree, the tie-breaker is the spec (per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.2`).

Test driver pseudocode:

```cpp
TEST(Conformance, helloworld_pubsub) {
  TempDir tmp;
  RtidRunner rtid(tmp.path() / "rtid.log");
  // Run pub + sub, capture canonical logs.
  auto pub_log = run_federate("./helloworld_pub", "--url", rtid.url());
  auto sub_log = run_federate("./helloworld_sub", "--url", rtid.url());
  // Normalize handles, diff vs golden.
  EXPECT_EQ(normalize(pub_log), load_golden("expected.publisher.log"));
  EXPECT_EQ(normalize(sub_log), load_golden("expected.subscriber.log"));
}
```

Golden file format — one canonical event per line, handle integers replaced by `<H>`:

```
PUB: CONNECT
PUB: CREATE federation=rti-parity
PUB: JOIN federate=pub
PUB: PUBLISH class=Vehicle attributes=[Position,Velocity]
PUB: PUBLISH interaction=Honk
PUB: REGISTER class=Vehicle name=car-1 handle=<H>
PUB: UPDATE name=car-1 Position=42.000000 Velocity=7.000000
PUB: SEND class=Honk Volume=5
PUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST
```

### 2.3 Parity harness layout (W3)

```
tests/parity/
├── README.md
├── CMakeLists.txt
├── run_parity.sh         (orchestrator: gorti round + pitch round + diff)
├── pitch_build.sh        (compiles fixtures against ~/prti1516e/api/cpp/HLA_1516-2010)
├── gorti_build.sh        (compiles fixtures against cppsdk)
└── (reuses cppsdk/tests/dlc/conformance/*/federate.cpp as the source under test)
```

Test flow per fixture:
1. Compile fixture against gorti's spec-aliased headers → `gorti_<fixture>`
2. Compile SAME source against Pitch's headers → `pitch_<fixture>`
3. Start gorti rtid; run `gorti_<fixture>`; capture canonical log.
4. Start Pitch CRC (skip if `PRTI_HOME` unset); run `pitch_<fixture>`; capture canonical log.
5. Diff the two subscriber canonical logs.

Test PASS iff (3) succeeds AND ((4) skipped OR (4)+(5) succeed).

This generalizes `/tmp/rti-parity/` (2026-06-30 ad-hoc smoke) into a checked-in regression suite. The Pitch leg gates on `PRTI_HOME` so the suite is still useful on machines without Pitch.

### 2.4 Build-target naming (W4)

```cmake
# cppsdk/tests/dlc/lockfile/CMakeLists.txt
add_library(dlc_lockfile OBJECT
  core/test_rtiambassador_signatures.cpp
  core/test_rtiambassador_connect.cpp
  # ...all ~140 .cpp files...
)
# Built but not linked into anything. Expected to FAIL TO COMPILE in M31.
# In M32+ when the surface lands, this target is GREEN.
target_include_directories(dlc_lockfile PRIVATE ${CMAKE_SOURCE_DIR}/include)

# Optional: a meta-test that catalogs which assertions are GREEN.
add_executable(dlc_lockfile_status status_report.cpp)
# In M31 this is hand-edited to record "0 / 200 GREEN".
```

CMake option `-DDLC_LOCKFILE_RED_EXPECTED=ON` (default ON in M31) wraps the
add_library in `try_compile` so the M31 build itself doesn't fail — instead
the failed-compile is reported as the test result. M32+ flips this to OFF
when GREEN is expected.

### 2.5 New top-level dir: `cppsdk/include/RTI/` (forward-declaration stubs, W4)

(Revised per second-review.) Empty namespace stubs would cause every lockfile TU to
fail with `'RTIambassador' is not a member of 'rti1516e'` — one error per TU, before
the `static_assert` even fires. We lose per-assertion failure signals.

Instead, M31 lands **forward-declaration stubs** at the spec paths. Each stub
declares the spec-mandated types as incomplete classes so `static_assert(is_same_v<...>)`
instantiates and fails on the right axis (signature mismatch, not symbol absence):

```cpp
// cppsdk/include/RTI/RTIambassador.h (M31 stub form)
#pragma once
namespace rti1516e {
class FederateAmbassador;        // forward decl — fleshed out in M33
class RTIambassador {
  // intentionally empty — methods land in M32-M35
};
class RTIambassadorFactory {};
class ConnectionFailed;          // exception forward decl — bodied in M33
class Exception;                 // base — bodied in M33
}  // namespace rti1516e
```

`RTI/Enums.h` ships M31 with the spec's unscoped-enum declarations (required for
the FR-DLC-16 lockfile assertion to fire on the type alias) and empty enumerators:

```cpp
// cppsdk/include/RTI/Enums.h (M31 stub form)
#pragma once
namespace rti1516e {
enum CallbackModel { HLA_IMMEDIATE = 0, HLA_EVOKED = 1 };  // unscoped per FR-DLC-16
enum OrderType { RECEIVE = 1, TIMESTAMP = 2 };
enum ResignAction {
  UNCONDITIONALLY_DIVEST_ATTRIBUTES = 0, DELETE_OBJECTS, CANCEL_PENDING_OWNERSHIP_ACQUISITIONS,
  DELETE_OBJECTS_THEN_DIVEST, CANCEL_THEN_DELETE_THEN_DIVEST, NO_ACTION
};
// ... more enums as needed for lockfile parse
}
```

Once stubs let the lockfile parse, each `static_assert` lockfile fires on its
own axis (signature mismatch, return-type mismatch, etc.) — which is the per-TU
failure signal the milestone needs.

---

## 3. Acceptance criteria (exit gate)

1. **Lockfile target compiles to a known FAILED state.** `cmake --build build --target dlc_lockfile` exits non-zero with ≥200 distinct `static_assert` failures (one per row in `docs/DLC_DIVERGENCE_CATALOGUE.md`, with composite rows multiplying). The exact count is recorded in `cppsdk/tests/dlc/lockfile/expected_red_count.txt` and `scripts/check-milestones.sh check_m31` verifies it matches.
2. **All 27 conformance fixtures fail-to-link.** `cmake --build build --target dlc_conformance` exits non-zero with `undefined reference to rti1516e::*` errors (impl doesn't exist yet). 27 fixture dirs present, each with `federate*.cpp`, `federation.fom.xml`, hand-authored `expected.*.log` goldens (~50-75 golden files), `test_*.cpp`, `README.md`.
3. **Parity harness exists + skips cleanly without Pitch.** `ctest -L parity --output-on-failure` reports `SKIPPED (PRTI_HOME unset)` for the Pitch leg and `BUILD_FAILED (gorti DLC headers stubbed)` for the gorti leg. `tests/parity/normalize.py` exists with canonicalization per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.1`.
4. **All 30 spec header stubs land at `cppsdk/include/RTI/`.** `find cppsdk/include/RTI -name '*.h' | wc -l` ≥ 33.
5. **`docs/agent-d-cppsdk.md` lands.** Brief in the same shape as `docs/agent-c-pysdk.md`, with the DLC track scoped as the agent's primary deliverable.
6. **`docs/srs.md` §5.14 (FR-DLC-1..15) + §7.4 (IR-CPPAPI-1..4) land verbatim from `docs/DLC_COMPLIANCE_PROGRAM.md §3`.**
7. **`docs/idd.md` §1.8 lands** (skeleton — subsections fill in M32–M35).
8. **`docs/PITCH_PARITY.md` updated** with a new "C++ DLC track" section and a "Pitch deviations from spec" section per the tie-breaker rule (`docs/DLC_COMPLIANCE_PROGRAM.md §5.3.2`).
9. **`docs/dlc-spec-coverage.md` auto-generates and lists ≥40 spec §-sections** with at least one of (lockfile / fixture / parity / audit) covering each. CI fails if any §-row has zero coverage.
10. **`docs/RTI_CONFORMANCE_AUDIT.md` referenced from `docs/srs.md` §10.6** as the sister track to the DLC program.
11. **`CHANGELOG-MASTERPLAN.md` M31 entry landed** with `(0/200) GREEN` baseline.
12. **`scripts/check-milestones.sh check_m31` reports `M31: DONE (N/N)`** via 11 probes (see W4 TASK-359).
13. **No M17 cppsdk build regression.** Existing `cppsdk/tests/` (test_callbacks_integration etc.) all stay GREEN.
14. **No pysdk regression.** Full M25–M29 spec suite passes unchanged.

---

## 4. Wave structure

```
                              M31 START
                                  │
   ┌──────────────────────────────┼──────────────────────────────┐
   │                              │                              │
   │   W1   Lockfile tests (~140 files, parallelizable)          │
   │        Sub-agent fan-out by file group (core/types/         │
   │        exceptions/encoding/time). Each sub-agent owns       │
   │        one subdir + its CMakeLists hookup.                  │
   │        Output: cppsdk/tests/dlc/lockfile/**                 │
   │                              │                              │
   │   W2   Conformance fixtures (27 fixtures, parallelizable)   │
   │        One sub-agent per fixture. Each writes               │
   │        federate.cpp + golden logs + driver. Federate        │
   │        sources include <RTI/*.h> (spec path).               │
   │        Output: cppsdk/tests/dlc/conformance/**              │
   │                              │                              │
   │   W3   Parity harness (depends on W2)                       │
   │        Single sub-agent — generalizes 2026-06-30 smoke.     │
   │        Reuses W2's federate.cpp sources.                    │
   │        Output: tests/parity/**                              │
   │                              │                              │
   │   W4   Docs + CI + agent brief                              │
   │        - docs/agent-d-cppsdk.md (NEW)                       │
   │        - docs/srs.md §5.14 + §7.4 patches                   │
   │        - docs/idd.md §1.8 skeleton                          │
   │        - cppsdk/include/RTI/*.h stubs (30 files)            │
   │        - scripts/check-milestones.sh check_m31              │
   │        - CHANGELOG-MASTERPLAN.md M31 row                    │
   │                              │                              │
                                  ▼
                       M31 DONE per program §6
                       Lockfile = RED with N assertions
                       M32 GREEN-flip work begins
```

W1, W2 fully parallelize across files / fixtures. W3 needs W2's federate sources. W4 lands after W1–W3 settle (the agent brief references the test layout established by W1–W3).

### 4.1 Effort estimate (honest)

Initial scoping suggested ~2 weeks. **Revised: ~4-6 weeks single-owner**, based on
second-review push-back. Breakdown:

| Wave | Deliverables | Realistic estimate |
|---|---|---|
| W1 | ~200 per-TU lockfile assertions + 30 forward-declaration stubs + Enums.h M31 form | ~1.5 weeks (mostly mechanical, parallelizable across catalogue sections) |
| W2 | 27 fixtures × (federate.cpp + FOM + golden(s) + driver + README) ≈ 130-160 files. Each fixture needs **design** (which spec sentences it locks), **C++ implementation** using the strict API, and **golden capture** by one-shot run against Pitch (TASK-350). | **2-3 weeks** if Pitch licensing (TASK-363) clears immediately; **+1-2 weeks** if EULA needs work. The 25-spec-section fixtures alone are ~25 days of focused work. |
| W3 | Per-fixture parity mode wiring + canonicalization library + normalize.py | ~1 week (mechanical once W2's directory layout exists) |
| W4 | SRS/IDD patches + agent-d brief + 11 probes + spec-coverage script + traceability lint + CHANGELOG + Pitch EULA review | ~1 week |

**Total: 5-7 weeks single-owner.** With one C++ developer + one docs-focused developer running in parallel (W4 + W2 reviews), could compress to ~4 weeks calendar.

Critical-path item: **TASK-350 (hand-author goldens against Pitch)**. This is the program's hidden bottleneck — each of 27 fixtures needs an independent Pitch run capturing 2-3 canonical logs. Cannot start until TASK-363 (Pitch EULA review) clears.

The original "~2 weeks" estimate underestimated W2 by ~3× and missed the Pitch EULA prerequisite entirely. The 5-7 week estimate is what the plan now signs up to.

---

## 5. Tasks

### W1 — Signature lockfile tests (parallel across subdirs)

- **TASK-335**: `cppsdk/tests/dlc/lockfile/core/` — RTIambassador signatures + the 8 service-group locks (federation_mgmt, declaration_mgmt, object_mgmt, ownership, time, ddm, handle_services, mom). ~14 files. Includes the M29-landed `CallbackModel` enum lock.
- **TASK-336**: `cppsdk/tests/dlc/lockfile/types/` — Handle.h, Typedefs.h, VariableLengthData.h, RangeBounds.h, Enums.h. 5 files.
- **TASK-337**: `cppsdk/tests/dlc/lockfile/exceptions/` — Exception base + ~50 spec exceptions + RTI_THROW macro. 3 files but the exception_hierarchy.cpp is large (one `static_assert` per exception class).
- **TASK-338**: `cppsdk/tests/dlc/lockfile/encoding/` — DataElement abstract + BasicDataElements + 5 composite encoders + encoding exceptions. 9 files.
- **TASK-339**: `cppsdk/tests/dlc/lockfile/time/` — LogicalTime + LogicalTimeFactory + Interval abstracts + 6 concrete types. 9 files.
- **TASK-340**: `cppsdk/tests/dlc/lockfile/CMakeLists.txt` — wires all subdirs as `OBJECT` libraries. Adds `-DDLC_LOCKFILE_RED_EXPECTED=ON` default.

### W2 — Conformance fixtures (parallel across fixtures, 27 total)

Per §2.2's full enumeration. Group by spec section for sub-agent parallelism:

- **TASK-341 (§4 Federation Mgmt — 5 fixtures)**: `fm_create_join_resign`, `fm_list_executions`, `fm_sync_full`, `fm_sync_subset_with_failure`, `fm_save_restore_roundtrip`.
- **TASK-342 (§5 Declaration Mgmt — 2 fixtures)**: `dm_pub_sub_active_passive`, `dm_unpublish_whole_vs_attrs`.
- **TASK-343 (§6 Object Mgmt — 7 fixtures)**: `om_helloworld_pubsub`, `om_reserve_multi_atomic`, `om_delete_object_tso`, `om_local_delete`, `om_request_attribute_update_class`, `om_request_attribute_update_instance`, `om_message_retraction`.
- **TASK-344 (§7 Ownership — 4 fixtures)**: `own_negotiated_divest_two_phase`, `own_acquire_if_available_race`, `own_release_request_denied`, `own_query_via_callbacks`.
- **TASK-344b (Threading + Cross-lang — 2 fixtures)**: `threading_callback_reentry` (catalogue 17.2 — re-entrancy + `CallNotAllowedFromWithinCallback`), `xlang_python_cpp_pubsub` (Python publisher via pysdk + C++ subscriber via cppsdk DLC; gorti-only).
- **TASK-345 (§8 Time Mgmt — 4 fixtures)**: `tm_ner_pair`, `tm_tar_tara_fqr_nmra`, `tm_lookahead_change`, `tm_tso_ordering`.
- **TASK-346 (§9 DDM — 2 fixtures)**: `ddm_region_overlap`, `ddm_region_mod_in_flight`.
- **TASK-347 (§11 MOM — 1 fixture)**: `mom_federation_lifecycle`.
- **TASK-348**: `_harness/` — RtidRunner, log_diff (handle normalization + LBTS-bucket sort for RO per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.1`), golden_loader. Shared utility code.
- **TASK-349**: `cppsdk/tests/dlc/conformance/CMakeLists.txt` — wires 27 fixtures as `add_test`s. Sets `WILL_FAIL` (CMake property) for M31; M32+ removes per fixture as impl lands.
- **TASK-350**: Hand-author 25 sets of expected `*.log` goldens by one-shot run against Pitch CRC (the known-good reference). Per fixture: 1-3 log files = ~50-75 goldens total. Tag any Pitch-deviates-from-spec cases in `docs/PITCH_PARITY.md`.

### W3 — Parity harness (depends on W2)

- **TASK-351**: `tests/parity/CMakeLists.txt` + `run_parity.sh` + `pitch_build.sh` + `gorti_build.sh`. Build targets per fixture for both RTIs (25 × 2 = 50 binaries). Uses ctest's `LABELS` for opt-in via `ctest -L parity`. Pitch version pin: requires `PRTI_VERSION` to match `5.5.10` band (per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3` pin); other versions fail-loud.
- **TASK-352**: `tests/parity/README.md` — Pitch install instructions, `PRTI_HOME`/`PRTI_VERSION` env, expected behavior with and without Pitch. Reference §5.3.2 tie-breaker (spec wins over Pitch).
- **TASK-353**: `tests/parity/normalize.py` — canonicalization rules per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.1` (handle integers → `<H>`, RO events sort within LBTS bucket, TSO events strict).

### W4 — Docs, agent brief, surface stubs, CI hooks

- **TASK-354**: `cppsdk/include/RTI/` — 30 spec header stubs (1-line `#pragma once + namespace rti1516e {}`). Auto-generate via a sub-task script if the LOC budget is tight.
- **TASK-355**: `docs/agent-d-cppsdk.md` — NEW brief mirroring `docs/agent-c-pysdk.md`. Identifies the DLC track as the agent's deliverable.
- **TASK-356**: `docs/srs.md` — §5.14 (FR-DLC-*) and §7.4 (IR-CPPAPI-*) patches per `docs/DLC_COMPLIANCE_PROGRAM.md §3`. §10.6 (Cut 4) gains the M31-M35 milestone rows.
- **TASK-357**: `docs/idd.md` — §1.8 skeleton per `docs/DLC_COMPLIANCE_PROGRAM.md §4`. Subsections 1.8.2-1.8.6 are placeholders pointing forward to M32-M35 dispatch plans.
- **TASK-358**: `docs/PITCH_PARITY.md` — new "C++ DLC track" section linking to the program doc; new "Pitch deviations from spec" section per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.2`; mark the C++ column "owed-to-M35" in the divergence table.
- **TASK-359**: `scripts/check-milestones.sh` — `check_m31()` probe with 11 probes:
  1. `cppsdk/tests/dlc/lockfile/` exists with ≥140 `.cpp` files containing ≥200 assertions.
  2. `cppsdk/tests/dlc/conformance/` has **27 fixture dirs**.
  3. Each fixture dir has `federate*.cpp`, `federation.fom.xml`, `expected.*.log`, `test_*.cpp`, `README.md`.
  4. `tests/parity/` exists with `run_parity.sh` + `normalize.py`.
  5. `cppsdk/include/RTI/` has ≥30 `.h` files.
  6. `docs/agent-d-cppsdk.md` exists + non-empty.
  7. `docs/srs.md` mentions `FR-DLC-1` through `FR-DLC-15` + `IR-CPPAPI-1`.
  8. `docs/idd.md` mentions `§1.8`.
  9. `docs/dlc-spec-coverage.md` auto-generated + lists ≥40 spec §-sections with non-empty coverage cells.
  10. `docs/RTI_CONFORMANCE_AUDIT.md` referenced from `docs/srs.md` §10.6.
  11. `CHANGELOG-MASTERPLAN.md` has the M31 row with `(0/200) GREEN`.
- **TASK-360**: `CHANGELOG-MASTERPLAN.md` — M31 entry, with the (0/200) baseline.
- **TASK-361 (PROMOTED FROM OPTIONAL)**: `scripts/gen-spec-coverage.sh` — auto-generate `docs/dlc-spec-coverage.md` from `// §N.M` tags. Rows: IEEE 1516.1 §-sections. Columns: lockfile, fixture (gorti), fixture (parity), audit. Fails CI if any row has zero coverage across all four columns. **Required in M31** because acceptance criterion #9 depends on it.
- **TASK-362 (PROMOTED FROM OPTIONAL)**: `scripts/check-spec-traceability.sh` — for every fixture, grep its `README.md` for `// §N.M` markers covering every event in `expected.*.log`. CI fails if any event lacks a spec cite. **Required in M31** because §5.2.2 "spec wins over Pitch" enforcement depends on this lint.
- **TASK-363**: Pitch EULA review for golden-file ownership (§5.6 prerequisite). Document outcome in `docs/MIGRATION_M17_TO_DLC.md` or a dedicated `docs/PITCH_GOLDEN_LICENSING.md`. **Required before TASK-350 (W2 goldens) starts.**
- **TASK-364**: `docs/agent-d-cppsdk.md` content **draft** (not just creation) — mirror agent-c length and depth; identifies owned/forbidden paths, conformance contract, milestone deliverables.

### W5 (genuinely optional polish, can defer)

- **TASK-365**: GitHub Actions: add a `dlc-lockfile-red` job that runs `ctest -L lockfile` and asserts the failing-TU count matches `expected_red_count.txt`.
- **TASK-366**: A `make dlc-status` Makefile target that runs `ctest -L lockfile` and prints `(GREEN/TOTAL)`. M32+ uses this for progress reports.
- **TASK-367**: `docs/MIGRATION_M17_TO_DLC.md` initial skeleton (full content lands in M35 W4 per `docs/DLC_COMPLIANCE_PROGRAM.md §5.5`).

---

## 6. Test plan

Every lockfile and fixture is itself a test. The "test plan" in the M31 sense is: how do we test that the test scaffold is correctly RED?

| Test | Asserts |
|---|---|
| `ctest -L lockfile` | All ~200 per-TU lockfile tests fail (each with `WILL_FAIL TRUE`); count matches `expected_red_count.txt`. M31 baseline `0 of 200 passed`. |
| `ctest -L conformance` | All 27 fixtures fail-to-link with `undefined reference` (no impl symbols yet). |
| `ctest -L parity` | Reports SKIPPED (no PRTI_HOME) OR BUILD_FAILED (gorti DLC stubs). |
| `cppsdk/include/RTI/*` headers | All 30 present, with forward-declaration stubs (NOT empty namespace). |
| `check_m31` probe | All 11 probes pass. |
| `scripts/check-spec-traceability.sh` | All fixture goldens have `// §N.M` cites in their READMEs (mandatory M31 lint). |
| `scripts/gen-spec-coverage.sh` | `docs/dlc-spec-coverage.md` regenerates with ≥40 spec §-sections covered. |
| `cppsdk/tests/test_callbacks_integration` etc. (M17) | Still GREEN. M31 does NOT regress M17. |
| `pysdk/tests/spec/m25..m29/**` | Still GREEN. M31 does NOT regress pysdk. |
| `docs/srs.md`, `docs/idd.md` | mypy / markdownlint pass. |

A meta-test in W4 verifies the RED count itself:

```cpp
// cppsdk/tests/dlc/lockfile/test_red_count.cpp
// Runs as part of the meta-test target. Reads expected_red_count.txt and
// confirms compile output of dlc_lockfile produces exactly that many
// static_assert failures.
```

---

## 7. Migration impact

**Zero** for any existing federate, library, or test.

- M17-era `cppsdk/include/rti1516e/*.h` headers continue to work. M31 adds `cppsdk/include/RTI/*.h` alongside as empty stubs.
- M17 federate source (`examples/cpp-pitch-smoke/publisher.cpp`, all cppsdk tests) compiles unchanged.
- pysdk untouched.
- Go SDK untouched.

The only visible artifact: a new CMake target `dlc_lockfile` that's expected to fail compilation. CI must distinguish "expected red" from "broken build" — TASK-357 wires this.

M32 begins the GREEN-flip work; M32 PRs report `(N/200) GREEN` deltas.

---

## 8. M31 row append target (W4 — for reference)

```markdown
| **M31** | Agent D | DLC C++ surface lockfile (RED tests scaffold) | ~200 `static_assert`-based lockfile assertions across ~140 TUs landed under `cppsdk/tests/dlc/lockfile/` (one per row in `docs/DLC_DIVERGENCE_CATALOGUE.md`, with composite rows multiplying); 5 conformance fixtures landed under `cppsdk/tests/dlc/conformance/`; parity harness at `tests/parity/` ready to run against Pitch CRC; ~33 empty `cppsdk/include/RTI/*.h` stubs; SRS §5.14 (FR-DLC-1..15) + §7.4 (IR-CPPAPI-1..4) landed; IDD §1.8 skeleton landed; `docs/agent-d-cppsdk.md` agent brief landed; no impl code — every test is RED by design. Closes the first wave of `docs/DLC_COMPLIANCE_PROGRAM.md`. **DONE 2026-MM-DD** — see `docs/M32_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
```
