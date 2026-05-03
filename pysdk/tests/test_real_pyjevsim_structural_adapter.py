"""Tests for ``RealPyjevsimStructuralAdapter`` (M6 W2 Part B).

Validates that the structural adapter wraps a coupled hierarchy of
real ``pyjevsim`` atomics, drives the underlying ``SysExecutor``, and
surfaces a flat DEVS-canonical surface to the bridge:

  - Output flow: a producer leaf inside the structural model fires,
    the executor routes its output to a coupled consumer leaf AND to
    the boundary output port. The adapter's ``output_handler`` returns
    the payload on the boundary port.
  - External-input flow: ``external_transition(port, payload)`` injects
    into the executor; the named leaf receives via ``ext_trans``.
  - Boundary port discipline: an unknown input port raises ``KeyError``
    instead of silently dropping the event.
  - Hierarchy walk: a 2-level hierarchy (root coupled → atomics) is
    flattened into the executor; the test asserts both leaves were
    registered.
"""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
EXAMPLES_DIR = REPO_ROOT / "examples" / "pyjevsim"
if str(EXAMPLES_DIR) not in sys.path:
    sys.path.insert(0, str(EXAMPLES_DIR))

pyjevsim = pytest.importorskip("pyjevsim", reason="pyjevsim optional runtime dep")

# Re-fetch via importorskip so mypy is happy and the symbols are lazy-loaded.
_BehaviorModel = pytest.importorskip(
    "pyjevsim.behavior_model"
).BehaviorModel
_StructuralModel = pytest.importorskip(
    "pyjevsim.structural_model"
).StructuralModel
_SysMessage = pytest.importorskip(
    "pyjevsim.system_message"
).SysMessage


class _Producer(_BehaviorModel):  # type: ignore[misc, valid-type]
    """Atomic that fires every ``deadline`` seconds + emits its tick on out."""

    def __init__(self, name: str, deadline: float = 1.0) -> None:
        super().__init__(name)
        self.tick = 0
        self.insert_state("active", deadline=deadline)
        self.insert_input_port("inp")
        self.insert_output_port("out")
        self.init_state("active")

    def output(self, msg_deliver) -> None:  # type: ignore[no-untyped-def]
        msg = _SysMessage(self.get_name(), "out")
        msg.insert(self.tick)
        msg_deliver.insert_message(msg)

    def int_trans(self) -> None:
        self.tick += 1

    def ext_trans(self, port: str, msg) -> None:  # type: ignore[no-untyped-def]
        # Producer doesn't consume input in this test, but the model
        # still needs a no-op handler for pyjevsim to route to.
        pass


class _Consumer(_BehaviorModel):  # type: ignore[misc, valid-type]
    """Atomic that records every ext_trans into ``received``."""

    def __init__(self, name: str) -> None:
        super().__init__(name)
        self.received: list[tuple[str, object]] = []
        # Long deadline so the consumer doesn't auto-fire during the
        # short test horizon — it should only fire on external events
        # routed to it from the producer or from the HLA boundary.
        self.insert_state("idle", deadline=1000.0)
        self.insert_input_port("inp")
        self.insert_output_port("ack")
        self.init_state("idle")

    def output(self, msg_deliver) -> None:  # type: ignore[no-untyped-def]
        # No autonomous output for the test.
        pass

    def int_trans(self) -> None:
        # No state advancement on the long cycle.
        pass

    def ext_trans(self, port: str, msg) -> None:  # type: ignore[no-untyped-def]
        for payload in msg.retrieve():
            self.received.append((port, payload))


def _build_hierarchy() -> tuple[object, _Producer, _Consumer]:
    """Construct a simple 2-level structural model with intra-coupling."""
    root = _StructuralModel("root")
    prod = _Producer("prod", deadline=1.0)
    cons = _Consumer("cons")
    root.register_entity(prod)
    root.register_entity(cons)
    # Intra-hierarchy coupling: producer's "out" goes to consumer's "inp".
    root.coupling_relation(prod, "out", cons, "inp")
    return root, prod, cons


def test_structural_adapter_collects_leaves() -> None:
    """Both atomics are registered with the internal SysExecutor."""
    from _real_pyjevsim_adapter import RealPyjevsimStructuralAdapter

    root, prod, cons = _build_hierarchy()
    adapter = RealPyjevsimStructuralAdapter(
        root,
        time_resolution=0.5,
        input_ports={"in_cmd": ("cons", "inp")},
        output_ports={"out_seq": ("prod", "out")},
    )
    leaves = adapter.leaves
    assert "prod" in leaves
    assert "cons" in leaves
    assert leaves["prod"] is prod
    assert leaves["cons"] is cons


def test_structural_adapter_output_flows_to_boundary() -> None:
    """A producer firing inside the hierarchy surfaces its output on the
    boundary output port via the adapter's ``output_handler``.

    Asserts the output dict contains the boundary port name with the
    producer's tick value (the producer emits ``tick`` then increments).
    """
    from _real_pyjevsim_adapter import RealPyjevsimStructuralAdapter

    root, prod, cons = _build_hierarchy()
    adapter = RealPyjevsimStructuralAdapter(
        root,
        time_resolution=0.5,
        input_ports={"in_cmd": ("cons", "inp")},
        output_ports={"out_seq": ("prod", "out")},
        default_ta=1.0,
    )

    # First cycle: bridge asks for ta, then output_handler. The
    # producer fires once because deadline=1.0 == ta. The adapter
    # buffers the boundary output and returns it as a {port: payload}
    # dict.
    out = adapter.output_handler()
    assert "out_seq" in out
    # The producer emitted its pre-int_trans tick value.
    assert out["out_seq"] == 0

    # internal_transition is a no-op (simulate already advanced). The
    # producer's tick advanced inside simulate.
    adapter.internal_transition()
    assert prod.tick >= 1

    # The intra-hierarchy coupling also delivered to the consumer:
    # the producer's "out" was routed to consumer's "inp" by pyjevsim.
    assert any(port == "inp" for port, _ in cons.received)


def test_structural_adapter_external_transition_reaches_leaf() -> None:
    """``external_transition`` injects through the boundary input port
    and the named destination leaf observes the payload via ``ext_trans``."""
    from _real_pyjevsim_adapter import RealPyjevsimStructuralAdapter

    root, _prod, cons = _build_hierarchy()
    adapter = RealPyjevsimStructuralAdapter(
        root,
        time_resolution=0.5,
        input_ports={"in_cmd": ("cons", "inp")},
        output_ports={"out_seq": ("prod", "out")},
    )
    # Inject an external event on the boundary input port. The adapter
    # should route it to the consumer leaf's "inp" port.
    adapter.external_transition("in_cmd", b"hello")
    # The consumer's ext_trans appends (port, payload) for each
    # message in the SysMessage's retrieve() list.
    received_payloads = [payload for port, payload in cons.received if port == "inp"]
    assert b"hello" in received_payloads


def test_structural_adapter_unknown_input_port_raises() -> None:
    """Sending to an undeclared boundary port raises ``KeyError`` rather
    than silently dropping the event."""
    from _real_pyjevsim_adapter import RealPyjevsimStructuralAdapter

    root, _prod, _cons = _build_hierarchy()
    adapter = RealPyjevsimStructuralAdapter(
        root,
        time_resolution=0.5,
        input_ports={"in_cmd": ("cons", "inp")},
        output_ports={"out_seq": ("prod", "out")},
    )
    with pytest.raises(KeyError, match="no input port"):
        adapter.external_transition("undeclared", b"x")


def test_structural_adapter_time_advance_returns_finite() -> None:
    """``time_advance`` returns a finite positive ``dt`` even on a fresh
    adapter (when the executor has no scheduled events yet)."""
    from _real_pyjevsim_adapter import RealPyjevsimStructuralAdapter

    root, _prod, _cons = _build_hierarchy()
    adapter = RealPyjevsimStructuralAdapter(
        root,
        time_resolution=0.5,
        input_ports={"in_cmd": ("cons", "inp")},
        output_ports={"out_seq": ("prod", "out")},
        default_ta=0.5,
    )
    dt = adapter.time_advance()
    assert dt > 0
    assert dt != float("inf")
