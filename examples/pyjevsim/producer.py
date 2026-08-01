"""Producer coupled-model: emits one ``ProducerOutput`` interaction per tick.

Implements the bridge's ``CoupledModelProtocol`` (see
``pysdk/pyjevsim_bridge/_protocol.py``) via duck typing, mirroring the
DEVS-canonical method names ``time_advance`` / ``output_handler`` /
``internal_transition`` / ``external_transition``.

Real pyjevsim 1.3.x ``StructuralModel`` integration is intentionally out
of scope for cut-1; the specification tests require only the Protocol shape.
"""

from __future__ import annotations

from typing import Any


class Producer:
    """Tick every 1.0 second; emit a monotonically increasing sequence number.

    The output port name is ``out_seq`` so that
    :class:`pyjevsim_bridge.PortMapping.from_dict` infers an outgoing
    direction from the ``out_`` prefix.
    """

    def __init__(self) -> None:
        self._tick = 0
        # Track the sequence numbers we have published so the runner can
        # diff producer output against consumer receipts in determinism
        # checks. Public attribute on purpose — the test harness reads it.
        self.published: list[int] = []

    def time_advance(self) -> float:
        """Cycle every 1.0 logical second."""
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        """Stamp the next sequence number and emit it on ``out_seq``.

        Big-endian 4-byte payload matches the FOM datatype declared in
        ``tests/conformance/foms/good/pyjevsim-bridge.xml`` (HLAinteger32BE).
        Cut-1 transmits the encoded bytes opaquely; full FOM-driven
        encoding is a M5 follow-up.
        """
        self._tick += 1
        self.published.append(self._tick)
        payload = self._tick.to_bytes(4, byteorder="big", signed=False)
        return {"out_seq": payload}

    def internal_transition(self) -> None:
        """No additional state to advance — ``output_handler`` already
        bumped the tick counter (it has to, because ``output_handler``
        is also where the deterministic payload is stamped)."""

    def external_transition(self, port: str, payload: Any) -> None:
        """Producer is publish-only; external interactions are ignored.

        Kept as a no-op rather than raising so the bridge's general
        delivery path doesn't have to special-case publish-only models.
        """
        del port, payload
