# Pitch / Portico / MAK API parity notes

`pysdk/rti1516e/standard.py::Rti1516eAmbassador` is the "Layer 2"
sync ambassador mirroring IEEE 1516.1-2010 §6 `RTIambassador`. It
is what federates ported from a commercial RTI (Pitch HLA Evolved,
Portico, MAK RTI) should bind against.

This document records the places where gorti's Layer-2 surface
diverges from a strict reading of the spec, what's compatible
anyway, and what's explicitly out of scope. Last updated for M28.

## Compatible (gorti behaves as Pitch would)

- IEEE 1516.1 §4 Federation Management, §5 Declaration Management,
  §6 Object Management (including the §6.1-6.5 reservation flow per
  M26 F and §6.30-6.31 instance handle services per M27 C),
  §7 Ownership Management, §8 Time Management, §9 DDM, §10.2 handle
  services. Method names follow Pitch's camelCase
  (`publishObjectClassAttributes`,
  `unconditionalAttributeOwnershipDivestiture`, etc.).
- FOM XML — gorti's parser accepts the IEEE 1516.2 standard form
  Pitch ships. Cross-language handle alignment is locked.
- mTLS + OIDC client authentication (M14) — bearer-token / cert
  args on `connect()`.
- M27 Phase B: methods that take class / attribute / parameter
  identifiers accept either **`int`** (Pitch-style FOM handle, e.g.
  from `getObjectClassHandle`) **or `str`** (FOM name, pysdk
  convenience). Mixed lists are supported.
- **M28** — typed handle classes (`ObjectClassHandle`, etc. — 9 total)
  and typed collections (`AttributeHandleSet`,
  `AttributeHandleValueMap`, etc. — 8 total) under `rti1516e.*`;
  ambassador factory accessors (`getAttributeHandleSetFactory` etc. —
  6 total). Federate code written against the Pitch typed-handle API
  compiles unchanged.

## Method-shape divergence table

| Spec method | Pitch shape | gorti shape | Status |
|---|---|---|---|
| `publishObjectClassAttributes` | `(ObjectClassHandle, AttributeHandleSet)` | `(int \| str \| ObjectClassHandle, list[int \| str \| AttributeHandle] \| AttributeHandleSet)` | Compatible (M28) |
| `subscribeObjectClassAttributes` | `(ObjectClassHandle, AttributeHandleSet)` | `(int \| str \| ObjectClassHandle, list[int \| str \| AttributeHandle] \| AttributeHandleSet)` | Compatible (M28) |
| `registerObjectInstance` | `(ObjectClassHandle[, String])` | `(int \| str \| ObjectClassHandle, str \| None)` | Compatible (M28) |
| `updateAttributeValues` | `(ObjectInstanceHandle, Map<AttributeHandle, byte[]>[, LogicalTime])` | `(int \| ObjectInstanceHandle, dict[int \| str \| AttributeHandle, bytes] \| AttributeHandleValueMap, float \| None)` | Compatible (M28) |
| `sendInteraction` | `(InteractionClassHandle, Map<ParameterHandle, byte[]>, ...)` | `(int \| str \| InteractionClassHandle, dict[int \| str \| ParameterHandle, bytes] \| ParameterHandleValueMap, ...)` | Compatible (M28) |
| `getObjectClassHandle(name)` | `(String) -> ObjectClassHandle` | `(str) -> ObjectClassHandle` | Compatible (M25 B, M28) |
| `getAttributeHandle` | `(ObjectClassHandle, String) -> AttributeHandle` | `(int \| str \| ObjectClassHandle, str) -> AttributeHandle` | Compatible (M25 B, M28) |
| `getObjectInstanceHandle(name)` | runtime query | wire RPC + SDK accessor | Compatible (M27 C) |
| `reserveObjectInstanceName` | async callback | async event + Pitch callback slot | Compatible (M26 F) |
| `evokeCallback` | strict HLA_EVOKED buffering | cheap yield-to-loop | **Diverging (see below)** |
| `enableCallbacks` / `disableCallbacks` | toggles dispatch | toggles dispatch (buffered when off) | Compatible (M27 C) |

## Diverging (compatible API, different runtime behavior)

### `evokeCallback` / `evokeMultipleCallbacks` (§10.4) — M26 Phase E

Pitch defaults to `HLA_EVOKED`: callbacks fire ONLY when the
federate calls `rtiAmb.evokeCallback(min, max)`. gorti is
HLA_IMMEDIATE-flavored: callbacks fire from a background pump
as soon as events arrive.

`Rti1516eAmbassador.evokeCallback` is provided for API
compatibility. It yields to the loop for `approx_min_time`
seconds (up to `approx_max_time` if no callback fires in the
minimum), and returns `True` iff a callback fired in the window.

**The cheap implementation does not buffer callbacks unless
`disableCallbacks()` is in effect.** A Pitch federate that relies
on "no callbacks fire outside `evokeCallback`" will see callbacks
at unexpected times under gorti unless it calls `disableCallbacks()`
when not actively evoking. Workaround pattern:

```python
amb.disableCallbacks()
while running:
    do_my_work()
    amb.evokeMultipleCallbacks(0.0, 0.1)  # callbacks dispatch here
amb.enableCallbacks()
```

If a ported federate requires the strict HLA_EVOKED semantics
without the explicit disable/evoke discipline, raise an issue —
buffered-drain mode would land in a future milestone.

### Typed-handle int-equality (§10.6) — M28 W1

gorti's typed handle classes (`ObjectClassHandle`, `AttributeHandle`,
…) subclass `int`. This preserves bare-int back-compat — code that
asserts `handle == 5` or passes a handle to `int(...)` keeps working
unchanged — but it has a behavioral consequence vs Pitch's typed
handles:

```python
>>> from rti1516e import ObjectClassHandle, AttributeHandle
>>> ObjectClassHandle(5) == AttributeHandle(5)
True   # gorti: equal (both are int(5))
       # Pitch: False (distinct typed handle classes)
```

This is a deliberate dual-accept concession — it lets mixed-typed
and bare-int callers interoperate in the same federate. Code that
needs strict cross-type distinction must use `isinstance()`:

```python
def assert_object_class(h):
    assert isinstance(h, ObjectClassHandle), f"want ObjectClassHandle, got {type(h)}"
```

Documented in `pysdk/rti1516e/handles.py::_StrongHandle` docstring
and locked by
`pysdk/tests/spec/m28/test_typed_handle_surface.py::test_spec_m28_typed_handle_int_equality_documented_concession`.

### C++ SDK parity (M17 Cut-2 + Cut-3)

The `rti1516e::` C++ SDK mirrors the same shape as the Python
`Rti1516eAmbassador` and inherits the same cheap-evoke divergence:

```cpp
amb.disableCallbacks();
while (running) {
  do_my_work();
  amb.evokeMultipleCallbacks(0.0, 0.1);  // callbacks dispatch here
}
amb.enableCallbacks();
```

As of M17.22 (Cut-4) the C++ `evokeCallback` is strict
at-most-one: it dispatches EXACTLY ONE buffered callback per
call and returns `true` iff more events remain queued.
`evokeMultipleCallbacks` remains a `tickCallback` alias
(drain-everything semantics). Federate code that wants the Pitch
"one callback at a time" discipline should use `evokeCallback`
in a `while` loop:

```cpp
while (amb.evokeCallback(0.0, 0.1)) { /* more queued */ }
```

### Outbox post-join window — closed in M27 A

Pre-M27, a service-group RPC fired immediately after
`joinFederation` returned could have its callback dropped because
rtid's outbox didn't have a channel for the federate yet. Fixed
in M27 Phase A via server-side pre-binding from
`OnFederateJoined`. Federates should NO LONGER need the
`await asyncio.sleep(0.1)` workaround that the M26 tests originally
shipped with.

## Out of scope

- **Wire-format interop**: gorti uses gRPC; Pitch uses a proprietary
  RTI-internal TCP/multicast protocol. A gorti rtid cannot
  federate-join a Pitch RTI server or vice versa. See `docs/srs.md:255`
  and `docs/srs.md:326`.
- **Cross-RTI handle normalization** (§10): `normalizeFederateHandle`
  / `normalizeServiceGroup` are spec methods for federating across
  RTI implementations. Not applicable.
- **`getUpdateRateValueForAttribute`** (§10.2): rate-throttling is
  not implemented; per-class declared rates in the FOM are
  informational.
- **MOM delegate methods on `Rti1516eAmbassador`**: §11 MOM
  introspection is available via `fed.mom.*` accessors but not yet
  surfaced as ambassador methods. Tracked for a future milestone
  (M27 Phase D — deferred).
- **`getAvailableDimensionsFor*`** (§10.2 advanced dimension queries):
  not implemented. Dimensions are enumerated via the FOM parser at
  load time; runtime enumeration RPCs are tracked for a future cut.

## Verification

See `examples/pitch-shape-smoke/` for a federate written using
ONLY the Pitch-style ambassador methods (no reach-around into
`sdk.ownership.*` / `sdk.ddm.*`). It exercises handle lookup →
publish-by-handle → reserve-name → register-by-handle → update →
send-interaction → sync point → resign. The smoke test at
`pysdk/tests/spec/m26/test_pitch_shape_smoke.py` runs it
end-to-end against rtid.

`pysdk/tests/spec/m27/test_handle_keyed_api.py` adds the
cross-federate Pitch-style scenario (publisher + subscriber both
using handles) including Discover, Reflect, and Receive callback
verification.

`pysdk/tests/spec/m25/test_ambassador_surface.py` is the lockfile:
every Pitch-style method the ambassador promises is asserted to
exist as a callable with `self` as first parameter.

---

## C++ DLC track (M31-M35)

The C++ SDK has its own parity track — the IEEE 1516.1-2010 DLC C++
federate API. Parent program: `docs/DLC_COMPLIANCE_PROGRAM.md`. 153
divergence rows enumerated in `docs/DLC_DIVERGENCE_CATALOGUE.md`.
M31 (closed 2026-07-01) lands the RED test scaffold
(`docs/M31_DISPATCH_PLAN.md`); M32 (in progress 2026-07) flips
catalogue §1+§2+§5+§7+§8+§15 GREEN; M33-M35 flip the remaining
slices per `docs/DLC_COMPLIANCE_PROGRAM.md §6`.

Where the Python track aims for **shape parity** with Pitch's
`Rti1516eAmbassador` ergonomics, the C++ track aims for **strict
spec compliance** — gorti's `cppsdk/include/RTI/*.h` headers are
byte-shape-identical to Pitch's reprint of Annex A/B/C at the type
level. Every divergence is a lockfile assertion under
`cppsdk/tests/dlc/lockfile/` (~200 `static_assert` checks).

The parity-mode leg of each conformance fixture
(`cppsdk/tests/dlc/conformance/<fixture>/parity/`) bake-offs the same
federate source against Pitch CRC. Opt-in via `PRTI_HOME`; off by
default. Pin: Pitch pRTI Free 5.5.10 build 9905.

C++17 forced deviations from the literal spec text are documented in
the "Pitch deviations from spec" section below — primarily the
`std::auto_ptr` → `rti1516e::auto_ptr` alias (the spec returns
`std::auto_ptr` from factories; C++17 removed it; gorti aliases to
`std::unique_ptr`). See `docs/DLC_COMPLIANCE_PROGRAM.md §3.1.0` for
the rationale.

The C++ column in the method-shape divergence table above shows
"owed-to-M35" for every cell that the DLC track will GREEN-flip.

### M32 catalogue progress (M32-resolved sections, ~50/200 lockfile TUs)

Per `docs/DLC_COMPLIANCE_PROGRAM.md §6`, M32 flips these catalogue
sections from RED→GREEN:

| Catalogue § | Title | Lockfile TU dir | M32 status |
|---|---|---|---|
| §1 | Header layout & namespace | `lockfile/core/test_*ambassador*.cpp` paths under `cppsdk/include/RTI/` | **M32-resolved** (header tree matches Annex A) |
| §2 | Construction & factory | `lockfile/core/test_rtiambassadorfactory.cpp`, `test_rtiambassador_connect.cpp` | **M32-resolved** (`RTIambassadorFactory().createRTIambassador()` returns `auto_ptr<RTIambassador>`) |
| §5 | Enums | `lockfile/core/test_callbackmodel_enum.cpp` + `RTI/Enums.h` reprint | **M32-resolved** (unscoped enums per FR-DLC-16) |
| §7 | Typed handles | `lockfile/types/test_*handle*.cpp` | **M32-resolved** (handle ctor / equality / hash / wstring roundtrip) |
| §8 | VariableLengthData | `lockfile/types/test_vld*.cpp` | **M32-resolved** (`VariableLengthData` deep-copy + size/data accessors) |
| §15 | Misc typedefs | `lockfile/types/test_typedefs.cpp` | **M32-resolved** (`OrderType`, `TransportationType`, `ResignAction`, `SaveStatus`, etc.) |
| §3 | Connect / federation lifecycle | `lockfile/core/test_rtiambassador_federation_mgmt.cpp` | owed-to-M32+M33 (M32 lands ctor, M33 lands save/restore behavioral) |
| §4 | FederateAmbassador callbacks | `lockfile/core/test_federateambassador_signatures.cpp` | owed-to-M33 |
| §6 | Exceptions (~120 classes) | `lockfile/exceptions/test_*.cpp` | owed-to-M33 |
| §10-§13 | Obj mgmt / ownership / DDM / time mgmt | `lockfile/core/test_rtiambassador_{object,ownership,ddm,time}_mgmt.cpp` | owed-to-M33 |
| §9 | Encoding helpers | `lockfile/encoding/test_*.cpp` | owed-to-M34 |
| §14 | Time types | `lockfile/time/test_*.cpp` | owed-to-M34 |
| §16 | MOM | `lockfile/core/test_rtiambassador_mom.cpp` | owed-to-M35 |

After M32 merge, `scripts/check-milestones.sh check_m32` reports the
exact GREEN count for verification against this matrix.

### M32 deviations surfaced (Agent F drift fixes)

When Agent F re-walked the M31 stubs against Pitch pRTI Free 5.5.10
in M32 W1, the following divergences were identified and either
corrected in the stubs or recorded as deliberate gorti choices below
in the "Pitch deviations from spec" section. Specific rows append
into that section as F's drift-fix commits land.

(M32-prep `8f6f50a` already captured 3 Pitch goldens + 24 attested
goldens + the initial deviations row for `fm_create_join_resign`
enumerators 3-6. M32 W1 surfaces additional rows as F's catalogue
re-walk completes.)

### M33 catalogue progress (M33-resolved sections, ~100/200 lockfile TUs — IN PROGRESS 2026-07-02)

Per `docs/DLC_COMPLIANCE_PROGRAM.md §6`, M33 flips these catalogue
sections from RED/M32-stub→GREEN:

| Catalogue § | Title | Lockfile TU dir | M33 status |
|---|---|---|---|
| §3 | Connect / federation lifecycle | `lockfile/core/test_rtiambassador_federation_mgmt.cpp` | **M33-in-progress** (M32 landed ctor; M33 Agent K lands real §4.2/§4.5/§4.9/§4.10 behavioral impl — no longer M32 stub-throw). |
| §4 | FederateAmbassador callbacks | `lockfile/core/test_federateambassador_signatures.cpp` | **M33-in-progress** (Agent J expands to ~40 methods with 3+3+3+2 overloads for the 4 canonical event callbacks — discoverObjectInstance / reflectAttributeValues / receiveInteraction / removeObjectInstance). |
| §6 | Exceptions (~120 classes) | `lockfile/exceptions/test_*.cpp` | **M33-in-progress** (Agent I lands ~120 concrete Annex C exception subclasses derived from `rti1516e::Exception`; every spec-declared throw now throws the concrete type, not `RTIinternalError`). |
| §10 | Object Management behavioral | `lockfile/core/test_rtiambassador_object_mgmt.cpp` | **M33-in-progress** (Agent L lands real impl — no longer M32 stub-throw — for `registerObjectInstance` / `updateAttributeValues` / `sendInteraction` / `deleteObjectInstance` / `localDeleteObjectInstance` / `requestAttributeValueUpdate` / `changeAttributeTransportationType`). |
| §11 | Ownership Management behavioral | `lockfile/core/test_rtiambassador_ownership.cpp` | **M33-in-progress** (Agent M lands real impl for the 8 ownership RPCs wiring through to M17 ownership transport). |
| §12 | DDM behavioral | `lockfile/core/test_rtiambassador_ddm.cpp` | **M33-in-progress** (Agent N lands real impl for routing spaces + dimensions + regions + region-aware subscribe/publish — wire through to M17 DDM transport). |
| §13 | Support services | `lockfile/core/test_rtiambassador_support.cpp` | **M33-in-progress** (Agent K adds `getObjectClassName` / `getAttributeName` reverse-lookups + `getDimensionUpperBound` + `getOrderType` + `getTransportationType` for DLC surface). |
| §9 | Encoding helpers | `lockfile/encoding/test_*.cpp` | owed-to-M34 (per-class break-out from aggregated `BasicDataElements.h`). |
| §14 | Time types | `lockfile/time/test_*.cpp` | owed-to-M34 (full behavioral conformance beyond M32's basic ctor + operator). |
| §16 | MOM | `lockfile/core/test_rtiambassador_mom.cpp` | owed-to-M35. |

After M33 merge, `scripts/check-milestones.sh check_m33` reports the
exact GREEN count for verification against this matrix. Agent O
backfills the final row counts + the parity-diff outcome once the
fan-out completes.

### M33 first-parity-diff experiment (Agent O)

**Fixture:** `cppsdk/tests/dlc/conformance/om_helloworld_pubsub` — the
2026-06-30 Vehicle+Honk smoke generalized to the DLC-strict surface
(§4.2 / §4.5 / §4.9 / §4.10 / §5.3 / §5.7 / §6.5 / §6.10 / §6.12 for
publisher; §5.6 / §5.8 / §6.9 / §6.11 / §6.13 for subscriber).

**Why this fixture:** M32-prep `8f6f50a` captured real Pitch goldens
for BOTH the publisher and subscriber legs (`expected.publisher.log`
+ `expected.subscriber.log` — both CAPTURED status per the fixture
table above). This is the ONLY fixture where a same-day gorti-vs-Pitch
byte-identical comparison is possible with committed goldens as of M33
open.

**Prerequisites:** the parity-diff attempt requires Agents J (callbacks)
+ L (object mgmt real impl) + M (ownership — for the `CANCEL_THEN_DELETE_THEN_DIVEST`
resign action) + potentially K (support services — the `getObjectClassHandle`
/ `getAttributeHandle` chain the federate uses). Without these, the
fixture throws `RTIinternalError("M32 stub — impl deferred to M33+")`
at first non-ctor method call and no log is captured.

**Outcome (backfilled by Agent O):** MATCH / PARTIAL / DIVERGE / NOT_YET_ATTEMPTED.
See CHANGELOG M33 row for the specific result and follow-on tickets.

**Agent O pre-merge measurement (2026-07-02):** NOT_YET_ATTEMPTED. Both
`federate_publisher.cpp` and `federate_subscriber.cpp` compile clean
against the M32 header set (`g++ -c -std=c++17 -I cppsdk/include ...`
succeeds without diagnostics). Runtime is blocked at the first non-ctor
call — `RTIambassador::connect()` at line 51 of `federate_publisher.cpp`
(and line 42 of `federate_subscriber.cpp`) throws `RTIinternalError`
with the message `"M32 stub — RTIambassador::connect() impl deferred to
M33+."`. The federate's `catch(rti1516e::Exception const&)` block
writes `PUB: ERROR ...` to stderr and returns 1. No `PUB: CONNECT` line
reaches stdout, so no downstream event (CREATE / JOIN / PUBLISH /
REGISTER / UPDATE / SEND / RESIGN) is exercised. The parity-diff attempt
is therefore deterministically NOT_YET_ATTEMPTED until Agent K's §4.2
`connect()` impl merges (parse `crcAddress=…` and route to gorti gRPC).

Additional blockers Agent O identified for a full end-to-end run:
1. Agent K §4.5 `createFederationExecution` / §4.9 `joinFederationExecution` / §4.10 `resignFederationExecution` real impl (currently M32-stub).
2. Agent K §10.2 support services (`getObjectClassHandle` / `getAttributeHandle` / `getInteractionClassHandle` / `getParameterHandle`) — currently M32-stub, but M17 transport already impls these; the wstring-adapter wire-through is Agent K's task.
3. Agent L §6 object management (`registerObjectInstance` / `updateAttributeValues` / `sendInteraction` / `publishObjectClassAttributes` / `publishInteractionClass`) — currently M32-stub.
4. Agent J FederateAmbassador dispatch — the subscriber's `NullFederateAmbassador` subclass expects the RTI to invoke `discoverObjectInstance` / `reflectAttributeValues` / `receiveInteraction`. Currently no dispatch loop.
5. `cppsdk/tests/dlc/conformance/CMakeLists.txt` — `target_link_libraries` currently references `rti1516e` (M17 lib). To exercise the DLC surface impl, the fixture link target needs to switch to `rti1516e_dlc` (M32-landed static archive). This is a mechanical Cmake update pending post-Agent-K/L merge (not a scope blocker per se, but a coordination note).

### M34-PARITY-OUTCOME sentinel (post-M34 baseline, pre-M35 impl merges)

Post-M34 merge (7-agent fan-out AA/AB/AC/AD/AE/AF/AG), first real
gorti↔Pitch parity attempted on `om_helloworld_pubsub`.

**M34 outcome (main = `e1f698d`):** PARTIAL — 2/9 publisher events, 2/7
subscriber events match Pitch byte-identical. Gap: §5 Declaration Mgmt
(publishObjectClassAttributes / subscribeObjectClassAttributes /
publishInteractionClass) and §6 Object Mgmt (registerObjectInstance /
updateAttributeValues / sendInteraction) still throw `NotConnected`
("M17 pImpl not yet wired into DLCRTIambassadorImpl"). Callback
dispatch to the subscriber's `NullFederateAmbassador` also not yet
installed, so DISCOVER / REFLECT / RECEIVE never fire.

**M35-BE (Agent BE, 2026-07-02): second parity re-attempt, 4 fixtures.**
Baseline snapshot with M35-B[ABCD] impl branches NOT YET MERGED to main:

| Fixture | Events matched | Outcome |
|---|---|---|
| `om_helloworld_pubsub` | 4/16 (PUB 2/9, SUB 2/7) | PARTIAL |
| `fm_list_executions` | 9/10 (FED 9/10) | NEAR_MATCH |
| `dm_pub_sub_active_passive` | 4/11 (PUB 2/6, SUB 2/5) | PARTIAL |
| `own_release_request_denied` | 4/11 (ALICE 2/6, BOB 2/5) | PARTIAL |

The `fm_list_executions` NEAR_MATCH is new (not captured in M34 rollup)
— the fixture exercises only §4 Federation Mgmt, which the M34-AA
`createFederationExecution` / `listFederationExecutions` / `destroy` /
`disconnect` sequence covers end-to-end. The single missing event is
`FED: REPORT_FEDERATION_EXECUTIONS count=3 alpha beta gamma` — this
line is emitted from a `reportFederationExecutions(...)` callback on
the FederateAmbassador (§4.8), and the M34-AD callback bridge has not
yet routed the M17 `reportFederationExecutions` reply to the DLC
federate. This is a small M35 fix (one bridge slot).

The 3 PARTIAL fixtures share the same blocker profile as
`om_helloworld_pubsub`: §5/§6 stubs throw `NotConnected`, subscriber
callbacks never dispatch. The M35-B[ABCD] fan-out addresses each of
these — BA fixture emission fix, BB §5 M17 delegation, BC §6 M17
delegation, BD `DLCFederateAmbassadorBridge` install in `connect()`.

**M35 impl branch state (2026-07-02):** all 4 impl branches exist on
agent worktrees but are not yet merged to main:

- `worktree-agent-aceaa0297d29c19af` — M35-BA fixture always emits `PUB: CREATE` (6 lines)
- `worktree-agent-a952f41a108b53743` — M35-BB §5 Declaration Mgmt real M17 delegation (231 lines)
- `worktree-agent-aefc398d2e2bd5247` — M35-BC §6 Object Mgmt real M17 delegations (318 lines)
- `worktree-agent-a7e9c84da2de60105` — M35-BD install DLCFederateAmbassadorBridge in connect() (87 lines)

Post-merge parity re-diff is deferred to the orchestrator's third
parity-diff attempt once BA/BB/BC/BD land on main; Agent BE's captured
logs (`gorti-captured.*.log`) reflect the pre-merge M34 baseline for
all 4 fixtures.

---

## Pitch deviations from spec

Pitch is a *vendor implementation* of IEEE 1516.1-2010, not the spec
itself. When Pitch's observable behavior diverges from the spec text,
the tie-breaker is the **spec** (per
`docs/DLC_COMPLIANCE_PROGRAM.md §5.2.2`). Fixtures that would only
pass against Pitch's specific reading get a `SkipIfPitchDeviation`
marker in the parity driver with a spec cite.

Initially empty — populated as the M32-M35 GREEN-flip work surfaces
divergences. Reserved rows for the deviations we've already noticed:

| Sentence | Pitch | Spec § | Status |
|---|---|---|---|
| `evokeCallback(0.0, 0.0)` blocking | Pitch blocks ~10 ms (scheduler quantum) before return | §10.41 "approximate minimum time = 0 means return immediately" | DEVIATION; gorti follows spec; parity-leg expected to diverge here; `SkipIfPitchDeviation("Pitch §10.41 timing quirk")`. |
| C++17 `std::auto_ptr` factory return | Pitch ships C++03-shape headers; impl declares `std::auto_ptr<T>` | Spec text (Annex A) writes literal `std::auto_ptr` | gorti's **forced deviation**: `rti1516e::auto_ptr` alias. See `docs/DLC_COMPLIANCE_PROGRAM.md §3.1.0`. NOT a spec violation — the spec references a removed C++ facility. |
| `fm_create_join_resign` enumerator iteration (3-6 of 6) | Pitch Free's 2-federate seat cap + seat-retention behavior between rapid in-process resign/disconnect/reconnect cycles prevents iterating all 6 `ResignAction` enumerators within a single federate process. Enumerator 3 (`CANCEL_PENDING_OWNERSHIP_ACQUISITIONS`) additionally errors when no pending acquisitions exist, which is implementation-defined per §4.10. | §4.10 (enumerator definitions are mandatory; iteration order is not) | PARTIAL CAPTURE; enumerators 1-2 Pitch-confirmed, 3-6 spec-derived. golden header attests both. |

(More rows append as M32-M35 surfaces them.)

---

## DLC fixture Pitch-capture status (TASK-350, M32 prep)

Snapshot of the parity-leg attestation state for all 27 DLC conformance fixtures
under `cppsdk/tests/dlc/conformance/<name>/`. Each golden file (`expected.*.log`)
carries a `Pitch-capture status:` header line; this table aggregates that state
for reviewer convenience.

| Fixture | # federates | Pitch-capture status | Notes |
|---|---|---|---|
| `om_helloworld_pubsub` | 2 | CAPTURED | Generalized 2026-06-30 smoke. Pub + sub. |
| `fm_create_join_resign` | 1 | PARTIAL | Enumerators 1-2 captured; 3-6 spec-derived. See row above. |
| `fm_list_executions` | 1 | CAPTURED | §4.6/4.7/4.8 lifecycle. REPORT order RTI-defined; capture sorts lexically. |
| `dm_pub_sub_active_passive` | 2 | PENDING | Pub/sub w/ advisory switches. Tractable; not in current session budget. |
| `dm_unpublish_whole_vs_attrs` | 1 | PENDING | Single federate; expected to capture cleanly. |
| `fm_save_restore_roundtrip` | 1 | PENDING | Save/restore lifecycle; Pitch supports `requestFederationSave/restore`. |
| `fm_sync_full` | 3 | BLOCKED (>2 fed) | registrar + bob + carol. Exceeds Pitch Free 2-fed cap. |
| `fm_sync_subset_with_failure` | 3 | BLOCKED (>2 fed) | Same as `fm_sync_full`. |
| `om_delete_object_tso` | 2 | PENDING | TSO delete ordering. |
| `om_local_delete` | 2 | PENDING | §6.16 — local-delete must NOT propagate. |
| `om_message_retraction` | 2 | PENDING | §8.20 retract. |
| `om_request_attribute_update_class` | 2 | PENDING | §6.19/§6.20. |
| `om_request_attribute_update_instance` | 2 | PENDING | §6.19/§6.20 instance variant. |
| `om_reserve_multi_atomic` | 2 | PENDING | §6.5 multi-reserve atomicity. |
| `own_negotiated_divest_two_phase` | 2 | PENDING | §7 ownership negotiated divest. |
| `own_acquire_if_available_race` | 3 | BLOCKED (>2 fed) | bob + carol + carrier. |
| `own_query_via_callbacks` | 2 | PENDING | §7 ownership query callbacks. |
| `own_release_request_denied` | 2 | PENDING | §7 ownership release-denied. |
| `tm_ner_pair` | 2 | PENDING | §8 NER pair. Pitch implements full time mgmt. |
| `tm_tar_tara_fqr_nmra` | 1 | PENDING | §8 TAR/TARA/FQR/NMRA. |
| `tm_lookahead_change` | 2 | PENDING | §8.21 modifyLookahead. |
| `tm_tso_ordering` | 4 | BLOCKED (>2 fed) | alice + bob + carol + subscriber. |
| `ddm_region_overlap` | 2 | PENDING | §9 DDM region overlap. |
| `ddm_region_mod_in_flight` | 2 | PENDING | §9 region modification mid-update. |
| `mom_federation_lifecycle` | 3 | BLOCKED (>2 fed) | §11 MOM observer. |
| `threading_callback_reentry` | 2 | N/A (gorti-only) | Tier 5; Pitch may not implement §10 `CallNotAllowedFromWithinCallback` per spec. |
| `xlang_python_cpp_pubsub` | 2 | N/A (gorti-only) | Tier 5; Pitch Free 5.5.10 ships no Python binding. |

**Tally:** 3 CAPTURED (incl. 1 PARTIAL), 17 PENDING (tractable 1-2-federate, follow-on
TASK-350 work), 5 BLOCKED by Pitch Free 2-federate EULA cap, 2 gorti-only by design.

All goldens currently committed pass `scripts/check-spec-traceability.sh` lint:
`27/27 fixtures clean`. No Pitch trademarks leaked into committed golden files
per `grep -rE "Pitch Technologies|pRTI.*[Tt]rade.*mark" expected*.log` → 0 hits.

### M35-PARITY-OUTCOME sentinel (post-reconciliation, 2026-07-02)

M35-PARITY-OUTCOME: FULL — om_helloworld_pubsub 9/9 pub + 7/7 sub events
byte-identical to Pitch pRTI Free 5.5.10 build 9905 after §5.2.1
canonicalization (handle ints → `<H>`, RO-bucket lexical sort). First
end-to-end FULL MATCH on integrated main: the strict DLC C++ surface
(RTIambassadorFactory → connect → create → join → publish → register →
update → send → resign, with subscriber-side DISCOVER/REFLECT/RECEIVE via
the DLCFederateAmbassadorBridge) produces the same canonical event
sequence as Pitch.

Known RTI-defined divergence (documented, not a spec violation): gorti
delivers REFLECT before RECEIVE within the same RO bucket; Pitch delivers
RECEIVE before REFLECT. §6 mandates only causal order for RO delivery —
the canonicalizer's within-bucket sort (rule 2) absorbs this by design.

Remaining fixtures: 26 of 27 not yet at FULL (17 tractable with the same
evoke-drain + emission-precision pattern applied here; 5 blocked by the
Pitch Free 2-federate cap for golden capture; 2 gorti-only by design +
2 own_* / tm_* fixture-level API fixes already landed in M35-K/N).
