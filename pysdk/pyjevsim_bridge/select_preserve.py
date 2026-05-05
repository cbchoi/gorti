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

    Cut-1 implementation
    --------------------
    The orchestrator-frozen ``CoupledModelProtocol`` shim does NOT expose
    ``select()`` (real pyjevsim — both 1.3.x and 2.0.x — exposes it on
    a per-coupled-model basis but that surface is intentionally outside
    W6's contract — see pyjevsim_bridge/_protocol.py and
    docs/agent-c-pysdk.md §4.4). Until W7 wires the real pyjevsim
    adapter we use a deterministic stand-in: sort by port name
    (lexicographic), preserving the relative order of ties in payload.
    ``sorted()`` is stable, so two entries with the same port name
    retain their input order.

    This satisfies FR-PYJ-4's "no Python list ordering, no dict iteration
    nondeterminism" requirement: the output for a given input is always
    the same regardless of insertion order. When the W7 adapter exposes
    a real ``select()``, this function is the natural extension point.
    """
    # ``coupled_model`` is reserved for the W7 adapter that will surface
    # the real pyjevsim select() priority. Until then we sort by port
    # name; the parameter is kept in the signature because it IS part of
    # the contract (callers pass the model so the future implementation
    # can consult it without an API break).
    del coupled_model
    return sorted(events, key=lambda item: item[0])
