"""Regulating federate model — emits a Heartbeat interaction every
cycle and advances logical time by ``step`` seconds per grant.

Three of these run in the runner with three different lookaheads
({0.5, 1.0, 2.0}). The federate with the smallest ``current + lookahead``
is the one whose NER grant will fire earliest in any tick — that's the
LBTS rule.

This example uses the ``pyjevsim_bridge`` directly (Heartbeat is an
interaction, which the bridge supports natively). The
``output_handler`` returns a single port carrying the federate's
current logical time so the wire log is decodable.
"""

from __future__ import annotations

import struct
from typing import Any


class Regulator:
    """One regulating federate. Logical clock + per-cycle heartbeat.

    Parameters
    ----------
    name : str
        Federate name, used both for federate registration and for
        labelling the wire-log payload (lets the runner correlate
        receive events with senders).
    step : float
        Time-advance step requested per cycle (passed back as the
        bridge's ``time_advance()`` value). Bridge issues
        ``next_message_request(now + step)`` on each cycle.
    lookahead : float
        Federate lookahead, passed to
        ``Federate.enable_time_regulation(lookahead)`` by the runner.
        Determines this federate's LBTS contribution
        (``current + lookahead``).

    Attributes
    ----------
    sent : list[float]
        Logical time at the moment each heartbeat was emitted.
    """

    def __init__(
        self,
        *,
        name: str,
        step: float = 1.0,
        lookahead: float = 1.0,
    ) -> None:
        if step <= 0:
            raise ValueError("Regulator.step must be > 0")
        if lookahead < 0:
            raise ValueError("Regulator.lookahead must be >= 0")
        self.name = name
        self.step = step
        self.lookahead = lookahead
        self._now = 0.0
        self.sent: list[float] = []

    @property
    def now(self) -> float:
        return self._now

    def time_advance(self) -> float:
        # Bridge calls this to determine ``ta``. Constant per-federate
        # step gives a predictable NER stream; differing steps would
        # interleave grants in a way that's already exercised by the
        # research-platform alt-strategies in `rti/internal/time/`.
        return self.step

    def output_handler(self) -> dict[str, Any]:
        # Emit one heartbeat per cycle. Payload is the federate's own
        # logical time encoded as 8-byte big-endian double — matches
        # the FOM's HLAfloat64BE declaration.
        payload = struct.pack(">d", self._now)
        self.sent.append(self._now)
        return {"out_heartbeat": payload}

    def internal_transition(self) -> None:
        # Advance the local logical clock by step. This mirrors what
        # the bridge will see on the next ``time_advance()`` call.
        self._now += self.step

    def external_transition(self, port: str, payload: Any) -> None:
        # Regulators in this example don't act on incoming heartbeats;
        # the runner observes peers via the wire log, not via DEVS
        # transitions. Accept-and-drop keeps the bridge's general
        # delivery path uniform.
        del port, payload
