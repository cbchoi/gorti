"""Dashboard coupled-model — Path B (bridged) variant.

Object-class subscriber. Implements
``ObjectClassFederateProtocol`` (subscriptions + handlers) plus
the minimal ``CoupledModelProtocol`` the bridge needs.

Hooks the bridge calls on this model:

  * ``object_class_subscriptions`` — declare interest in
    ``SensorReading.value``. The bridge issues
    ``Federate.subscribe_object_class`` once at startup.
  * ``discover_handler(handle, class, name)`` — invoked once when
    the bridge sees the sensor's instance.
  * ``reflect_handler(handle, attrs)`` — invoked once per
    ``ReflectAttributeValues`` event the bridge routes here.

Wire bytes (4-byte big-endian signed) are decoded inside
``reflect_handler`` exactly as in the bypass variant — that's
deliberately the same logic so the verifier can compare outputs
side-by-side.
"""

from __future__ import annotations

from typing import Any


class Dashboard:
    """Object-class subscriber for the Path-B bridged dashboard.

    Attributes
    ----------
    received : list[int]
        Decoded ``value`` integers, in arrival order. Equals the
        sensor's ``published`` list at end of run if no reflects
        were lost.
    discovered : list[tuple[int, str]]
        ``(object_handle, instance_name)`` pairs for each
        ``DiscoverObjectInstance`` the bridge routed here.
    """

    CLASS_NAME = "SensorReading"
    ATTR_NAME = "value"

    def __init__(self) -> None:
        self.received: list[int] = []
        self.discovered: list[tuple[int, str]] = []

    # --- CoupledModelProtocol surface ---

    def time_advance(self) -> float:
        """Pure subscriber: ta is constant 1.0. The bridge's
        external-first short-circuit means a queued reflect bumps
        the cycle straight to ``_drain_pending_external``, so this
        ta is only used when no reflects are pending (idle ticks)."""
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        """No outputs — interactions or attribute updates."""
        return {}

    def internal_transition(self) -> None:
        return None

    def external_transition(self, port: str, payload: Any) -> None:
        """No interaction subscriptions; defensive no-op."""
        return None

    # --- ObjectClassFederateProtocol opt-ins ---

    def object_class_subscriptions(self) -> dict[str, list[str]]:
        """Subscribe to SensorReading.value."""
        return {self.CLASS_NAME: [self.ATTR_NAME]}

    def discover_handler(
        self,
        instance_handle: int,
        class_name: str,
        instance_name: str,
    ) -> None:
        """Record the (handle, name) for verifier inspection.

        ``class_name`` is also visible — present for future-
        compatibility (a multi-class dashboard could route by
        class), but the single-class verifier here doesn't need it.
        """
        del class_name  # single-class dashboard; not used.
        self.discovered.append((instance_handle, instance_name))

    def reflect_handler(
        self,
        instance_handle: int,
        attrs: dict[str, Any],
    ) -> None:
        """Decode ``value`` (4-byte big-endian signed) from the
        attribute dict and append to ``received``.

        Skips silently if the attribute is missing — same
        defensive convention the bypass variant uses, and matches
        the bridge's "drop unrecognized" interaction-side rule.
        """
        del instance_handle  # single-instance dashboard; not used.
        raw = attrs.get(self.ATTR_NAME)
        if raw is None:
            return
        if isinstance(raw, bytes):
            seq = int.from_bytes(raw, byteorder="big", signed=True)
        elif isinstance(raw, int):
            seq = raw
        else:
            return
        self.received.append(seq)
