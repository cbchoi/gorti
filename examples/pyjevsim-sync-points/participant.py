"""Participant federate — joins the federation, achieves named sync
points on demand, and emits a Tick heartbeat during the running phase.

The federate model is deliberately tiny: the pedagogical focus of
this example is the rendezvous semantics (the runner-driven sync
gating), not the per-cycle compute.

Sync-point lifecycle modelled here:

   pending  ──register──▶  registered
   registered ──achieve──▶ achieved
   achieved (all peers achieved) ──synchronized──▶ released

The federate exposes ``achieve(label)`` and
``mark_synchronized(label)`` model callbacks. ``participant_main``
invokes them only after the corresponding real gorti RPC/event step
succeeds, so the model state is evidence of the wire-level rendezvous.
"""

from __future__ import annotations

from typing import Any


class Participant:
    """One federate in the sync-point demo.

    Attributes
    ----------
    achieved : list[str]
        Labels this federate voted achieved on, in call order.
    synchronized : list[str]
        Labels this federate observed as fully synchronized
        (every required peer voted achieved).
    sent_ticks : list[int]
        Per-tick seq numbers emitted via output_handler during
        the running phase. Drives the wire-log verification.
    name : str
        Federate name (also the federate-name in HLA registration).
    """

    def __init__(self, *, name: str) -> None:
        self.name = name
        self.achieved: list[str] = []
        self.synchronized: list[str] = []
        self.sent_ticks: list[int] = []
        self.received_ticks: list[int] = []
        self._tick = 0
        # Whether this federate is currently in its "running" phase —
        # the runner toggles this between rendezvous points so
        # output_handler() emits ticks only between start_simulation
        # and end_simulation. Outside of running we return {} so the
        # bridge skips send_interaction.
        self.running = False

    # --- Sync-point lifecycle callbacks ------------------------------------

    def achieve(self, label: str) -> None:
        """Vote 'achieved' on a sync-point label.

        Called after ``synchronization_point_achieved(label)``
        succeeds against rtid.
        """
        self.achieved.append(label)

    def mark_synchronized(self, label: str) -> None:
        """Notify the federate that the federation is now synchronized
        on ``label`` (every required peer voted achieved). Called
        after ``FederationSynchronized`` arrives on the event stream.
        """
        self.synchronized.append(label)

    # --- pyjevsim CoupledModelProtocol surface (used during running phase) -

    def time_advance(self) -> float:
        return 1.0

    def output_handler(self) -> dict[str, Any]:
        if not self.running:
            return {}
        self._tick += 1
        seq = self._tick
        self.sent_ticks.append(seq)
        return {"out_tick": seq.to_bytes(4, byteorder="big", signed=False)}

    def internal_transition(self) -> None:
        # output_handler bumped _tick; nothing else to do.
        pass

    def external_transition(self, port: str, payload: Any) -> None:
        if port != "in_tick":
            return
        if isinstance(payload, int):
            self.received_ticks.append(payload)
            return
        if isinstance(payload, dict) and "_payload" in payload:
            raw = payload["_payload"]
        elif isinstance(payload, dict) and len(payload) == 1:
            raw = next(iter(payload.values()))
        elif isinstance(payload, (bytes, bytearray)):
            raw = payload
        else:
            return
        if isinstance(raw, (bytes, bytearray)) and len(raw) == 4:
            self.received_ticks.append(int.from_bytes(raw, "big"))
