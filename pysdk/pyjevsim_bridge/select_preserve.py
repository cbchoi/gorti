"""pyjevsim select() preservation under simultaneous events.

Agent C implements per TASK-071. When the bridge is about to deliver
multiple events with the same timestamp, it must sort by pyjevsim's
``select()`` order BEFORE handing to HLA — so the HLA tie-break
(federate→object→attribute handle) sees them in the order pyjevsim
would have chosen.
"""

from __future__ import annotations

from typing import Any

from pyjevsim_bridge._protocol import CoupledModelProtocol


def order_simultaneous_events(
    coupled_model: CoupledModelProtocol,
    events: list[tuple[str, Any]],
) -> list[tuple[str, Any]]:
    """Reorder ``events`` (list of (port_name, payload)) by pyjevsim's
    select() priority.

    Real pyjevsim exposes select() at the coupled-model level; the bridge
    invokes it to get the priority ordering. The bridge must NOT rely on
    Python list ordering, dict iteration order, or any other source of
    nondeterminism.

    Raises NotImplementedError until TASK-071.
    """
    raise NotImplementedError("TASK-071")
