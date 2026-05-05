"""Sensor federate model: registers a SensorReading object instance and
updates its ``value`` attribute every tick.

This federate uses the **lower-level pysdk surface** directly
(``RtiConnection`` + ``Federate.register_object_instance`` +
``Federate.update_attributes``) rather than the ``pyjevsim_bridge``
HLAFederate. The bridge is interaction-only at this cut: its
``port_mapping.py`` wraps payloads under ``_payload`` and its
``time_advance.py`` calls ``send_interaction`` from the
``output_handler`` dict — there is no equivalent path for object-class
attribute updates. See README.md "Why this example bypasses the bridge".

The sensor produces a deterministic sequence (default: ``i`` for each
tick ``i``) so the dashboard's reflected list can be verified by the
runner with a simple equality check. Swap ``mode="sine"`` to publish
a quantised sine wave (the same sequence the unit-tested FOM coding
samples produce) — useful for showing the example with a non-monotonic
verification target.
"""

from __future__ import annotations

import math
from typing import Any


class Sensor:
    """Logical model of the sensor federate. Stateful but transport-free
    — the runner is responsible for calling ``register`` once and
    ``tick`` per cycle, then writing the resulting attribute updates
    to the federate.

    Parameters
    ----------
    stop_after : int
        Stop publishing after this many ticks. The federate stays
        joined but its ``tick`` method returns ``None`` once the
        cap is reached.
    mode : {"sequence", "sine"}
        ``"sequence"`` publishes ``i`` on tick ``i`` (default; trivial
        verification). ``"sine"`` publishes ``round(amp * sin(...))``.
    amplitude : int
        For ``mode="sine"``: integer amplitude (max absolute value).

    Attributes
    ----------
    published : list[int]
        Sequence of ``value`` updates the model emitted, in tick order.
        Public so the runner can compare against the dashboard's view.
    """

    def __init__(
        self,
        *,
        stop_after: int = 10,
        mode: str = "sequence",
        amplitude: int = 100,
    ) -> None:
        if stop_after < 0:
            raise ValueError("Sensor.stop_after must be >= 0")
        if mode not in ("sequence", "sine"):
            raise ValueError(
                f"Sensor.mode must be 'sequence' or 'sine'; got {mode!r}"
            )
        self.stop_after = stop_after
        self.mode = mode
        self.amplitude = amplitude
        self._tick = 0
        self.published: list[int] = []

    def tick(self) -> int | None:
        """Compute the next ``value`` to publish, append it to
        ``published``, and return it. Returns ``None`` once the
        ``stop_after`` cap is reached so the runner can stop calling
        ``update_attributes``.
        """
        if self._tick >= self.stop_after:
            return None
        i = self._tick
        self._tick += 1
        if self.mode == "sequence":
            value = i
        else:  # "sine"
            # 8-step sine wave at integer amplitude. Period = 8 ticks
            # so a 16-tick run shows two full periods.
            value = int(round(self.amplitude * math.sin(2 * math.pi * i / 8)))
        self.published.append(value)
        return value
