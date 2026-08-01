"""Real pyjevsim models used by the gorti integration example."""

from __future__ import annotations

from typing import Any

from pyjevsim.behavior_model import BehaviorModel


class PulseGenerator(BehaviorModel):
    """Periodic pyjevsim atomic model that emits a monotonic sequence."""

    def __init__(self) -> None:
        super().__init__("pulse-generator")
        self.insert_state("active", deadline=1.0)
        self.init_state("active")
        self.insert_output_port("out_seq")
        self.sequence = 1
        self.published: list[int] = []

    def output(self, deliverer: Any) -> None:
        value = self.sequence
        deliverer.insert_message("out_seq", value.to_bytes(4, "big"))
        self.published.append(value)

    def int_trans(self) -> None:
        self.sequence += 1

    def ext_trans(self, port: str, message: Any) -> None:
        del port, message


class PulseSink(BehaviorModel):
    """pyjevsim atomic model that records HLA-delivered sequences."""

    def __init__(self) -> None:
        super().__init__("pulse-sink")
        self.insert_state("waiting", deadline=float("inf"))
        self.init_state("waiting")
        self.insert_input_port("in_seq")
        self.received: list[int] = []

    def output(self, deliverer: Any) -> None:
        del deliverer

    def int_trans(self) -> None:
        return

    def ext_trans(self, port: str, message: Any) -> None:
        if port != "in_seq" or not isinstance(message, dict):
            return
        wire = message.get("_payload")
        if wire is None and len(message) == 1:
            wire = next(iter(message.values()))
        if isinstance(wire, (bytes, bytearray)) and len(wire) == 4:
            self.received.append(int.from_bytes(wire, "big"))
