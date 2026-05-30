# M28 Dispatch Plan — pysdk typed handles + typed collections (Pitch-port parity)

How the orchestrator dispatches the M28 tasks (TASK-312..325) to maximize parallel sub-agent throughput.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md`, `docs/PITCH_PARITY.md`, `docs/agent-c-pysdk.md`, `docs/MILESTONE_CHECK.md`, `cppsdk/include/rti1516e/Types.h` (typed-handle prior art).

---

## 1. Goal & non-goals

### Goal

First milestone of the Pitch 1516e source-compat track. Bring **pysdk** to typed-handle parity with the C++ SDK and with the Pitch / Portico / MAK Python ambassador shape, so that a Python federate written against `hla.rti1516e.*` typed handles (`ObjectClassHandle`, `AttributeHandleSet`, etc.) compiles and runs against gorti unchanged.

The C++ SDK already ships typed handles via `StrongHandle<Tag>` (`cppsdk/include/rti1516e/Types.h:32-117`) and typed collections (`AttributeHandleSet`, `AttributeHandleValueMap`, `ParameterHandleValueMap`, `AttributeRegionMap`, etc.). M28 is **pysdk-only**; the M28+M29 split from the original scoping memo (`memory/project_gorti_pitch_compat.md`) collapses into this single milestone because the C++ work is already complete.

Three parts:

1. **Typed handle classes** as `int` subclasses (W1). Pitch-style names (`ObjectClassHandle`, `AttributeHandle`, `InteractionClassHandle`, `ParameterHandle`, `ObjectInstanceHandle`, `FederateHandle`, `DimensionHandle`, `RegionHandle`). int-subclass keeps existing `handle == 5` / `int(handle)` / arithmetic-on-handle assertions green.
2. **Typed collection classes** (W2): `AttributeHandleSet`, `ParameterHandleSet`, `FederateHandleSet`, `DimensionHandleSet`, `RegionHandleSet`, `AttributeHandleValueMap`, `ParameterHandleValueMap`, `AttributeRegionMap`. Coerce on insert so mixed `int` / typed callers both work.
3. **Ambassador return-type tightening + factory accessors** (W3). `getObjectClassHandle()` returns `ObjectClassHandle` (subclass of int — back-compat); ambassador grows the Pitch `getXxxHandleFactory()` / `getXxxHandleSetFactory()` / `getXxxHandleValueMapFactory()` slots.

### Non-goals

- **C++ SDK changes.** `cppsdk/include/rti1516e/Types.h` already has typed handles + typed collections; M28 does NOT touch cppsdk.
- **Go SDK changes.** Go is not a Pitch-port target. The `rti/pkg/federate` types stay as-is.
- **Wire-protocol changes.** Handles still travel as `uint64` over gRPC. M28 is a Python API layer.
- **Strict `HLA_EVOKED` callback buffering.** Moved to M29.
- **§10 `getUpdateRateValueForAttribute`, §10.2 `getAvailableDimensionsFor*`, §11 MOM ambassador methods.** Moved to M30.
- **Hard cutover.** Per `memory/project_gorti_pitch_compat.md`, dual-accept is permanent: ambassador methods accept `int | str | TypedHandle` and `list[...] | TypedHandleSet`. M25-M27 tests pass unmodified.
- **Java SDK coupling.** Per the deferred open question — design assumes pysdk/cppsdk-only for now. If M18 (Java) starts later and the handle shape needs to change, M28 is the foundation we'll iterate on, not a contract.

### Why now

- M27 closed v0.1.0 (2026-05-22). Source-compat track was scoped 2026-05-30 and re-opened by the user. M28 is the first milestone.
- The `docs/PITCH_PARITY.md:32-44` divergence table flags this exact gap: every Pitch-shape method in the table is marked "Compatible" but the typed-handle column reads `(int | str, list[int | str])` instead of `(ObjectClassHandle, AttributeHandleSet)`. M28 closes that column.
- Existing `examples/pitch-shape-smoke/` exercises Pitch-style method NAMES against bare ints. M28 adds a sibling smoke that uses Pitch-style TYPES too — the "would this code compile against pRTI Community Edition unchanged?" test.

---

## 2. Surface design

### 2.1 Typed handle classes (W1)

`pysdk/rti1516e/handles.py` (NEW):

```python
"""Pitch-style typed handles.

Wrappers subclass ``int`` so existing gorti code that compares handles
to ints, hashes them in dicts, or passes them to ``int(...)`` keeps
working. The wrapper class disambiguates ``ObjectClassHandle`` from
``AttributeHandle`` at the type level via ``isinstance()`` — which is
the only distinction porting users from Pitch need.

NOTE: Two handles of different types but the same underlying value
will compare equal (``ObjectClassHandle(5) == AttributeHandle(5)``
returns True). This is a deliberate dual-accept concession: it lets
mixed-typed and bare-int callers interoperate. Code that needs strict
type-distinction must use ``isinstance()``.
"""

from __future__ import annotations


class _StrongHandle(int):
    __slots__ = ()

    def __repr__(self) -> str:
        return f"{type(self).__name__}({int(self)})"

    # Subclassing int preserves __hash__, __eq__, __lt__, __index__,
    # and arithmetic. No overrides needed.


class ObjectClassHandle(_StrongHandle):
    __slots__ = ()


class AttributeHandle(_StrongHandle):
    __slots__ = ()


class InteractionClassHandle(_StrongHandle):
    __slots__ = ()


class ParameterHandle(_StrongHandle):
    __slots__ = ()


class ObjectInstanceHandle(_StrongHandle):
    __slots__ = ()


class FederateHandle(_StrongHandle):
    __slots__ = ()


class DimensionHandle(_StrongHandle):
    __slots__ = ()


class RegionHandle(_StrongHandle):
    __slots__ = ()


class MessageRetractionHandle(_StrongHandle):
    __slots__ = ()
```

### 2.2 Typed collections (W2)

`pysdk/rti1516e/sets.py` (NEW):

```python
"""Pitch-style typed handle collections.

Set subclasses coerce ``int`` to the typed handle on insert so mixed
callers work. Map subclasses coerce keys on ``__setitem__``.
"""

from __future__ import annotations
from typing import Iterable

from .handles import (
    AttributeHandle,
    DimensionHandle,
    FederateHandle,
    ParameterHandle,
    RegionHandle,
)


class AttributeHandleSet(set):
    def __init__(self, iterable: Iterable = ()):
        super().__init__(AttributeHandle(int(h)) for h in iterable)

    def add(self, h) -> None:
        super().add(h if isinstance(h, AttributeHandle) else AttributeHandle(int(h)))


class ParameterHandleSet(set):
    def __init__(self, iterable: Iterable = ()):
        super().__init__(ParameterHandle(int(h)) for h in iterable)

    def add(self, h) -> None:
        super().add(h if isinstance(h, ParameterHandle) else ParameterHandle(int(h)))


class FederateHandleSet(set):
    def __init__(self, iterable: Iterable = ()):
        super().__init__(FederateHandle(int(h)) for h in iterable)

    def add(self, h) -> None:
        super().add(h if isinstance(h, FederateHandle) else FederateHandle(int(h)))


class DimensionHandleSet(set): ...  # same shape
class RegionHandleSet(set): ...     # same shape


class AttributeHandleValueMap(dict):
    def __setitem__(self, k, v):
        super().__setitem__(
            k if isinstance(k, AttributeHandle) else AttributeHandle(int(k)),
            v,
        )


class ParameterHandleValueMap(dict):
    def __setitem__(self, k, v):
        super().__setitem__(
            k if isinstance(k, ParameterHandle) else ParameterHandle(int(k)),
            v,
        )


class AttributeRegionMap(dict):
    """AttributeHandle -> RegionHandleSet."""
    def __setitem__(self, k, v):
        super().__setitem__(
            k if isinstance(k, AttributeHandle) else AttributeHandle(int(k)),
            v if isinstance(v, RegionHandleSet) else RegionHandleSet(v),
        )
```

### 2.3 Factory accessors on the ambassador (W2)

Pitch's idiom is `rtiAmb.getAttributeHandleSetFactory().create()`. Stubs:

```python
# pysdk/rti1516e/factories.py (NEW)

class AttributeHandleSetFactory:
    def create(self) -> AttributeHandleSet:
        return AttributeHandleSet()

class AttributeHandleValueMapFactory:
    def create(self, capacity: int = 0) -> AttributeHandleValueMap:
        return AttributeHandleValueMap()

# ... ParameterHandleSetFactory, ParameterHandleValueMapFactory,
#     FederateHandleSetFactory, DimensionHandleSetFactory,
#     RegionHandleSetFactory, AttributeHandleFactory, etc.
```

Added to `Rti1516eAmbassador`:

```python
def getAttributeHandleSetFactory(self) -> AttributeHandleSetFactory: ...
def getAttributeHandleValueMapFactory(self) -> AttributeHandleValueMapFactory: ...
def getParameterHandleValueMapFactory(self) -> ParameterHandleValueMapFactory: ...
def getFederateHandleSetFactory(self) -> FederateHandleSetFactory: ...
def getDimensionHandleSetFactory(self) -> DimensionHandleSetFactory: ...
def getRegionHandleSetFactory(self) -> RegionHandleSetFactory: ...
```

### 2.4 Ambassador return-type tightening (W3)

`pysdk/rti1516e/standard.py`:

```python
def getObjectClassHandle(self, class_name: str) -> ObjectClassHandle:
    # was: -> int
    return ObjectClassHandle(self._lookup(...))

def getAttributeHandle(
    self,
    class_handle: int | str | ObjectClassHandle,
    attribute_name: str,
) -> AttributeHandle:
    # was: -> int, was: class_handle: int | str
    ...

# ... all get*Handle methods return the typed wrapper.
```

Accept-types broaden for every method already taking `int | str`:

```python
def publishObjectClassAttributes(
    self,
    class_handle: int | str | ObjectClassHandle,
    attribute_handles: list[int | str | AttributeHandle] | AttributeHandleSet,
) -> None: ...

def updateAttributeValues(
    self,
    object_handle: int | ObjectInstanceHandle,
    attribute_values: dict[int | str | AttributeHandle, bytes] | AttributeHandleValueMap,
    time: float | None = None,
) -> None: ...
```

### 2.5 Public re-exports

`pysdk/rti1516e/__init__.py` gains:

```python
from .handles import (
    AttributeHandle,
    DimensionHandle,
    FederateHandle,
    InteractionClassHandle,
    MessageRetractionHandle,
    ObjectClassHandle,
    ObjectInstanceHandle,
    ParameterHandle,
    RegionHandle,
)
from .sets import (
    AttributeHandleSet,
    AttributeHandleValueMap,
    AttributeRegionMap,
    DimensionHandleSet,
    FederateHandleSet,
    ParameterHandleSet,
    ParameterHandleValueMap,
    RegionHandleSet,
)
```

so porting users write `from rti1516e import ObjectClassHandle, AttributeHandleSet` exactly as they would against Pitch.

---

## 3. Acceptance criteria (exit gate)

1. **Typed handles importable + isinstance works.** `from rti1516e import ObjectClassHandle; isinstance(amb.getObjectClassHandle("Foo"), ObjectClassHandle)` is True; `isinstance(..., AttributeHandle)` is False.
2. **int-equality back-compat preserved.** `amb.getObjectClassHandle("Foo") == 1` still True; `int(amb.getObjectClassHandle("Foo"))` returns the bare int; existing M25-M27 tests pass with **zero modifications**.
3. **Typed sets/maps usable as Pitch expects.** `s = AttributeHandleSet(); s.add(attr_handle); s.add(7)` — both end up as `AttributeHandle` instances; `len(s) == 2`.
4. **Factory accessors return Pitch-shape factories.** `amb.getAttributeHandleSetFactory().create()` returns an empty `AttributeHandleSet`.
5. **Ambassador accepts mixed bare + typed args.** `amb.publishObjectClassAttributes(handle, [1, AttributeHandle(2), "Position"])` works.
6. **Ambassador accepts typed collections directly.** `amb.publishObjectClassAttributes(handle, AttributeHandleSet([1, 2]))` works.
7. **Pitch-port smoke test (typed) passes.** New `examples/pitch-typed-smoke/` mirrors the M26 pitch-shape smoke but uses ONLY `ObjectClassHandle` / `AttributeHandleSet` / `AttributeHandleValueMap` (no bare ints in user code). Smoke driver at `pysdk/tests/spec/m28/test_pitch_typed_smoke.py`.
8. **Existing pitch-shape smoke (M26) still green** — proves back-compat.
9. **Lockfile updated.** `pysdk/tests/spec/m25/test_ambassador_surface.py` lockfile extended with the factory accessors; new `pysdk/tests/spec/m28/test_typed_handle_surface.py` asserts every typed handle class + collection class is importable and constructible from the public `rti1516e` namespace.
10. **`docs/PITCH_PARITY.md` divergence table updated** — every "gorti shape" column entry that currently reads `(int | str, list[int | str])` etc. updated to reflect M28 typed-accept; the §"Method-shape divergence table" row for `publishObjectClassAttributes` etc. moves from "Compatible" with int-only to "Compatible (M28 typed-handle parity)".
11. **`scripts/check-milestones.sh` reports `M28: DONE (N/N)`.**
12. **CHANGELOG-MASTERPLAN.md M28 entry landed.**
13. **`docs/srs.md` §10 M28 row appended.**

---

## 4. Wave structure

```
                                M28 START
                                    │
    ┌───────────────────────────────┼───────────────────────────────┐
    │                               │                               │
    │   W1   — handles.py + sets.py + factories.py (NEW)            │
    │           Pure data types, no ambassador coupling             │
    │           Parallelizable across the 9 handle types            │
    │                               │                               │
    │   W2   — Public re-exports + factory wiring on ambassador     │
    │           pysdk/rti1516e/__init__.py                          │
    │           pysdk/rti1516e/standard.py factory slots            │
    │                               │                               │
    │   W3   — Ambassador return-type tightening + accept-type      │
    │           broadening across all Pitch-shape methods           │
    │           pysdk/rti1516e/standard.py + connection.py          │
    │                               │                               │
    │   W4   — Acceptance gate + docs                               │
    │           examples/pitch-typed-smoke/                         │
    │           pysdk/tests/spec/m28/*                              │
    │           docs/PITCH_PARITY.md update                         │
    │           srs.md M28 row + CHANGELOG + check-milestones probe │
    │                               │                               │
                                    ▼
                       M28 DONE per srs.md §10
```

W1 is the only fully-parallelizable wave. W2 depends on W1 (factories reference set classes). W3 depends on W1+W2 (typed annotations import the new module). W4 depends on W3.

---

## 5. Tasks

### W1 — Pure data types (parallelizable)
- **TASK-312**: `pysdk/rti1516e/handles.py` (NEW). 9 `_StrongHandle` subclasses + `__repr__`.
- **TASK-313**: `pysdk/rti1516e/sets.py` (NEW). 5 set classes + 2 value-map classes + AttributeRegionMap with coerce-on-insert.
- **TASK-314**: `pysdk/rti1516e/factories.py` (NEW). 6 factory classes returning the W1 collections.
- **TASK-315**: `pysdk/tests/unit/test_handles.py` (NEW). Round-trip int↔Handle, isinstance distinction, dict-key hashability, repr.
- **TASK-316**: `pysdk/tests/unit/test_sets.py` (NEW). Coerce-on-insert, len, iter, contains, mixed-type construction.

### W2 — Public surface + factory wiring
- **TASK-317**: `pysdk/rti1516e/__init__.py`: re-export the 9 handles + 8 collection classes.
- **TASK-318**: `pysdk/rti1516e/standard.py`: add 6 `get*Factory` slots returning W1 factory instances. Cache as instance attrs.

### W3 — Ambassador signature update
- **TASK-319**: `pysdk/rti1516e/standard.py`: tighten return types on all `get*Handle` methods (9 methods).
- **TASK-320**: `pysdk/rti1516e/standard.py`: broaden accept types on Pitch-shape methods to accept typed handles + typed collections (Publish/Subscribe/Register/Update/Send/Delete/RequestUpdate/ChangeTransport — ~14 methods).
- **TASK-321**: `pysdk/rti1516e/connection.py`: same for the connection-level passthroughs.
- **TASK-322**: Ensure internal Pitch-shape adapters in `pysdk/rti1516e/object.py`, `pysdk/rti1516e/interaction.py`, `pysdk/rti1516e/ddm.py`, `pysdk/rti1516e/ownership.py` accept typed handles via `int(h)` coercion at the boundary.

### W4 — Acceptance gate + docs
- **TASK-323**: `examples/pitch-typed-smoke/` (NEW). Federate that uses ONLY typed handles + typed sets. README mirrors `examples/pitch-shape-smoke/` style.
- **TASK-324**: `pysdk/tests/spec/m28/test_pitch_typed_smoke.py` + `test_typed_handle_surface.py`. Lockfile for the new public types.
- **TASK-325**: Docs sweep: `docs/PITCH_PARITY.md` table update; `docs/srs.md` §10 M28 row; `CHANGELOG-MASTERPLAN.md` M28 entry; `scripts/check-milestones.sh` check_m28() probe.

---

## 6. Test plan

| Test | Asserts |
|---|---|
| `test_handles.py` | int-equality preserved, isinstance distinction, hashable as dict keys, repr shows type |
| `test_sets.py` | coerce-on-insert, mixed-type construction, len/iter/contains, value-map __setitem__ coercion |
| `test_pitch_typed_smoke.py` | full federate cycle (connect→join→publish→register→update→sendInteraction→resign) using ONLY typed handles + typed sets |
| `test_typed_handle_surface.py` | every typed handle class + collection class + factory accessor importable from `rti1516e.*` namespace |
| `pysdk/tests/spec/m26/test_pitch_shape_smoke.py` | (existing, MUST NOT REGRESS) — proves bare-int back-compat |
| `pysdk/tests/spec/m25/test_ambassador_surface.py` | (existing, EXTENDED) — add the 6 factory-accessor methods to the lockfile |

---

## 7. Migration impact

**None for existing callers.** The two changes are:

1. **Return-type narrowing** (`-> int` becomes `-> ObjectClassHandle`). Back-compat because `ObjectClassHandle(5) == 5` and `int(ObjectClassHandle(5)) == 5`. Existing tests that assert handle equality against integers pass unchanged.
2. **Accept-type widening** (`int | str` becomes `int | str | TypedHandle`, `list[...]` becomes `list[...] | TypedHandleSet`). Pure widening — existing callers unaffected.

The one behavioral subtlety to flag in `docs/PITCH_PARITY.md`: `ObjectClassHandle(5) == AttributeHandle(5)` returns True (int-equality). Pitch's typed handles return False. Code that relies on cross-type inequality must use `isinstance()`. This is called out in the docstring of `_StrongHandle` and documented in PITCH_PARITY as a known dual-accept concession.

---

## 8. M28 row append target (W4 — for reference)

```markdown
| **M28** | Agent C | pysdk typed-handle + typed-collection parity with Pitch 1516e | 9 typed handle classes (ObjectClassHandle, AttributeHandle, InteractionClassHandle, ParameterHandle, ObjectInstanceHandle, FederateHandle, DimensionHandle, RegionHandle, MessageRetractionHandle) as `int` subclasses; 5 typed sets (AttributeHandleSet, ParameterHandleSet, FederateHandleSet, DimensionHandleSet, RegionHandleSet) + 2 value-maps + AttributeRegionMap with coerce-on-insert; 6 factory accessors on Rti1516eAmbassador; ambassador returns typed handles, accepts mixed bare+typed args. Back-compat: M25-M27 tests unmodified, M26 pitch-shape smoke still green. **DONE 2026-MM-DD** — see `docs/M28_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
```
