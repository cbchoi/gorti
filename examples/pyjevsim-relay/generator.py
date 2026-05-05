"""Generator coupled-model: DEVS source emitting one ``GenToBuffer``
interaction per tick.

Implements the bridge's ``CoupledModelProtocol`` (see
``pysdk/pyjevsim_bridge/_protocol.py``) via duck typing. Mirrors the
existing ``examples/pyjevsim/producer.py`` pattern but stamps the
``GenToBuffer`` interaction class instead of ``ProducerOutput``.

Stops emitting after ``stop_after`` messages so the runner's drain
phase can flush whatever's still queued in the buffer.
"""

from __future__ import annotations

from typing import Any


class Generator:
    """DEVS Source. Tick every 1.0 logical second; emit a monotonically
    increasing sequence number on the ``out_seq`` port.

    Attributes
    ----------
    stop_after : int
        Stop emitting once ``len(published) >= stop_after``. The bridge's
        ``output_handler`` then returns ``{}`` and the federate is
        effectively idle on subsequent ticks.
    published : list[int]
        Sequence numbers actually emitted, in order. Public so the
        runner can verify the pipeline end-to-end.
    """

    def __init__(self, *, stop_after: int = 50) -> None:
        self.stop_after = stop_after
        self._tick = 0
        self.published: list[int] = []

    def time_advance(self) -> float:
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        if len(self.published) >= self.stop_after:
            return {}
        self._tick += 1
        self.published.append(self._tick)
        payload = self._tick.to_bytes(4, byteorder="big", signed=False)
        return {"out_seq": payload}

    def internal_transition(self) -> None:
        # output_handler already advanced the sequence counter; nothing
        # else to advance here. Kept for protocol symmetry.
        pass

    def external_transition(self, port: str, payload: Any) -> None:
        # Generator is publish-only; the bridge never delivers externals
        # to it under the runner's port mapping, but we accept-and-drop
        # rather than raise so the bridge's general delivery path stays
        # uniform.
        del port, payload
