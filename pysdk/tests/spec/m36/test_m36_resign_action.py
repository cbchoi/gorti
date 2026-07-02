"""M36 DD-1 — resignFederationExecution must thread the action to the wire.

Bug (parity-CF, xlang_python_cpp_pubsub 16/17): Layer-2
``Rti1516eAmbassador.resignFederationExecution(action)`` discarded the
action (``del action``), so Layer-1 always sent
``RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES`` and a
``CANCEL_THEN_DELETE_THEN_DIVEST`` resign never deleted the resigning
federate's instances (no REMOVE for subscribers).

Contract asserted here:
  1. Layer 2 threads the IEEE 1516.1-2010 §4.10 action string through the
     FederateContextManager into the transport ``record()`` boundary.
  2. The gRPC transport maps every IEEE action name to the matching
     ``rti.v1.ResignAction`` proto enum value (M24 W2 wire surface).
"""

from __future__ import annotations

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e._transport import resign_action_to_proto

# Spec-test-only fake (same consumption pattern as tests/spec/m4).
from tests.spec.m4._fakes import FakeRtiServer


@pytest.fixture
def fake_rti() -> FakeRtiServer:
    return FakeRtiServer()


def _resign_calls(fake_rti: FakeRtiServer) -> list:
    return [c for c in fake_rti.calls if c.method == "resign_federation"]


def test_resign_action_reaches_transport_record(fake_rti: FakeRtiServer) -> None:
    """§4.10 — the explicit action must reach the wire boundary."""
    amb = Rti1516eAmbassador()
    try:
        amb.connect(amb, "memory://fake-rti")
        amb.createFederationExecution("demo", ["demo.fom.xml"])
        amb.joinFederationExecution("alice", "demo")
        amb.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
    finally:
        amb.disconnect()

    calls = _resign_calls(fake_rti)
    assert len(calls) == 1
    assert calls[0].args.get("action") == "CANCEL_THEN_DELETE_THEN_DIVEST"


def test_resign_default_action_is_unconditional_divest(fake_rti: FakeRtiServer) -> None:
    """No-arg resign keeps the pre-M36 default on the wire."""
    amb = Rti1516eAmbassador()
    try:
        amb.connect(amb, "memory://fake-rti")
        amb.createFederationExecution("demo", ["demo.fom.xml"])
        amb.joinFederationExecution("alice", "demo")
        amb.resignFederationExecution()
    finally:
        amb.disconnect()

    calls = _resign_calls(fake_rti)
    assert len(calls) == 1
    assert calls[0].args.get("action") == "UNCONDITIONALLY_DIVEST_ATTRIBUTES"


def test_resign_rejects_unknown_action(fake_rti: FakeRtiServer) -> None:
    """§4.10 InvalidResignAction — unknown designators fail fast, before
    any resign reaches the transport."""
    amb = Rti1516eAmbassador()
    try:
        amb.connect(amb, "memory://fake-rti")
        amb.createFederationExecution("demo", ["demo.fom.xml"])
        amb.joinFederationExecution("alice", "demo")
        with pytest.raises(ValueError):
            amb.resignFederationExecution("DELETE_EVERYTHING_TWICE")
        assert _resign_calls(fake_rti) == []
    finally:
        amb.disconnect()


# --- Layer-1 mapping: IEEE action name -> rti.v1.ResignAction enum ---------

_EXPECTED_WIRE_NAMES = {
    "UNCONDITIONALLY_DIVEST_ATTRIBUTES": "RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES",
    "DELETE_OBJECTS": "RESIGN_ACTION_DELETE_OBJECTS",
    "CANCEL_PENDING_OWNERSHIP_ACQUISITIONS": "RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP",
    "DELETE_OBJECTS_THEN_DIVEST": "RESIGN_ACTION_DELETE_THEN_DIVEST",
    "CANCEL_THEN_DELETE_THEN_DIVEST": "RESIGN_ACTION_CANCEL_THEN_DELETE",
    "NO_ACTION": "RESIGN_ACTION_NO_ACTION",
}


@pytest.mark.parametrize(("ieee_name", "wire_name"), sorted(_EXPECTED_WIRE_NAMES.items()))
def test_resign_action_to_proto_mapping(ieee_name: str, wire_name: str) -> None:
    common_pb2 = pytest.importorskip("rti.v1.common_pb2")
    assert resign_action_to_proto(ieee_name) == common_pb2.ResignAction.Value(wire_name)


def test_resign_action_to_proto_passthrough_and_default() -> None:
    common_pb2 = pytest.importorskip("rti.v1.common_pb2")
    unconditional = common_pb2.ResignAction.RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES
    # None -> pre-M24 default; int -> passthrough; junk -> ValueError.
    assert resign_action_to_proto(None) == unconditional
    assert resign_action_to_proto(int(unconditional)) == int(unconditional)
    with pytest.raises(ValueError):
        resign_action_to_proto("NOT_AN_ACTION")
