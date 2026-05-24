# Pitch / Portico / MAK API parity notes

`pysdk/rti1516e/standard.py::Rti1516eAmbassador` is the "Layer 2"
sync ambassador mirroring IEEE 1516.1-2010 §6 `RTIambassador`. It
is what federates ported from a commercial RTI (Pitch HLA Evolved,
Portico, MAK RTI) should bind against.

This document records the places where gorti's Layer-2 surface
diverges from a strict reading of the spec, what's compatible
anyway, and what's explicitly out of scope. Last updated for M27.

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

## Method-shape divergence table

| Spec method | Pitch shape | gorti shape | Status |
|---|---|---|---|
| `publishObjectClassAttributes` | `(ObjectClassHandle, AttributeHandleSet)` | `(int \| str, list[int \| str])` | Compatible |
| `subscribeObjectClassAttributes` | `(ObjectClassHandle, AttributeHandleSet)` | `(int \| str, list[int \| str])` | Compatible |
| `registerObjectInstance` | `(ObjectClassHandle[, String])` | `(int \| str, str \| None)` | Compatible |
| `updateAttributeValues` | `(ObjectInstanceHandle, Map<AttributeHandle, byte[]>[, LogicalTime])` | `(int, dict[int \| str, bytes], float \| None)` | Compatible |
| `sendInteraction` | `(InteractionClassHandle, Map<ParameterHandle, byte[]>, ...)` | `(int \| str, dict[int \| str, bytes], ...)` | Compatible |
| `getObjectClassHandle(name)` | `(String) -> ObjectClassHandle` | `(str) -> int` | Compatible (M25 B) |
| `getAttributeHandle` | `(ObjectClassHandle, String) -> AttributeHandle` | `(int, str) -> int` | Compatible (M25 B) |
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

`evokeCallback` / `evokeMultipleCallbacks` are aliases over
`tickCallback` in Cut-3 — a single call may dispatch MORE than
one buffered callback. Strict at-most-one `evokeCallback`
semantics defer to a Cut-4 refactor that extracts the dispatch
switch from `tickCallback` into a shared private helper.

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
