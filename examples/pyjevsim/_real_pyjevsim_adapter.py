"""Adapter mapping a real pyjevsim ``BehaviorModel`` to ``CoupledModelProtocol``.

Cut-1 scope (per docs/M5_DISPATCH_PLAN.md §3 W1C):
  - Wraps a single atomic ``BehaviorModel`` (NOT a structural hierarchy).
    Nested ``StructuralModel`` adaptation is post-MVP — the cross-language
    smoke does not need it.
  - Translates the bridge's DEVS-canonical method names to pyjevsim 1.3.x's
    method names:

      bridge (CoupledModelProtocol)        pyjevsim 1.3.x
      ----------------------------         ---------------
      time_advance() -> float              ``deadline'' of the current state
                                           (insert_state's ``deadline`` arg)
      output_handler() -> dict             ``output(msg_deliver)`` -> read back
                                           the queued outputs
      internal_transition() -> None        ``int_trans()``
      external_transition(port, payload)   ``ext_trans(port, msg)``

Why an adapter at all instead of using pyjevsim directly: the bridge
(``HLAFederate`` in pysdk/pyjevsim_bridge/time_advance.py) speaks the
DEVS-canonical names so the bridge logic stays substrate-agnostic. Without
this adapter the bridge would have to special-case pyjevsim's API.

NB — Cut-1 simplification: the adapter exposes a flat
``time_advance / output_handler / internal_transition / external_transition``
surface that the bridge's loop can drive directly. It does NOT spin up a
``SysExecutor`` — that machinery exists for pyjevsim's standalone
simulation loop and would conflict with the bridge's own loop. A future
cut may run the SysExecutor in lockstep, but for the cross-language
smoke a single-atomic-model wrapper is sufficient.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:  # pragma: no cover - import-only for type checking
    from pyjevsim.behavior_model import BehaviorModel


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
