"""pyjevsim bridge — PortMapping construction + lookups.

Implements: FR-PYJ-2.
"""

from __future__ import annotations

import pytest

from pyjevsim_bridge import PortMapping


@pytest.mark.spec
def test_spec_m4_port_mapping_builds_from_dict() -> None:
    pm = PortMapping.from_dict({"out_pos": "Position", "in_cmd": "Command"})
    assert pm.fom_class_for_out_port("out_pos") == "Position"
    assert pm.in_port_for_fom_class("Command") == "in_cmd"


@pytest.mark.spec
def test_spec_m4_port_mapping_unknown_returns_none() -> None:
    pm = PortMapping.from_dict({"out_pos": "Position"})
    assert pm.fom_class_for_out_port("nope") is None
    assert pm.in_port_for_fom_class("Nope") is None
