"""Unit tests for ``pyjevsim_bridge.PortMapping``.

These cover the cut-1 direction-inference branches not exercised by the
specification tests under ``tests/spec/m4/``:

* mixed in/out dicts split correctly into the two backing maps,
* an unknown prefix is rejected loudly with a :class:`ValueError`,
* an empty mapping yields empty maps,
* the dataclass is hashable / frozen.
"""

from __future__ import annotations

import dataclasses

import pytest

from pyjevsim_bridge import PortMapping


def test_from_dict_splits_in_and_out() -> None:
    pm = PortMapping.from_dict(
        {
            "out_pos": "Position",
            "out_vel": "Velocity",
            "in_cmd": "Command",
            "in_ack": "Ack",
        }
    )
    assert pm.out_ports == {"out_pos": "Position", "out_vel": "Velocity"}
    assert pm.in_ports == {"Command": "in_cmd", "Ack": "in_ack"}


def test_from_dict_rejects_unknown_prefix() -> None:
    with pytest.raises(ValueError, match="lacks an 'out_' or 'in_' prefix"):
        PortMapping.from_dict({"position": "Position"})


def test_from_dict_empty_mapping() -> None:
    pm = PortMapping.from_dict({})
    assert pm.out_ports == {}
    assert pm.in_ports == {}
    assert pm.fom_class_for_out_port("anything") is None
    assert pm.in_port_for_fom_class("anything") is None


def test_lookup_helpers_round_trip() -> None:
    pm = PortMapping.from_dict({"out_pos": "Position", "in_cmd": "Command"})
    assert pm.fom_class_for_out_port("out_pos") == "Position"
    assert pm.fom_class_for_out_port("in_cmd") is None
    assert pm.in_port_for_fom_class("Command") == "in_cmd"
    assert pm.in_port_for_fom_class("Position") is None


def test_dataclass_is_frozen() -> None:
    pm = PortMapping.from_dict({"out_pos": "Position"})
    with pytest.raises(dataclasses.FrozenInstanceError):
        pm.out_ports = {}  # type: ignore[misc]
