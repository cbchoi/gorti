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
M31 lands the RED test scaffold (`docs/M31_DISPATCH_PLAN.md`); M32-M35
flip slices GREEN per `docs/DLC_COMPLIANCE_PROGRAM.md §6`.

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
