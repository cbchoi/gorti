"""M28 W4 — Pitch typed-handle public-namespace lockfile.

Asserts every M28-added public symbol is importable from the
``rti1516e`` namespace and behaves correctly:

  - 9 typed handle classes (ObjectClassHandle..MessageRetractionHandle)
    are importable + constructible + int-subclassed.
  - 8 typed collection classes (5 sets, 2 value-maps, AttributeRegionMap)
    are importable + constructible.
  - 6 ambassador factory accessors return the matching factory type.
  - ``getAttributeHandleSetFactory().create()`` returns
    ``AttributeHandleSet``.
  - Pitch-style imports + construction round-trip works.

Lock pattern mirrors ``pysdk/tests/spec/m25/test_ambassador_surface.py``.
"""

from __future__ import annotations

import pytest

import rti1516e
from rti1516e import (
    AttributeHandle,
    AttributeHandleSet,
    AttributeHandleValueMap,
    AttributeRegionMap,
    DimensionHandle,
    DimensionHandleSet,
    FederateHandle,
    FederateHandleSet,
    InteractionClassHandle,
    MessageRetractionHandle,
    ObjectClassHandle,
    ObjectInstanceHandle,
    ParameterHandle,
    ParameterHandleSet,
    ParameterHandleValueMap,
    RegionHandle,
    RegionHandleSet,
)
from rti1516e.factories import (
    AttributeHandleSetFactory,
    AttributeHandleValueMapFactory,
    DimensionHandleSetFactory,
    FederateHandleSetFactory,
    ParameterHandleValueMapFactory,
    RegionHandleSetFactory,
)
from rti1516e.standard import Rti1516eAmbassador

# --- 9 typed handle classes ---

TYPED_HANDLE_CLASSES = [
    ObjectClassHandle,
    AttributeHandle,
    InteractionClassHandle,
    ParameterHandle,
    ObjectInstanceHandle,
    FederateHandle,
    DimensionHandle,
    RegionHandle,
    MessageRetractionHandle,
]


@pytest.mark.spec
@pytest.mark.parametrize("cls", TYPED_HANDLE_CLASSES, ids=lambda c: c.__name__)
def test_spec_m28_typed_handle_importable_and_constructible(cls: type) -> None:
    """Each typed handle class is importable from ``rti1516e`` and
    constructible from an int, and is an ``int`` subclass."""
    assert getattr(rti1516e, cls.__name__) is cls
    inst = cls(5)
    assert isinstance(inst, cls)
    assert isinstance(inst, int)
    assert int(inst) == 5
    # repr shows the type name so debuggability survives.
    assert cls.__name__ in repr(inst)


# --- 8 typed collection classes ---

TYPED_SET_CLASSES = [
    AttributeHandleSet,
    ParameterHandleSet,
    FederateHandleSet,
    DimensionHandleSet,
    RegionHandleSet,
]

TYPED_MAP_CLASSES = [
    AttributeHandleValueMap,
    ParameterHandleValueMap,
    AttributeRegionMap,
]


@pytest.mark.spec
@pytest.mark.parametrize("cls", TYPED_SET_CLASSES, ids=lambda c: c.__name__)
def test_spec_m28_typed_set_importable_and_constructible(cls: type) -> None:
    """Each typed handle-set class is importable from ``rti1516e`` and
    constructible (empty + from-iterable)."""
    assert getattr(rti1516e, cls.__name__) is cls
    empty = cls()
    assert len(empty) == 0
    populated = cls([1, 2, 3])
    assert len(populated) == 3


@pytest.mark.spec
@pytest.mark.parametrize("cls", TYPED_MAP_CLASSES, ids=lambda c: c.__name__)
def test_spec_m28_typed_map_importable_and_constructible(cls: type) -> None:
    """Each typed value-map class is importable from ``rti1516e`` and
    constructible (empty)."""
    assert getattr(rti1516e, cls.__name__) is cls
    empty = cls()
    assert len(empty) == 0


# --- 6 factory accessors on the ambassador ---

# (accessor name, expected factory class)
FACTORY_ACCESSORS = [
    ("getAttributeHandleSetFactory", AttributeHandleSetFactory),
    ("getAttributeHandleValueMapFactory", AttributeHandleValueMapFactory),
    ("getParameterHandleValueMapFactory", ParameterHandleValueMapFactory),
    ("getFederateHandleSetFactory", FederateHandleSetFactory),
    ("getDimensionHandleSetFactory", DimensionHandleSetFactory),
    ("getRegionHandleSetFactory", RegionHandleSetFactory),
]


@pytest.mark.spec
@pytest.mark.parametrize(
    ("accessor_name", "factory_cls"),
    FACTORY_ACCESSORS,
    ids=[name for name, _ in FACTORY_ACCESSORS],
)
def test_spec_m28_factory_accessor_returns_factory(
    accessor_name: str, factory_cls: type
) -> None:
    """Each §10.6 factory accessor on Rti1516eAmbassador returns the
    matching factory type."""
    amb = Rti1516eAmbassador()
    accessor = getattr(amb, accessor_name, None)
    assert accessor is not None, f"Rti1516eAmbassador.{accessor_name} missing"
    assert callable(accessor), f"Rti1516eAmbassador.{accessor_name} not callable"
    factory = accessor()
    assert isinstance(factory, factory_cls), (
        f"{accessor_name}() returned {type(factory).__name__}, "
        f"expected {factory_cls.__name__}"
    )


@pytest.mark.spec
def test_spec_m28_attribute_handle_set_factory_create_returns_set() -> None:
    """``getAttributeHandleSetFactory().create()`` returns an empty
    ``AttributeHandleSet`` instance."""
    amb = Rti1516eAmbassador()
    s = amb.getAttributeHandleSetFactory().create()
    assert isinstance(s, AttributeHandleSet)
    assert len(s) == 0


@pytest.mark.spec
def test_spec_m28_pitch_style_import_path_roundtrip() -> None:
    """Pitch-style code path works: import the typed handle + typed
    set from ``rti1516e``, construct, and observe coerce-on-insert."""
    h = ObjectClassHandle(5)
    assert isinstance(h, ObjectClassHandle)
    s = AttributeHandleSet([1, 2])
    assert isinstance(s, AttributeHandleSet)
    assert all(isinstance(x, AttributeHandle) for x in s)
    # Coerce-on-insert: mixed bare-int + typed insertion.
    s.add(3)
    s.add(AttributeHandle(4))
    assert len(s) == 4
    assert all(isinstance(x, AttributeHandle) for x in s)


@pytest.mark.spec
def test_spec_m28_typed_handle_int_equality_documented_concession() -> None:
    """``ObjectClassHandle(5) == AttributeHandle(5)`` returns True — the
    deliberate dual-accept concession documented in PITCH_PARITY.md.
    Code that needs strict type-distinction must use ``isinstance()``."""
    a = ObjectClassHandle(5)
    b = AttributeHandle(5)
    # mypy flags this as non-overlapping; the equality holds at runtime
    # because both are int(5). Documented concession per PITCH_PARITY.md.
    assert a == b  # type: ignore[comparison-overlap]
    assert not isinstance(a, AttributeHandle)
    assert not isinstance(b, ObjectClassHandle)
