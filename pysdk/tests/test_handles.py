"""Unit tests for ``rti1516e.handles`` (M28 TASK-315).

Asserts the typed-handle int-subclass shape: int-equality back-compat,
isinstance-based type distinction, dict-key hashability, and the
``repr`` that surfaces the wrapper class name.
"""

from __future__ import annotations

import pytest

from rti1516e.handles import (
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

ALL_HANDLES = [
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


@pytest.mark.parametrize("cls", ALL_HANDLES)
def test_handle_round_trips_int(cls: type) -> None:
    h = cls(5)
    assert int(h) == 5
    assert h == 5
    assert hash(h) == hash(5)


@pytest.mark.parametrize("cls", ALL_HANDLES)
def test_handle_is_int_subclass(cls: type) -> None:
    h = cls(5)
    assert isinstance(h, int)
    assert isinstance(h, cls)


def test_handle_isinstance_distinguishes_types() -> None:
    h = ObjectClassHandle(5)
    assert isinstance(h, ObjectClassHandle) is True
    assert isinstance(h, AttributeHandle) is False
    assert isinstance(h, int) is True


@pytest.mark.parametrize("cls", ALL_HANDLES)
def test_handle_repr_shows_type(cls: type) -> None:
    assert repr(cls(5)) == f"{cls.__name__}(5)"


def test_handle_usable_as_dict_key_with_bare_int_lookup() -> None:
    # int-equality means a bare-int key lookup hits a wrapper-keyed entry.
    # Annotate as dict[int, str] so mypy --strict accepts the bare-int
    # lookup; at runtime an ObjectClassHandle inserts as the key (int-
    # subclass) and `d[5]` resolves by hash/equality, which is the
    # behavior the test pins.
    d: dict[int, str] = {ObjectClassHandle(5): "x"}
    assert d[5] == "x"
    assert d[ObjectClassHandle(5)] == "x"


def test_cross_type_handles_compare_equal_by_int() -> None:
    # Deliberate dual-accept concession — see docs/PITCH_PARITY.md and
    # the docstring on rti1516e.handles._StrongHandle. Code that needs
    # strict type-distinction must use isinstance(). The compares below
    # are int-equality semantics; mypy's comparison-overlap warning
    # would mask this concession so we widen the operands to ``int`` to
    # document that ``int``-equality is exactly what's being asserted.
    oc: int = ObjectClassHandle(5)
    at: int = AttributeHandle(5)
    assert oc == at
    assert hash(oc) == hash(at)
    # isinstance is the correct way to distinguish:
    assert not isinstance(AttributeHandle(5), ObjectClassHandle)


def test_handle_arithmetic_preserves_int_semantics() -> None:
    h = AttributeHandle(5)
    # int-subclass arithmetic returns bare int (Python's default for int ops).
    assert h + 1 == 6
    assert int(h) + 0 == 5
