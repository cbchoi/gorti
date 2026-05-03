"""HLAFederate — DEVS time-advance bridge using NER. Agent C implements
per TASK-070.

Per docs/agent-c-pysdk.md §4.4:

  - Bridge calls coupled_model.time_advance() → DEVS ``ta``.
  - Issues nextMessageRequest(now + ta) on the federate.
  - On TimeAdvanceGrant(t):
      - if t == now + ta (no external event arrived earlier) →
            run output_handler → drain ports → send_interaction
            → internal_transition.
      - if t < now + ta (external event arrived first) →
            external_transition(port, payload). No internal cycle.
  - Loop.

This file is FROZEN-shape — public method names and run() signature are
the contract.
"""

from __future__ import annotations

from typing import Any

from pyjevsim_bridge._protocol import CoupledModelProtocol
from pyjevsim_bridge.port_mapping import PortMapping
from rti1516e.connection import FederationSpec, RtiConnection


class HLAFederate:
    """Wrap a pyjevsim CoupledModel as an HLA federate.

    Construct with a connection (either an open RtiConnection or a URL
    that the bridge will connect to itself), the coupled model, the
    federation spec, the federate name, and the port mapping.
    """

    def __init__(
        self,
        coupled_model: CoupledModelProtocol,
        federation: FederationSpec,
        federate_name: str,
        port_mapping: PortMapping,
        *,
        connection: RtiConnection | None = None,
        url: str | None = None,
    ) -> None:
        self.coupled_model = coupled_model
        self.federation = federation
        self.federate_name = federate_name
        self.port_mapping = port_mapping
        self._connection = connection
        self._url = url

    async def run(self, *, ticks: int) -> None:
        """Run the bridge loop for ``ticks`` time-advance cycles.

        Each tick:
        1. Read ta from coupled_model.
        2. NER for now + ta.
        3. Drain output_handler if internal cycle granted, otherwise
           apply external_transition.
        4. Increment cycle count; stop after ``ticks`` total.

        Raises NotImplementedError until TASK-070.
        """
        raise NotImplementedError("TASK-070")

    async def step_once(self) -> None:
        """Run one bridge cycle. Returns when the cycle's TimeAdvanceGrant
        is fully processed. Used by tests to drive the bridge in
        single-step mode without the run() loop's stop condition.

        Raises NotImplementedError until TASK-070.
        """
        raise NotImplementedError("TASK-070")

    async def deliver_external(self, fom_class_name: str, payload: Any) -> None:
        """Test/integration hook: simulate an externally-arrived
        interaction (in production this comes from the events() stream).
        Agent C may keep this internal if the design doesn't need it
        exposed; spec tests use the public API only.
        """
        raise NotImplementedError("TASK-070")
