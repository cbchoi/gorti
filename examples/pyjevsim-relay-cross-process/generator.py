"""Generator coupled-model: DEVS source emitting one ``GenToBuffer``
interaction per tick.

Identical-shape to ``examples/pyjevsim-relay/generator.py``; this copy
lives alongside the cross-process runner so the federate's ``__main__``
has a sibling-importable model module without a hard dependency on the
in-process example tree (the two examples can evolve independently).
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
        Sequence numbers actually emitted, in order. The federate's
        ``__main__`` reads this list and writes it to the result file
        before exiting; the runner aggregates the three result files
        and verifies the end-to-end accounting.
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
