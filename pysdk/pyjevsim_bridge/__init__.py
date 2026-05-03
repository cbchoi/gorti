"""DEVS↔HLA bridge for pyjevsim coupled models.

Public API:

    from pyjevsim_bridge import HLAFederate, PortMapping

    federate = HLAFederate(
        coupled_model=my_coupled_model,
        federation=FederationSpec(name="demo", fom_modules=["./demo.fom.xml"]),
        federate_name="alice",
        port_mapping=PortMapping({
            "out_position": "Position",   # pyjevsim port -> FOM interaction class
            "in_command": "Command",
        }),
    )
    await federate.run(ticks=100)

Implementation lives across TASK-069..072. See docs/agent-c-pysdk.md §6
for the DEVS↔HLA mapping reference.
"""

from __future__ import annotations

from pyjevsim_bridge.port_mapping import PortMapping
from pyjevsim_bridge.time_advance import HLAFederate

__all__ = ["HLAFederate", "PortMapping"]
