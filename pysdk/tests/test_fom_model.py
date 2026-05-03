"""Unit tests for the FOM dataclass lookup helpers (TASK-060)."""

from __future__ import annotations

from rti1516e.fom import (
    FOM,
    Attribute,
    BasicData,
    InteractionClass,
    ObjectClass,
    Parameter,
    SimpleData,
)


def _sample_fom() -> FOM:
    obj_root = ObjectClass(name="HLAobjectRoot", parent=None, attributes=())
    obj_speed = ObjectClass(
        name="HLAobjectRoot.Vehicle",
        parent="HLAobjectRoot",
        attributes=(Attribute(name="Speed", data_type="Speed"),),
    )
    int_root = InteractionClass(
        name="HLAinteractionRoot",
        parent=None,
        parameters=(),
    )
    int_msg = InteractionClass(
        name="HLAinteractionRoot.Message",
        parent="HLAinteractionRoot",
        parameters=(Parameter(name="Body", data_type="HLAunicodeString"),),
    )
    dt_int = BasicData(name="HLAinteger32BE", size=4, endianness="BE")
    dt_speed = SimpleData(name="Speed", representation="HLAfloat64BE")
    return FOM(
        object_classes=(obj_root, obj_speed),
        interaction_classes=(int_root, int_msg),
        data_types=(dt_int, dt_speed),
    )


def test_find_object_class_hit() -> None:
    fom = _sample_fom()
    found = fom.find_object_class("HLAobjectRoot.Vehicle")
    assert found is not None
    assert found.parent == "HLAobjectRoot"
    assert found.attributes[0].name == "Speed"


def test_find_object_class_miss_returns_none() -> None:
    fom = _sample_fom()
    assert fom.find_object_class("NoSuchClass") is None


def test_find_interaction_class_hit() -> None:
    fom = _sample_fom()
    found = fom.find_interaction_class("HLAinteractionRoot.Message")
    assert found is not None
    assert found.parameters[0].data_type == "HLAunicodeString"


def test_find_interaction_class_miss_returns_none() -> None:
    fom = _sample_fom()
    assert fom.find_interaction_class("HLAinteractionRoot.Missing") is None


def test_find_data_type_hit_basic() -> None:
    fom = _sample_fom()
    found = fom.find_data_type("HLAinteger32BE")
    assert isinstance(found, BasicData)
    assert found.size == 4
    assert found.endianness == "BE"


def test_find_data_type_hit_simple() -> None:
    fom = _sample_fom()
    found = fom.find_data_type("Speed")
    assert isinstance(found, SimpleData)
    assert found.representation == "HLAfloat64BE"


def test_find_data_type_miss_returns_none() -> None:
    fom = _sample_fom()
    assert fom.find_data_type("HLAinteger64BE") is None


def test_empty_fom_lookups_return_none() -> None:
    fom = FOM()
    assert fom.find_object_class("anything") is None
    assert fom.find_interaction_class("anything") is None
    assert fom.find_data_type("anything") is None
