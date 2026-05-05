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

from collections import deque
from typing import Any

from pyjevsim_bridge._protocol import CoupledModelProtocol
from pyjevsim_bridge.port_mapping import PortMapping
from rti1516e.connection import Federate, FederationSpec, RtiConnection
from rti1516e.events import ReceiveInteraction, TimeAdvanceGrant


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
        # Logical federate clock — DEVS ``now``. Advances on every
        # successful TimeAdvanceGrant.
        self._now: float = 0.0
        # Queue of externally-arrived interactions awaiting delivery to
        # the coupled model on the next step. Each entry is the FOM class
        # name + the raw payload; ``step_once`` translates the class to
        # an input port via ``port_mapping.in_port_for_fom_class``.
        self._pending_external: deque[tuple[str, Any]] = deque()
        # Lazy-initialized SDK objects when step_once is called outside
        # of run(). Held across calls so successive step_once invocations
        # reuse the same federate.
        self._owned_connection: RtiConnection | None = None
        self._owned_federate_cm: Any | None = None
        self._federate: Federate | None = None

    async def run(self, *, ticks: int) -> None:
        """Run the bridge loop for ``ticks`` time-advance cycles.

        Opens the connection (if a URL was supplied) and joins the
        federation, then drives ``step_once`` ``ticks`` times. Resigns
        and closes deterministically on exit, even if a step raises.
        """
        if self._connection is not None:
            # Caller-managed connection: just join + step + resign.
            async with self._connection.join_federation(
                self.federation, federate_name=self.federate_name
            ) as fed:
                self._federate = fed
                try:
                    for _ in range(ticks):
                        await self.step_once()
                finally:
                    self._federate = None
            return

        if self._url is None:
            raise RuntimeError(
                "HLAFederate.run: neither connection nor url was supplied"
            )

        async with (
            RtiConnection.connect(self._url) as rti,
            rti.join_federation(
                self.federation, federate_name=self.federate_name
            ) as fed,
        ):
            self._federate = fed
            try:
                for _ in range(ticks):
                    await self.step_once()
            finally:
                self._federate = None

    async def step_once(self) -> None:
        """Run one bridge cycle. Returns when the cycle's TimeAdvanceGrant
        is fully processed. Used by tests to drive the bridge in
        single-step mode without the run() loop's stop condition.

        Cycle semantics
        ---------------
        - If external events are pending (queued by ``deliver_external``
          or by the previous cycle's grant), drain them via
          ``external_transition`` and return WITHOUT an internal cycle —
          that matches docs/agent-c-pysdk.md §4.4 "external arrived
          earlier than ta → no internal cycle this round".
        - Otherwise: read ``ta``, issue NER for ``_now + ta``, drain
          events until the matching TimeAdvanceGrant. Externals arriving
          in flight are buffered; if the grant time is strictly less
          than the requested time we treat the cycle as external and
          skip the internal phase. Otherwise we run output_handler →
          send_interaction → internal_transition.
        """
        fed = await self._ensure_federate()

        # External-first: if anything is queued before this cycle, drain
        # it without asking the RTI for a grant. Production traffic for
        # an external event would arrive via fed.events() as a
        # ReceiveInteraction; deliver_external is the test seam for
        # injecting that without a real RTI roundtrip.
        if self._pending_external:
            self._drain_pending_external()
            return

        ta = self.coupled_model.time_advance()
        t_request = self._now + ta
        await fed.next_message_request(t_request)

        grant = await self._await_grant(fed, t_request)

        # If we collected externals while waiting for the grant, deliver
        # them and skip the internal cycle (per §4.4 "external arrived
        # earlier"). Otherwise this is a clean internal cycle.
        if self._pending_external or grant.time < t_request:
            self._drain_pending_external()
        else:
            await self._run_internal_cycle(fed)

        self._now = grant.time

    async def deliver_external(self, fom_class_name: str, payload: Any) -> None:
        """Test/integration hook: simulate an externally-arrived
        interaction.

        Production traffic delivers this via ``fed.events()`` as a
        ``ReceiveInteraction``; ``step_once`` collects those into the
        same internal queue this method writes to. The two paths
        converge at ``_drain_pending_external``.
        """
        self._pending_external.append((fom_class_name, payload))

    # --- internal helpers ---------------------------------------------------

    async def _ensure_federate(self) -> Federate:
        """Return an open Federate, lazily joining via the configured
        connection or URL on first call.

        Lazy init lets spec tests call ``step_once`` directly without
        going through ``run()``; the resulting connection + federate
        are owned by the bridge instance and torn down by ``aclose``.
        """
        if self._federate is not None:
            return self._federate

        if self._connection is not None:
            cm = self._connection.join_federation(
                self.federation, federate_name=self.federate_name
            )
        else:
            if self._url is None:
                raise RuntimeError(
                    "HLAFederate: no connection and no url configured"
                )
            self._owned_connection = await RtiConnection.connect(
                self._url
            ).__aenter__()
            cm = self._owned_connection.join_federation(
                self.federation, federate_name=self.federate_name
            )

        self._owned_federate_cm = cm
        federate = await cm.__aenter__()
        self._federate = federate
        return federate

    async def _await_grant(
        self, fed: Federate, t_request: float
    ) -> TimeAdvanceGrant:
        """Drain ``fed.events()`` until a TimeAdvanceGrant arrives.

        ReceiveInteractions encountered along the way are translated
        to (port_name, payload) entries appended to the pending
        external queue, where ``step_once`` will hand them to
        ``external_transition`` in arrival order.

        ``t_request`` is unused by the cut-1 dispatch (the FakeRtiServer
        auto-grants at exactly t_request) but is part of the signature
        so a future implementation can detect "grant earlier than
        requested" without re-deriving it.
        """
        del t_request  # reserved for future scheduler-aware behavior
        async for event in fed.events():
            if isinstance(event, TimeAdvanceGrant):
                return event
            if isinstance(event, ReceiveInteraction):
                self._pending_external.append((event.class_name, event.parameters))
                continue
            # Other event kinds (DiscoverObjectInstance,
            # ReflectAttributeValues, FederationHalted) are not part of
            # the W6 contract; drop them on the floor for now. W7 will
            # decide how the bridge surfaces them to the coupled model.
        # The async-for can only exit via return inside the loop; the
        # explicit raise here is for type-checker coverage of the "loop
        # exited without yielding" branch.
        raise RuntimeError("HLAFederate: events() exhausted before grant")

    def _drain_pending_external(self) -> None:
        """Hand every queued external event to ``external_transition``.

        Events whose FOM class is not present in the port mapping are
        silently dropped — the bridge can subscribe to interactions the
        coupled model hasn't bound a port to. A future revision may log
        or surface this; cut-1 keeps it quiet.
        """
        while self._pending_external:
            fom_class, payload = self._pending_external.popleft()
            port = self.port_mapping.in_port_for_fom_class(fom_class)
            if port is None:
                continue
            self.coupled_model.external_transition(port, payload)

    async def _run_internal_cycle(self, fed: Federate) -> None:
        """Output → send_interaction → internal_transition.

        Outputs whose port is not bound to a FOM class in the mapping
        are skipped — same rationale as ``_drain_pending_external``.
        Payloads are passed through opaquely under a ``_payload`` key;
        full FOM-driven encoding is W7's territory.
        """
        outputs = self.coupled_model.output_handler()
        # Sort port names before iterating so the wire-visible
        # send_interaction order is deterministic regardless of how
        # the user model assembled its output dict. Mirrors Go's D-2
        # discipline (sort before iterate over maps). M5-audit issue #2.
        for port_name in sorted(outputs):
            payload = outputs[port_name]
            class_name = self.port_mapping.fom_class_for_out_port(port_name)
            if class_name is None:
                continue
            await fed.send_interaction(
                class_name,
                parameters={"_payload": payload},
            )
        self.coupled_model.internal_transition()

    async def aclose(self) -> None:
        """Tear down lazily-acquired connection/federate. Safe to call
        multiple times; no-op when nothing is owned.

        Tests that exercise step_once directly do not need to call this
        — the FakeRtiServer-backed transport is purely in-process and
        leaks no resources. The method is provided for symmetry with
        the run() lifecycle and for the W7 example runner.
        """
        cm = self._owned_federate_cm
        if cm is not None:
            await cm.__aexit__(None, None, None)
            self._owned_federate_cm = None
        self._federate = None
        owned = self._owned_connection
        if owned is not None:
            await owned.__aexit__(None, None, None)
            self._owned_connection = None
