"""Dashboard federate model: receives ReflectAttributeValues events for
the SensorReading object class and accumulates the reflected sequence.

Pure subscriber. The runner subscribes the dashboard federate to
``SensorReading.value`` via ``Federate.subscribe_object_class`` and
fans out the sensor's ``update_attributes`` calls as
``ReflectAttributeValues`` events into the dashboard's event queue.
The dashboard's ``handle_reflect`` method decodes the wire bytes and
appends to ``received``.

Two events are observed:

  * ``DiscoverObjectInstance``: arrives once when the dashboard first
    sees the sensor-side object. Recorded in ``discovered`` for
    debugging; the dashboard can already accept reflects without it
    (the object_handle is in the reflect event itself).
  * ``ReflectAttributeValues``: arrives once per sensor update.
    Decoded and appended to ``received``.
"""

from __future__ import annotations

from typing import Any


class Dashboard:
    """Logical model of the dashboard federate. Transport-free; the
    runner pushes events into ``handle_*`` methods.

    Attributes
    ----------
    received : list[int]
        Decoded ``value`` integers, in arrival order. Equals the
        sensor's ``published`` list at end of run if no reflects
        were lost.
    discovered : list[tuple[int, str]]
        ``(object_handle, instance_name)`` pairs for each
        DiscoverObjectInstance the federate observed. Useful for
        confirming the discover-before-reflect ordering invariant.
    """

    def __init__(self) -> None:
        self.received: list[int] = []
        self.discovered: list[tuple[int, str]] = []

    def handle_discover(self, object_handle: int, instance_name: str) -> None:
        self.discovered.append((object_handle, instance_name))

    def handle_reflect(self, values: dict[str, Any]) -> None:
        """Decode the ``value`` attribute from a reflect-event payload.

        Wire bytes are 4-byte big-endian signed; the runner's
        synthesizer mirrors what the sensor wrote. Skips silently
        if the attribute we care about is missing (matches the
        bridge's "drop unrecognized" convention).
        """
        raw = values.get("value")
        if raw is None:
            return
        if isinstance(raw, bytes):
            seq = int.from_bytes(raw, byteorder="big", signed=True)
        elif isinstance(raw, int):
            seq = raw
        else:
            return
        self.received.append(seq)
