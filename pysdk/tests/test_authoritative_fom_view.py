from types import SimpleNamespace as NS  # noqa: N814 - compact wire fixture

import pytest

from rti1516e._transport import GrpcTransport


def view():
    return NS(
        revision=3,
        object_classes=[
            NS(name="HLAobjectRoot.Sensor", handle=71, members=[NS(name="value", handle=9)])
        ],
        interaction_classes=[
            NS(name="HLAinteractionRoot.Tick", handle=84, members=[NS(name="step", handle=6)])
        ],
    )


def test_authoritative_bindings_replace_local_positions():
    transport = object.__new__(GrpcTransport)
    transport._install_fom_view(view())
    assert transport._object_class_handle_for("Sensor") == 71
    assert transport._attribute_handle_for("Sensor", "value") == 9
    assert transport._interaction_handle_for("Tick") == 84
    assert transport._parameter_indices_for("Tick") == {"step": 6}


def test_invalid_view_does_not_replace_working_tables():
    transport = object.__new__(GrpcTransport)
    transport._install_fom_view(view())
    invalid = view()
    invalid.object_classes[0].members[0].handle = 0
    with pytest.raises(ValueError):
        transport._install_fom_view(invalid)
    assert transport._attribute_handle_for("Sensor", "value") == 9


def test_ambiguous_short_class_name_is_not_resolved():
    transport = object.__new__(GrpcTransport)
    model = view()
    model.object_classes.append(NS(name="HLAobjectRoot.Other.Sensor", handle=72, members=[]))
    transport._install_fom_view(model)
    assert transport._object_class_handle_for("Sensor") == 0
    assert transport._object_class_handle_for("Other.Sensor") == 72
