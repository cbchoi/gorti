"""Participant federate — joins the federation, achieves named sync
points on demand, and emits a Tick heartbeat during the running phase.

The federate model is deliberately tiny: the pedagogical focus of
this example is the rendezvous semantics (the runner-driven sync
gating), not the per-cycle compute.

Sync-point lifecycle modelled here:

   pending  ──register──▶  registered
   registered ──achieve──▶ achieved
   achieved (all peers achieved) ──synchronized──▶ released

The federate exposes ``achieve(label)`` that the runner calls when
it wants this federate to vote "achieved" on a label. The federate
records the call so the runner can verify every federate achieved
every label exactly once. The federate also exposes
``mark_synchronized(label)`` which the runner calls AFTER all
peers have voted achieved — this is the post-rendezvous callback
the proto layer can't yet wire (M12 deferral #1).
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
        (every required peer voted achieved). The runner is the
        oracle for "fully synchronized" because the in-process
        transport doesn't auto-route the federationSynchronized
        callback (see README).
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
        self._tick = 0
        # Whether this federate is currently in its "running" phase —
        # the runner toggles this between rendezvous points so
        # output_handler() emits ticks only between start_simulation
        # and end_simulation. Outside of running we return {} so the
        # bridge skips send_interaction.
        self.running = False

    # --- Sync-point voting (runner-driven) ---------------------------------

    def achieve(self, label: str) -> None:
        """Vote 'achieved' on a sync-point label.

        In a real federation this would be ``fed.sync.synchronization_point_achieved(label)``;
        the runner is the gate-keeper here because the proto layer
        does not yet expose the federationSynchronized callback.
        """
        self.achieved.append(label)

    def mark_synchronized(self, label: str) -> None:
        """Notify the federate that the federation is now synchronized
        on ``label`` (every required peer voted achieved). In a wired
        cut-4 world this would arrive as a ``FederationSynchronized``
        event on ``fed.events()``; here the runner sends it directly.
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
        # We don't need to consume peers' Ticks for this example.
        # Accept-and-drop keeps the bridge's general delivery path
        # uniform. If someone subscribes the model to Tick they can
        # observe the peers' arrivals here.
        del port, payload
