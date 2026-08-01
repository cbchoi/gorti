"""Focused tests for the real pyjevsim models."""
# ruff: noqa: S101

from __future__ import annotations

import sys
from pathlib import Path

import pytest

HERE = Path(__file__).resolve().parent
BASE_EXAMPLE = HERE.parent / "pyjevsim"
for path in (BASE_EXAMPLE, HERE):
    if str(path) not in sys.path:
        sys.path.insert(0, str(path))

BehaviorModel = pytest.importorskip(
    "pyjevsim.behavior_model",
    reason="pyjevsim is an optional runtime dependency",
).BehaviorModel

from _real_pyjevsim_adapter import RealPyjevsimAdapter  # noqa: E402
from models import PulseGenerator, PulseSink  # noqa: E402


def test_generator_uses_real_behavior_model_and_emits_big_endian_sequence() -> None:
    model = PulseGenerator()
    adapter = RealPyjevsimAdapter(
        model,
        ta_seconds=1.0,
        out_ports=("out_seq",),
    )

    assert isinstance(model, BehaviorModel)
    assert adapter.output_handler() == {"out_seq": b"\x00\x00\x00\x01"}
    adapter.internal_transition()
    assert adapter.output_handler() == {"out_seq": b"\x00\x00\x00\x02"}
    assert model.published == [1, 2]


def test_sink_accepts_transport_alias_and_declared_fom_parameter() -> None:
    model = PulseSink()
    adapter = RealPyjevsimAdapter(model, ta_seconds=1.0)

    assert isinstance(model, BehaviorModel)
    adapter.external_transition("in_seq", {"_payload": (7).to_bytes(4, "big")})
    adapter.external_transition("in_seq", {"seq": (8).to_bytes(4, "big")})

    assert model.received == [7, 8]


@pytest.mark.parametrize(
    ("port", "message"),
    [
        ("other", {"seq": b"\x00\x00\x00\x01"}),
        ("in_seq", b"\x00\x00\x00\x01"),
        ("in_seq", {"seq": b"\x00"}),
        ("in_seq", {"seq": "1"}),
    ],
)
def test_sink_ignores_messages_outside_its_wire_contract(
    port: str,
    message: object,
) -> None:
    model = PulseSink()

    model.ext_trans(port, message)

    assert model.received == []
