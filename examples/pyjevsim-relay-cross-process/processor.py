"""Processor coupled-model: DEVS sink that consumes ``BufferToProc``
interactions and records what it saw.

Identical-shape to ``examples/pyjevsim-relay/processor.py``. Pure
subscriber: no internal scheduling, no output. Each external arrival
decodes the seq number and appends to ``received``. The runner then
asserts ``set(received) == set(forwarded)`` after aggregating the
three federate result files.
"""

from __future__ import annotations

from typing import Any


class Processor:
    """DEVS Sink.

    Attributes
    ----------
    received : list[int]
        Sequence numbers consumed from the buffer, in arrival order.
    """

    def __init__(self) -> None:
        self.received: list[int] = []

    def time_advance(self) -> float:
        # Always tick at 1.0 so processor and buffer share a clock; the
        # processor never produces output, but a finite time_advance
        # keeps the bridge's NER loop driving forward when the runner
        # calls step_once.
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        return {}

    def internal_transition(self) -> None:
        pass

    def external_transition(self, port: str, payload: Any) -> None:
        if port != "in_msg":
            return
        if isinstance(payload, dict) and "_payload" in payload:
            raw = payload["_payload"]
        elif isinstance(payload, bytes):
            raw = payload
        else:
            return
        try:
            seq = int.from_bytes(raw, byteorder="big", signed=False)
        except (TypeError, ValueError):
            return
        self.received.append(seq)
