"""Unit tests for the real-pyjevsim adapter (TASK-081 Part B).

Validates that ``RealPyjevsimAdapter`` exposes the
``CoupledModelProtocol`` shape on top of a real
``pyjevsim.behavior_model.BehaviorModel``. Cut-1 only wraps a single
atomic behavior model — these tests cover that path; the structural
hierarchy adapter is post-MVP and unused by the cross-language smoke.
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
# Re-import the submodule explicitly: ``pytest.importorskip("pyjevsim")``
# returns the package object but ``pyjevsim.behavior_model`` is not
# auto-imported on every revision. mypy also can't see attributes on
# the importorskip-returned Any, so we use a runtime-only base class
# fetched via getattr to keep both happy.
_BehaviorModel = pytest.importorskip(
    "pyjevsim.behavior_model"
).BehaviorModel


class _CountingTickModel(_BehaviorModel):  # type: ignore[misc, valid-type]
    """Minimal pyjevsim atomic model used as the adapter's target.

    Counts ``int_trans`` invocations and exposes the count via
    ``output(deliverer)`` on the ``"out_ticks"`` port. Mirrors the M4
    Producer pattern but uses real pyjevsim instead of a duck-typed
    Python class.
    """

    def __init__(self) -> None:
        super().__init__("counter")
        self.tick = 0
        self.ext_received: list[tuple[str, object]] = []
        # Wire pyjevsim's port machinery so the deliverer actually has
        # something to receive — without this, output() may silently
        # drop messages on some pyjevsim revisions.
        self.insert_state("active", deadline=1.0)
        self.insert_output_port("out_ticks")
        self.insert_input_port("in_cmd")

    def output(self, msg_deliver):  # type: ignore[no-untyped-def]
        msg_deliver.insert_message("out_ticks", self.tick)

    def int_trans(self) -> None:
        self.tick += 1

    def ext_trans(self, port: str, msg) -> None:  # type: ignore[no-untyped-def]
        self.ext_received.append((port, msg))


def test_real_pyjevsim_adapter_satisfies_protocol_shape() -> None:
    """All four DEVS-canonical methods are present + callable."""
    from _real_pyjevsim_adapter import RealPyjevsimAdapter

    model = _CountingTickModel()
    adapter = RealPyjevsimAdapter(model, ta_seconds=0.5)
    assert callable(adapter.time_advance)
    assert callable(adapter.output_handler)
    assert callable(adapter.internal_transition)
    assert callable(adapter.external_transition)
    assert adapter.time_advance() == 0.5


def test_real_pyjevsim_adapter_output_then_internal_increments_tick() -> None:
    """Cut-1 cycle: output_handler reads the model, internal_transition
    advances state. After one cycle, the model's tick counter is 1
    and the adapter returns the pre-tick value (0) on the output port."""
    from _real_pyjevsim_adapter import RealPyjevsimAdapter

    model = _CountingTickModel()
    adapter = RealPyjevsimAdapter(
        model, ta_seconds=1.0, out_ports=("out_ticks",)
    )
    out = adapter.output_handler()
    assert out == {"out_ticks": 0}
    adapter.internal_transition()
    assert model.tick == 1
    out2 = adapter.output_handler()
    assert out2 == {"out_ticks": 1}


def test_real_pyjevsim_adapter_external_transition_delegates() -> None:
    """``external_transition(port, payload)`` reaches the model's
    ``ext_trans``, preserving the (port, payload) pair."""
    from _real_pyjevsim_adapter import RealPyjevsimAdapter

    model = _CountingTickModel()
    adapter = RealPyjevsimAdapter(model)
    adapter.external_transition("in_cmd", b"\x01\x02")
    adapter.external_transition("in_cmd", "stop")
    assert model.ext_received == [("in_cmd", b"\x01\x02"), ("in_cmd", "stop")]
