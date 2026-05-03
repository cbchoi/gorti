"""Adapters mapping real pyjevsim models to ``CoupledModelProtocol``.

Two adapter shapes ship in this module:

  - :class:`RealPyjevsimAdapter` — wraps a single atomic
    ``BehaviorModel``. Cut-1 (M5 W1C) shipped this only; suitable for
    federations whose model is one atomic per federate.
  - :class:`RealPyjevsimStructuralAdapter` — wraps a
    ``StructuralModel`` (a hierarchy of atomic + nested coupled
    models). Drives the underlying ``SysExecutor`` to schedule and
    fire the right atomics at the right times, surfacing a flat
    DEVS-canonical surface (``time_advance / output_handler /
    internal_transition / external_transition``) that the bridge can
    consume identically to the single-atomic case.

Both adapters translate pyjevsim 1.3.x's API to the bridge's
DEVS-canonical names::

      bridge (CoupledModelProtocol)        pyjevsim 1.3.x
      ----------------------------         ---------------
      time_advance() -> float              per-state ``deadline``
                                           (single-atomic) OR
                                           ``SysExecutor.get_next_event_time
                                           - get_global_time`` (structural)
      output_handler() -> dict             ``output(msg_deliver)`` (single)
                                           OR drained
                                           ``SysExecutor.output_event_queue``
                                           after one ``simulate(dt)`` call
                                           (structural)
      internal_transition() -> None        ``int_trans()`` (single) OR
                                           the ``simulate(dt)`` call
                                           already advanced state
                                           (structural — no-op here)
      external_transition(port, payload)   ``ext_trans`` (single) OR
                                           ``insert_external_event(port,
                                           msg)`` then drive the executor
                                           (structural)

Why an adapter instead of using pyjevsim directly: ``HLAFederate``
(``pysdk/pyjevsim_bridge/time_advance.py``) speaks the DEVS-canonical
surface so bridge logic stays substrate-agnostic. The adapters are the
substrate-specific glue.

Structural adapter — cut-1 scope notes
--------------------------------------

The structural adapter targets the typical pyjevsim composition: a
root ``StructuralModel`` containing N atomic ``BehaviorModel`` leaves
with intra-model couplings + a small set of "boundary" ports the
adapter surfaces to the bridge. Nested ``StructuralModel`` children
(2- and 3-level hierarchies) work — the adapter walks ``get_models``
recursively to find leaves; couplings declared at any level are
honoured through pyjevsim's existing routing.

What's NOT in cut-1:

  - ``insert_external_transition`` (per-state transition table changes)
    — the adapter sticks with ``ext_trans`` semantics directly.
  - Multiple messages-per-port-per-cycle aggregation — outputs collapse
    to one payload per port per cycle (matches the bridge's send loop).
  - Real-time execution mode — uses ``ExecutionType.V_TIME`` only.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:  # pragma: no cover - import-only for type checking
    from pyjevsim.behavior_model import BehaviorModel
    from pyjevsim.structural_model import StructuralModel


class _CapturingDeliverer:
    """Capture pyjevsim ``output(msg_deliver)`` calls into a {port: [msg]} dict.

    pyjevsim's ``BehaviorModel.output(msg_deliver)`` writes to the
    deliverer via duck-typed methods; we capture those calls so the
    bridge can pull them as a {port: payload} dict on the next
    ``output_handler()`` invocation.

    The exact API of pyjevsim's MessageDeliverer evolves between
    versions; we sniff for the common ``insert_message(port, msg)``
    + ``insert(port, msg)`` shapes and fall back to attribute-style
    storage so the adapter survives small upstream churn.
    """

    def __init__(self) -> None:
        self.collected: dict[str, list[Any]] = {}

    def insert_message(self, port: str, msg: Any) -> None:
        """Most common pyjevsim 1.3.x deliverer entry point."""
        self.collected.setdefault(port, []).append(msg)

    # Aliases for shape-variations seen in upstream pyjevsim revisions.
    def insert(self, port: str, msg: Any) -> None:  # pragma: no cover
        self.insert_message(port, msg)

    def append(self, port: str, msg: Any) -> None:  # pragma: no cover
        self.insert_message(port, msg)


class RealPyjevsimAdapter:
    """Adapt a single pyjevsim ``BehaviorModel`` to ``CoupledModelProtocol``.

    Construct with the model and the deadline (in logical seconds) that
    the bridge should use as ``time_advance``. The deadline is supplied
    explicitly because pyjevsim's per-state ``deadline`` lives inside
    the model's state-machine setup and would require introspecting
    pyjevsim internals to extract — clean to pass it in instead.

    Usage::

        from pyjevsim.behavior_model import BehaviorModel
        # ...build model with insert_state(name, deadline=1.0) etc.
        adapter = RealPyjevsimAdapter(model, ta_seconds=1.0)
        # adapter satisfies CoupledModelProtocol; pass to HLAFederate.
    """

    def __init__(
        self,
        model: "BehaviorModel",
        *,
        ta_seconds: float = 1.0,
        out_ports: tuple[str, ...] = (),
    ) -> None:
        self.model = model
        self._ta = float(ta_seconds)
        self._out_ports = tuple(out_ports)

    # --- CoupledModelProtocol implementation --------------------------------

    def time_advance(self) -> float:
        """Return the configured ``ta`` (logical seconds until next firing).

        pyjevsim's per-state deadlines drive its standalone scheduler;
        the bridge needs a single number per cycle, so the adapter
        surfaces the constant the caller supplied at construction time.
        """
        return self._ta

    def output_handler(self) -> dict[str, Any]:
        """Invoke pyjevsim's ``output(msg_deliver)`` and return the captured dict.

        The deliverer is a fresh capturing instance per call so a model
        that emits on multiple cycles doesn't accumulate stale outputs.
        For ports with multiple messages in a cycle, the bridge will
        send one interaction per message; cut-1 collapses to the first
        message per port (matches the cut-1 producer semantics).
        """
        deliverer = _CapturingDeliverer()
        try:
            self.model.output(deliverer)
        except TypeError:
            # Some pyjevsim revisions take no argument; tolerate that
            # by calling with no args and treating the return value (if
            # a dict) as the output map.
            result = self.model.output()  # type: ignore[call-arg]
            if isinstance(result, dict):
                return dict(result)
            return {}
        # Collapse {port: [msg, ...]} -> {port: msg_first}
        return {port: msgs[0] for port, msgs in deliverer.collected.items() if msgs}

    def internal_transition(self) -> None:
        """Delegate to pyjevsim's ``int_trans``."""
        self.model.int_trans()

    def external_transition(self, port: str, payload: Any) -> None:
        """Delegate to pyjevsim's ``ext_trans(port, msg)``."""
        self.model.ext_trans(port, payload)


# --- Structural-hierarchy adapter (M6 W2 Part B) ---------------------------


def _make_boundary_sink_class() -> Any:
    """Build the ``_BoundarySinkLeaf`` class lazily (pyjevsim must be
    importable at this point — we deferred the dep).

    Lazy class construction lets the structural adapter bring
    ``BehaviorModel`` into scope only when it is actually instantiated;
    callers that only use the single-atomic ``RealPyjevsimAdapter``
    (which doesn't need the sink) avoid the import cost.
    """
    from pyjevsim.behavior_model import BehaviorModel as _BM  # noqa: PLC0415

    class _BoundarySinkLeaf(_BM):  # type: ignore[misc, valid-type]
        """A pyjevsim atomic that records every ``ext_trans`` into a
        shared dict.

        Used by ``RealPyjevsimStructuralAdapter`` as the destination of
        coupling_relations from leaf models' output ports onto the
        boundary output ports. The shared ``cycle_outputs`` dict is
        owned by the adapter; the sink writes ``{port: payload_latest}``
        on each delivery so the adapter's ``output_handler`` can
        return + clear it.

        The sink has a single state with an ``Infinite`` deadline so
        it never fires autonomously — its only purpose is to receive
        externally routed messages.
        """

        def __init__(
            self,
            *,
            cycle_outputs: dict[str, Any],
            ports: tuple[str, ...],
        ) -> None:
            super().__init__("_hla_boundary_sink")
            self._cycle_outputs = cycle_outputs
            # Deadline 'inf' so the sink never fires int_trans on its
            # own. pyjevsim accepts the string 'inf' and the float
            # equivalent equally.
            self.insert_state("idle", deadline=float("inf"))
            for p in ports:
                self.insert_input_port(p)
            # pyjevsim requires at least one output port for
            # well-formedness; declare a synthetic one nothing routes
            # to.
            self.insert_output_port("_unused")
            self.init_state("idle")

        def output(self, msg_deliver: Any) -> None:  # noqa: D401
            return  # never fires

        def int_trans(self) -> None:
            return  # never fires

        def ext_trans(self, port: str, msg: Any) -> None:
            # ``msg`` is a ``SysMessage``; ``retrieve()`` returns the
            # list of payloads packed into it. Take the latest entry
            # so multi-message cycles collapse to one payload per
            # port (matches the bridge's send_interaction semantics).
            payloads = msg.retrieve()
            if payloads:
                self._cycle_outputs[port] = payloads[-1]

    return _BoundarySinkLeaf


# Lazily-resolved at first instantiation; cached to avoid re-importing.
_BOUNDARY_SINK_LEAF_CLS: Any = None


def _BoundarySinkLeaf(  # noqa: N802 — factory exposed as class-shape for typing
    *, cycle_outputs: dict[str, Any], ports: tuple[str, ...]
) -> Any:
    """Construct a sink leaf instance. Wraps the lazy class build so the
    structural adapter can ``self._sink = _BoundarySinkLeaf(...)`` without
    referencing pyjevsim at adapter-import time."""
    global _BOUNDARY_SINK_LEAF_CLS  # noqa: PLW0603 — module-level cache
    if _BOUNDARY_SINK_LEAF_CLS is None:
        _BOUNDARY_SINK_LEAF_CLS = _make_boundary_sink_class()
    return _BOUNDARY_SINK_LEAF_CLS(cycle_outputs=cycle_outputs, ports=ports)



class RealPyjevsimStructuralAdapter:
    """Adapt a ``pyjevsim.StructuralModel`` (coupled hierarchy) to
    ``CoupledModelProtocol``.

    The structural adapter wraps a coupled hierarchy by constructing a
    ``SysExecutor`` internally, registering every atomic leaf walked
    out of the hierarchy, replaying intra-model couplings, and
    surfacing a flat DEVS-canonical interface to the bridge.

    Logical model
    -------------

    The bridge's loop expects a four-method contract:

      - ``time_advance() -> dt`` — how long until the next firing?
      - ``output_handler() -> {port: payload}`` — drain the outputs
        produced at the about-to-fire instant.
      - ``internal_transition()`` — advance simulation past that
        instant.
      - ``external_transition(port, payload)`` — inject an external
        event arriving at the boundary.

    The structural adapter implements these by:

      - Computing ``dt`` from ``SysExecutor.get_next_event_time() -
        get_global_time()``. Returns a configurable
        ``default_ta`` (default 1.0 s) when there's no scheduled
        event yet (initial state) so the bridge always sees a finite
        step.
      - For ``output_handler``: advances the executor by ``dt``,
        which fires every leaf whose deadline has elapsed. The
        adapter installs an ``output_event_callback`` that pushes
        boundary outputs into a per-cycle buffer keyed by the port
        the leaf emitted on. ``output_handler`` returns + clears the
        buffer.
      - ``internal_transition`` is a no-op because the
        ``simulate(dt)`` call already executed the int_trans of every
        fired atomic. The bridge calls it for symmetry; the adapter
        honors the contract by returning silently.
      - ``external_transition(port, payload)`` does
        ``insert_external_event(port, payload)`` against the executor
        + a single ``simulate(0)`` to drain the input queue into the
        atomic via the regular pyjevsim routing.

    Boundary ports
    --------------

    The caller declares the ports the adapter surfaces to the bridge
    via ``input_ports`` (events from HLA → into the federation) and
    ``output_ports`` (events from the federation → out to HLA). These
    are registered against the internal SysExecutor as the
    "external" port set; intra-hierarchy couplings stay private to
    pyjevsim.

    For each ``output_port``, the caller specifies the
    ``(source_model_name, source_port_name)`` pair that should
    publish onto it via ``output_couplings``. The adapter wires
    ``coupling_relation(source, source_port, executor, output_port)``
    so pyjevsim routes the leaf's output into the executor's outbound
    queue, where the adapter's callback collects it.

    For each ``input_port``, the caller specifies the
    ``(dest_model_name, dest_port_name)`` pair that should consume
    HLA-arriving events via ``input_couplings``. The adapter wires
    ``coupling_relation(executor, input_port, dest, dest_port)`` so
    pyjevsim routes the inbound queue into the leaf.

    Example::

        from pyjevsim.structural_model import StructuralModel
        from pyjevsim.system_executor import SysExecutor

        root = StructuralModel("root")
        root.register_entity(prod := Producer("prod"))
        root.register_entity(cons := Consumer("cons"))
        root.coupling_relation(prod, "out", cons, "inp")

        adapter = RealPyjevsimStructuralAdapter(
            root,
            time_resolution=0.5,
            input_ports={"in_cmd": ("cons", "inp_cmd")},
            output_ports={"out_seq": ("prod", "out_seq")},
            default_ta=1.0,
        )
        # adapter satisfies CoupledModelProtocol; pass to HLAFederate.

    Cut-1 scope (per the M6 dispatch plan):

      - 2-3-level hierarchies are exercised via ``get_models`` walked
        recursively. Deeper hierarchies are accepted but not
        stress-tested; pyjevsim's own routing handles the depth as
        long as every atomic was registered with the executor.
      - ``ExecutionType.V_TIME`` only.
    """

    def __init__(
        self,
        coupled_model: "StructuralModel",
        *,
        time_resolution: float = 1.0,
        input_ports: dict[str, tuple[str, str]] | None = None,
        output_ports: dict[str, tuple[str, str]] | None = None,
        default_ta: float = 1.0,
    ) -> None:
        # Lazy imports keep pyjevsim out of import-time for callers
        # that only construct the single-atomic adapter (the M5 path).
        from pyjevsim.system_executor import (  # noqa: PLC0415
            ExecutionType,
            SysExecutor,
        )

        self.coupled_model = coupled_model
        self._default_ta = float(default_ta)

        self._executor = SysExecutor(
            time_resolution, ex_mode=ExecutionType.V_TIME
        )
        # Walk the hierarchy, register every leaf, then replay
        # intra-hierarchy couplings against the executor so pyjevsim's
        # internal routing fires.
        self._leaves: dict[str, BehaviorModel] = {}
        self._collect_leaves(coupled_model)
        for leaf in self._leaves.values():
            self._executor.register_entity(leaf)
        self._replay_couplings(coupled_model)

        # Boundary port wiring. Two design choices worth flagging:
        #
        # 1. Output ports route to a private "sink" leaf model, NOT to
        #    the executor's external output queue. The latter would
        #    seem cleaner but pyjevsim 1.3.x's
        #    ``SysExecutor.single_output_handling`` has a bug
        #    (``msg[1].retrieve()`` on a non-subscriptable
        #    ``SysMessage``) when the destination is the executor
        #    itself; routing through a sink atomic sidesteps the bug
        #    while preserving the same observable behaviour.
        #
        # 2. Input ports use the standard
        #    ``insert_input_port`` + ``coupling_relation(None, port,
        #    dest, dest_port)`` path (this codepath is sound).
        #
        # The sink model is created on the fly with one input port per
        # boundary output; its ``ext_trans`` records the (port, payload)
        # pair into a per-cycle buffer that ``output_handler`` drains.
        self._input_ports = dict(input_ports or {})
        self._output_ports = dict(output_ports or {})
        for port_name, (dest_name, dest_port) in self._input_ports.items():
            self._executor.insert_input_port(port_name)
            dest = self._leaves[dest_name]
            self._executor.coupling_relation(
                None, port_name, dest, dest_port
            )

        # Per-cycle output buffer populated by the sink leaf when it
        # receives a routed output. {port: latest payload} so multiple
        # outputs on the same boundary port collapse to one — matching
        # the bridge's send semantics (one interaction per port per
        # cycle).
        self._cycle_outputs: dict[str, Any] = {}
        if self._output_ports:
            self._sink: _BoundarySinkLeaf | None = _BoundarySinkLeaf(
                cycle_outputs=self._cycle_outputs,
                ports=tuple(self._output_ports.keys()),
            )
            self._executor.register_entity(self._sink)
            for port_name, (src_name, src_port) in self._output_ports.items():
                src = self._leaves[src_name]
                # Route the source leaf's output port to the sink's
                # boundary-named input port. The sink's ``ext_trans``
                # writes into ``self._cycle_outputs`` keyed by port.
                self._executor.coupling_relation(
                    src, src_port, self._sink, port_name
                )
        else:
            self._sink = None

        # Pyjevsim's scheduler doesn't activate freshly-registered
        # entities until the FIRST step()/schedule() — and on that
        # call, ``create_entity`` sets ``req_time = global_time + ta``,
        # so a step(grant) for grant=ta would NOT fire the entity
        # (req_time would equal grant + ta, > grant). Workaround:
        # call ``step(0)`` once at construction time to move every
        # waiting entity into the active map and seed its req_time.
        # After this, ``step(grant)`` correctly fires entities whose
        # req_time <= grant.
        self._executor.step(0)

    # --- Hierarchy walk + coupling replay -----------------------------------

    def _collect_leaves(self, node: Any) -> None:
        """Recursively collect every atomic ``BehaviorModel`` under
        ``node`` into ``self._leaves`` keyed by ``get_name()``.

        Tolerates 2- and 3-level structural hierarchies (and deeper —
        recursion has no depth cap). A leaf is anything that isn't a
        ``StructuralModel``; that detection is duck-typed via
        ``get_models`` to avoid an import-time pyjevsim dep.
        """
        # Duck-type structural detection: a coupled node has
        # ``get_models()`` returning a {name: child} dict.
        get_models = getattr(node, "get_models", None)
        if callable(get_models):
            for child in get_models().values():
                self._collect_leaves(child)
            return
        name = node.get_name()
        # Last-writer-wins on duplicate names; in practice the hierarchy
        # should give distinct names. We don't dedupe across multiple
        # registrations of the same physical instance because pyjevsim
        # already rejects that at register_entity time.
        self._leaves[name] = node

    def _replay_couplings(self, node: Any) -> None:
        """Replay intra-hierarchy ``coupling_relation`` calls against
        the SysExecutor so pyjevsim's native routing fires across
        leaves.

        ``StructuralModel.get_couplings()`` returns a dict keyed by
        ``(src_obj, src_port)`` with values of ``[(dst_obj, dst_port), ...]``.
        We only register couplings between objects that survived the
        leaf walk — couplings whose endpoints are intermediate
        coupled nodes are silently skipped (cut-1 doesn't surface
        coupled-to-coupled aliases; deeper rewiring is post-MVP).
        """
        get_couplings = getattr(node, "get_couplings", None)
        if callable(get_couplings):
            for (src_obj, src_port), dst_pairs in get_couplings().items():
                for dst_obj, dst_port in dst_pairs:
                    src_in = src_obj in self._leaves.values()
                    dst_in = dst_obj in self._leaves.values()
                    if src_in and dst_in:
                        self._executor.coupling_relation(
                            src_obj, src_port, dst_obj, dst_port
                        )
        # Recurse into nested coupled children so their couplings
        # also lift to the executor.
        get_models = getattr(node, "get_models", None)
        if callable(get_models):
            for child in get_models().values():
                self._replay_couplings(child)

    # --- CoupledModelProtocol implementation --------------------------------

    def time_advance(self) -> float:
        """Return seconds until the next leaf firing.

        Computed as ``next_event_time - global_time`` from the
        executor; falls back to ``default_ta`` when the executor
        reports no upcoming event (e.g. immediately after construction
        before any simulate call). Negative or zero results are
        clamped to ``default_ta`` so the bridge always advances by a
        finite positive step.
        """
        try:
            next_t = float(self._executor.get_next_event_time())
            now = float(self._executor.get_global_time())
        except Exception:  # noqa: BLE001 — defensive across pyjevsim revisions
            return self._default_ta
        if next_t == float("inf") or next_t <= now:
            return self._default_ta
        dt = next_t - now
        return dt if dt > 0 else self._default_ta

    def output_handler(self) -> dict[str, Any]:
        """Advance the executor by one ``ta`` step + drain boundary outputs.

        Computes ``dt = time_advance()``, calls ``self._executor.step(
        global_time + dt)`` so pyjevsim fires every leaf whose req_time
        falls inside the new horizon. The sink leaf's ``ext_trans``
        captures boundary outputs into ``self._cycle_outputs`` as a
        side effect of pyjevsim's regular routing; this method returns
        the buffered dict and clears it for the next cycle.
        """
        dt = self.time_advance()
        target_time = float(self._executor.get_global_time()) + dt
        # ``step(grant)`` fires every entity with req_time <= grant
        # at the new global_time. Returns the deque of external
        # output events captured via the executor's
        # output_event_queue — we ignore that deque and rely on the
        # sink-leaf path (see __init__ for the rationale on avoiding
        # pyjevsim's external-output queue bug).
        self._executor.step(target_time)
        result = dict(self._cycle_outputs)
        self._cycle_outputs.clear()
        return result

    def internal_transition(self) -> None:
        """No-op: ``simulate(dt)`` inside ``output_handler`` already
        ran ``int_trans`` for every fired leaf. The bridge calls this
        method for protocol symmetry; honoring the contract here is
        the right behaviour because the executor already advanced
        state."""
        return

    def external_transition(self, port: str, payload: Any) -> None:
        """Inject an HLA-arriving payload into the federation via the
        boundary input port.

        Validates the port is declared so a typo surfaces as a clear
        ``KeyError`` rather than as silent dropped traffic. Pushes
        the event onto the executor's input queue + drives a
        ``simulate(0)`` so the executor's
        ``handle_external_input_event`` routes the message to the
        coupled leaf via ``ext_trans``.
        """
        if port not in self._input_ports:
            raise KeyError(
                f"RealPyjevsimStructuralAdapter: no input port {port!r} "
                f"(declared: {sorted(self._input_ports.keys())})"
            )
        self._executor.insert_external_event(port, payload, scheduled_time=0)
        # Drive the executor at the current global_time so
        # ``handle_external_input_event`` routes the queued external
        # into the coupled leaf via the recorded coupling. ``step``
        # at the current time advances no internals (no req_time
        # equals it for fresh entities) but does flush the input
        # queue.
        now = float(self._executor.get_global_time())
        self._executor.step(now)

    # --- Inspection helpers (test convenience) ------------------------------

    @property
    def executor(self) -> Any:
        """Expose the internal SysExecutor for tests that need to inspect
        ``get_global_time`` or peek at registered leaves. Not part of the
        bridge contract; using it from production code is a smell."""
        return self._executor

    @property
    def leaves(self) -> dict[str, "BehaviorModel"]:
        """Read-only view of name -> atomic leaf. Useful for tests that
        want to assert on per-leaf state after a cycle."""
        return dict(self._leaves)
