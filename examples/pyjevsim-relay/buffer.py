"""Buffer coupled-model: DEVS bounded-FIFO queue between the Generator
and the Processor.

Receives ``GenToBuffer`` interactions on the ``in_msg`` port, holds
them in a fixed-capacity FIFO, and emits the head of the queue as
``BufferToProc`` on the ``out_msg`` port every ``service_period``
ticks.

Drop-on-overflow policy: if a new external arrival lands while the
queue is at capacity, the message is dropped silently and the seq
number is appended to ``dropped`` so the runner can verify the
end-to-end accounting (gen.published == proc.received ∪ buf.dropped).
"""

from __future__ import annotations

from typing import Any


class Buffer:
    """DEVS Queue. Bounded FIFO with drop-on-overflow + service-rate
    pacing.

    Parameters
    ----------
    capacity : int
        Maximum number of in-flight messages. New arrivals while full
        are dropped (the seq is recorded in ``dropped``).
    service_period : int
        Emit one queued message every Nth ``output_handler`` call.
        ``service_period == 1`` is line-rate (drop only when arrivals
        exceed 1/tick); ``service_period == 2`` halves the drain rate
        and lets the queue build up under steady-rate input.

    Attributes
    ----------
    queue : list[int]
        Sequence numbers currently buffered (FIFO, head at index 0).
    dropped : list[int]
        Sequence numbers refused on overflow, in arrival order.
    forwarded : list[int]
        Sequence numbers actually emitted to the processor, in
        emission order. ``forwarded + dropped == every external seen``.
    """

    def __init__(self, *, capacity: int = 5, service_period: int = 2) -> None:
        if capacity < 1:
            raise ValueError("Buffer.capacity must be >= 1")
        if service_period < 1:
            raise ValueError("Buffer.service_period must be >= 1")
        self.capacity = capacity
        self.service_period = service_period
        self._tick = 0
        self.queue: list[int] = []
        self.dropped: list[int] = []
        self.forwarded: list[int] = []

    def time_advance(self) -> float:
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        # Service-rate pacing: only emit on every Nth tick. Returning
        # an empty dict means "no output this tick"; internal_transition
        # then leaves the queue head in place.
        self._tick += 1
        if not self.queue:
            return {}
        if self._tick % self.service_period != 0:
            return {}
        head = self.queue[0]
        payload = head.to_bytes(4, byteorder="big", signed=False)
        return {"out_msg": payload}

    def internal_transition(self) -> None:
        # Mirror the gating logic in output_handler: only pop when we
        # actually emitted. Reading _tick (not bumping it) — the bump
        # happened in output_handler immediately before this call.
        if not self.queue:
            return
        if self._tick % self.service_period != 0:
            return
        head = self.queue.pop(0)
        self.forwarded.append(head)

    def external_transition(self, port: str, payload: Any) -> None:
        # Bridge wraps the wire bytes under ``_payload``; see
        # pysdk/pyjevsim_bridge/time_advance.py::_run_internal_cycle.
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
        if len(self.queue) >= self.capacity:
            self.dropped.append(seq)
            return
        self.queue.append(seq)
