"""Unit tests for ``rti1516e.sets`` (M28 TASK-316).

Asserts the coerce-on-insert behavior for typed sets and typed
value-maps, plus the dual-coercion ``AttributeRegionMap`` (key to
``AttributeHandle``, value to ``RegionHandleSet``).
"""

from __future__ import annotations

import pytest

from rti1516e.handles import (
    AttributeHandle,
    DimensionHandle,
    FederateHandle,
    ParameterHandle,
    RegionHandle,
)
from rti1516e.sets import (
    AttributeHandleSet,
    AttributeHandleValueMap,
    AttributeRegionMap,
    DimensionHandleSet,
    FederateHandleSet,
    ParameterHandleSet,
    ParameterHandleValueMap,
    RegionHandleSet,
)

SET_CASES = [
    (AttributeHandleSet, AttributeHandle),
    (ParameterHandleSet, ParameterHandle),
    (FederateHandleSet, FederateHandle),
    (DimensionHandleSet, DimensionHandle),
    (RegionHandleSet, RegionHandle),
]


@pytest.mark.parametrize(("set_cls", "handle_cls"), SET_CASES)
def test_set_construction_from_mixed_iterable_coerces_all(
    set_cls: type, handle_cls: type
) -> None:
    s = set_cls([1, handle_cls(2), 3])
    assert len(s) == 3
    for h in s:
        assert isinstance(h, handle_cls)
    assert {int(h) for h in s} == {1, 2, 3}


@pytest.mark.parametrize(("set_cls", "handle_cls"), SET_CASES)
def test_set_add_coerces_bare_int(set_cls: type, handle_cls: type) -> None:
    s = set_cls()
    s.add(7)
    assert len(s) == 1
    member = next(iter(s))
    assert isinstance(member, handle_cls)
    assert member == 7


@pytest.mark.parametrize(("set_cls", "handle_cls"), SET_CASES)
def test_set_add_preserves_existing_typed_handle(
    set_cls: type, handle_cls: type
) -> None:
    s = set_cls()
    original = handle_cls(9)
    s.add(original)
    assert len(s) == 1
    member = next(iter(s))
    assert isinstance(member, handle_cls)


@pytest.mark.parametrize(("set_cls", "handle_cls"), SET_CASES)
def test_set_update_coerces_mixed(set_cls: type, handle_cls: type) -> None:
    s = set_cls()
    s.update([1, handle_cls(2), 3])
    assert len(s) == 3
    for h in s:
        assert isinstance(h, handle_cls)


@pytest.mark.parametrize(("set_cls", "handle_cls"), SET_CASES)
def test_set_supports_len_iter_contains(
    set_cls: type, handle_cls: type
) -> None:
    s = set_cls([1, 2, 3])
    assert len(s) == 3
    members = list(s)
    assert len(members) == 3
    # int-equality means bare-int containment works.
    assert 2 in s
    assert handle_cls(2) in s
    assert 99 not in s


def test_attribute_handle_value_map_coerces_keys() -> None:
    m = AttributeHandleValueMap()
    m[5] = b"x"
    keys = list(m.keys())
    assert len(keys) == 1
    assert isinstance(keys[0], AttributeHandle)
    assert keys[0] == 5
    assert m[5] == b"x"
    assert m[AttributeHandle(5)] == b"x"


def test_attribute_handle_value_map_preserves_typed_key() -> None:
    m = AttributeHandleValueMap()
    m[AttributeHandle(3)] = b"y"
    key = next(iter(m.keys()))
    assert isinstance(key, AttributeHandle)


def test_parameter_handle_value_map_coerces_keys() -> None:
    m = ParameterHandleValueMap()
    m[7] = b"a"
    m[ParameterHandle(8)] = b"b"
    for k in m:
        assert isinstance(k, ParameterHandle)
    assert m[7] == b"a"
    assert m[8] == b"b"


def test_attribute_region_map_coerces_key_and_value() -> None:
    m = AttributeRegionMap()
    region1 = RegionHandle(11)
    region2 = RegionHandle(22)
    m[5] = [region1, region2]

    keys = list(m.keys())
    assert len(keys) == 1
    assert isinstance(keys[0], AttributeHandle)
    assert keys[0] == 5

    value = m[5]
    assert isinstance(value, RegionHandleSet)
    assert len(value) == 2
    for r in value:
        assert isinstance(r, RegionHandle)
    assert {int(r) for r in value} == {11, 22}


def test_attribute_region_map_accepts_bare_int_regions() -> None:
    m = AttributeRegionMap()
    m[AttributeHandle(1)] = [11, 22]
    value = m[AttributeHandle(1)]
    assert isinstance(value, RegionHandleSet)
    for r in value:
        assert isinstance(r, RegionHandle)


def test_attribute_region_map_preserves_region_handle_set_value() -> None:
    m = AttributeRegionMap()
    existing = RegionHandleSet([1, 2, 3])
    m[AttributeHandle(7)] = existing
    assert m[AttributeHandle(7)] is existing
