"""Consumer coupled-model: collects ``ProducerOutput`` interactions.

Implements the bridge's ``CoupledModelProtocol`` via duck typing. Like
:class:`producer.Producer`, this is intentionally a plain Python class —
the pyjevsim ``StructuralModel`` adapter is a follow-up.
"""

from __future__ import annotations

from typing import Any


class Consumer:
    """Subscribe-only model: every external arrival is appended to ``received``.

    The input port name is ``in_seq`` so that
    :class:`pyjevsim_bridge.PortMapping.from_dict` infers an incoming
    direction from the ``in_`` prefix.
    """

    def __init__(self) -> None:
        # (port_name, payload) tuples in arrival order. Public so the
        # runner / determinism harness can hash it as the determinism
        # witness — ``repr(self.received)`` is what gets sha256'd.
        self.received: list[tuple[str, Any]] = []

    def time_advance(self) -> float:
        """Cycle every 1.0 logical second so consumer and producer share a
        clock; nothing forces them to match, but it keeps the interleaving
        in the determinism harness easy to reason about.
        """
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        """Subscribe-only: no outgoing payload."""
        return {}

    def internal_transition(self) -> None:
        """No internal state to advance."""

    def external_transition(self, port: str, payload: Any) -> None:
        """Record the (port, payload) pair in arrival order."""
        # The bridge wraps the wire payload under ``_payload`` (see
        # pyjevsim_bridge/time_advance.py::_run_internal_cycle), so the
        # parameter dict shape is ``{"_payload": <bytes>}``. Keep the
        # raw payload around so the determinism witness covers the wire
        # bytes, not just the port name.
        self.received.append((port, payload))
