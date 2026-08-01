"""Sensor coupled-model — Path B (bridged) variant.

This is the OBJECT-CLASS analogue of the producer/consumer interaction
models in ``examples/pyjevsim/``. Where the bypass example
(``examples/pyjevsim-dashboard/sensor.py``) drives
``Federate.register_object_instance`` + ``Federate.update_attributes``
directly from the runner, this file implements the new
``ObjectClassFederateProtocol`` surface so the bridge owns those
calls.

Hooks the bridge calls on this model:

  * ``object_class_publications`` — declare ``SensorReading.value``
    is published.
  * ``register_instances`` — register ``"sensor-1"`` of class
    ``SensorReading`` at startup.
  * ``time_advance`` / ``output_handler`` / ``internal_transition`` —
    bridge's standard interaction-cycle hooks (no interactions in
    this example, so output is empty and ta returns 1.0).
  * ``attribute_update_handler`` — drained every cycle; returns
    ``{"sensor-1": {"value": <bytes>}}`` or ``{}`` once the model
    has reached its tick cap.

The model is transport-free (no ``Federate``, no
``InProcessTransport``); the bridge does all RTI calls. Same
``stop_after`` / ``mode`` / ``amplitude`` knobs as the bypass variant
so the runner can reuse the verifier.
"""

from __future__ import annotations

import math


class Sensor:
    """Object-class publisher for the Path-B bridged dashboard.

    Implements ``ObjectClassFederateProtocol`` (publications +
    instance registration + per-cycle attribute updates) plus the
    minimal ``CoupledModelProtocol`` (time_advance + output_handler
    + internal_transition + external_transition) the bridge needs
    for its interaction cycle.

    Parameters
    ----------
    stop_after : int
        Stop publishing after this many ticks.
    mode : {"sequence", "sine"}
        ``sequence`` publishes ``i`` on tick ``i``; ``sine`` publishes
        ``round(amp * sin(2 pi i / 8))``.
    amplitude : int
        For ``mode="sine"``: integer amplitude.
    """

    INSTANCE_NAME = "sensor-1"
    CLASS_NAME = "SensorReading"
    ATTR_NAME = "value"

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

    # --- CoupledModelProtocol surface ---

    def time_advance(self) -> float:
        """Constant 1.0 — one publish per cycle. The runner controls
        the total cycle count."""
        return 1.0

    def output_handler(self) -> dict[str, object]:
        """No interactions in this example; return ``{}``."""
        return {}

    def internal_transition(self) -> None:
        """Tick increments here so a single cycle = one published
        value."""
        if self._tick < self.stop_after:
            self._tick += 1

    def external_transition(self, port: str, payload: object) -> None:
        """No subscriptions; defensive no-op."""
        return None

    # --- ObjectClassFederateProtocol opt-ins ---

    def object_class_publications(self) -> dict[str, list[str]]:
        """Publish SensorReading.value."""
        return {self.CLASS_NAME: [self.ATTR_NAME]}

    def register_instances(self) -> dict[str, str]:
        """Register exactly one instance ("sensor-1") at startup."""
        return {self.INSTANCE_NAME: self.CLASS_NAME}

    def attribute_update_handler(self) -> dict[str, dict[str, object]]:
        """Compute the next ``value`` and emit a single
        update_attributes call.

        Returns ``{}`` once the model has reached ``stop_after`` so
        no further wire calls are made — the runner can drive a few
        extra cycles without polluting the per-tick accounting.
        """
        if self._tick >= self.stop_after:
            return {}
        if self.mode == "sequence":
            value = self._tick
        else:  # "sine"
            value = int(
                round(self.amplitude * math.sin(2 * math.pi * self._tick / 8))
            )
        self.published.append(value)
        payload = value.to_bytes(4, byteorder="big", signed=True)
        return {self.INSTANCE_NAME: {self.ATTR_NAME: payload}}
