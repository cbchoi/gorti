"""Unit tests for ``pyjevsim_bridge.HLAFederate``.

These cover behavior not pinned by the specification tests
under ``tests/spec/m4/test_spec_m4_time_advance.py``:

* ``deliver_external`` queueing semantics (multiple events drained in
  arrival order on the same step),
* unmapped FOM classes are silently skipped,
* unmapped output ports are silently skipped,
* ``_now`` advances by ta on a clean internal cycle,
* successive ``step_once`` calls reuse the lazily-acquired federate
  (no second ``join_federation``),
* ``run(ticks=N)`` runs N cycles and resigns afterwards,
* a ReceiveInteraction arriving in flight is buffered and delivered.
"""

from __future__ import annotations

import pytest

from pyjevsim_bridge import HLAFederate, PortMapping
from rti1516e.connection import FederationSpec
from rti1516e.events import FederationHalted, ReceiveInteraction, TimeAdvanceGrant
from tests.spec.m4._fakes import FakeRtiServer, StubCoupledModel


def _make_federate(
    stub: StubCoupledModel,
    mapping: dict[str, str] | None = None,
    *,
    name: str = "alice",
) -> HLAFederate:
    return HLAFederate(
        coupled_model=stub,
        federation=FederationSpec(name="demo"),
        federate_name=name,
        port_mapping=PortMapping.from_dict(mapping or {}),
        url="memory://fake-rti",
    )


@pytest.mark.asyncio
async def test_deliver_external_drains_in_arrival_order() -> None:
    fake = FakeRtiServer()
    del fake  # ensures the in-process transport is registered
    stub = StubCoupledModel()
    fed = _make_federate(stub, {"in_a": "AClass", "in_b": "BClass"})
    await fed.deliver_external("AClass", payload="p1")
    await fed.deliver_external("BClass", payload="p2")
    await fed.deliver_external("AClass", payload="p3")
    await fed.step_once()
    ports = [c.port for c in stub.external_transitions]
    payloads = [c.payload for c in stub.external_transitions]
    assert ports == ["in_a", "in_b", "in_a"]
    assert payloads == ["p1", "p2", "p3"]
    assert stub.internal_transitions == 0


@pytest.mark.asyncio
async def test_deliver_external_unmapped_class_is_dropped() -> None:
    fake = FakeRtiServer()
    del fake
    stub = StubCoupledModel()
    fed = _make_federate(stub, {"in_a": "AClass"})
    await fed.deliver_external("UnknownClass", payload="x")
    await fed.deliver_external("AClass", payload="y")
    await fed.step_once()
    assert [c.port for c in stub.external_transitions] == ["in_a"]
    assert [c.payload for c in stub.external_transitions] == ["y"]


@pytest.mark.asyncio
async def test_internal_cycle_skips_unmapped_output_port() -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.append(1.0)  # noqa: SLF001
    stub._outputs.append({"out_known": b"k", "out_unknown": b"u"})  # noqa: SLF001
    fed = _make_federate(stub, {"out_known": "Known"})
    await fed.step_once()
    sends = fake.calls_for("send_interaction")
    assert len(sends) == 1
    assert sends[0].args["class_name"] == "Known"
    assert stub.internal_transitions == 1


@pytest.mark.asyncio
async def test_internal_cycle_sends_outputs_in_port_name_order() -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.append(1.0)  # noqa: SLF001
    stub._outputs.append(  # noqa: SLF001
        {"out_zeta": b"z", "out_alpha": b"a", "out_mu": b"m"}
    )
    fed = _make_federate(
        stub,
        {"out_zeta": "Zeta", "out_alpha": "Alpha", "out_mu": "Mu"},
    )
    await fed.step_once()
    sends = fake.calls_for("send_interaction")
    assert [call.args["class_name"] for call in sends] == ["Alpha", "Mu", "Zeta"]
    await fed.aclose()


@pytest.mark.asyncio
async def test_now_advances_by_ta_on_internal_cycle() -> None:
    fake = FakeRtiServer()
    del fake
    stub = StubCoupledModel()
    stub._ta.append(2.5)  # noqa: SLF001
    stub._outputs.append({})  # noqa: SLF001
    fed = _make_federate(stub)
    assert fed._now == 0.0  # noqa: SLF001
    await fed.step_once()
    assert fed._now == 2.5  # noqa: SLF001


@pytest.mark.asyncio
async def test_successive_step_once_reuses_federate() -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.extend([1.0, 1.0, 1.0])  # noqa: SLF001
    stub._outputs.extend([{}, {}, {}])  # noqa: SLF001
    fed = _make_federate(stub)
    await fed.step_once()
    await fed.step_once()
    await fed.step_once()
    # Exactly one join_federation across the three cycles.
    assert len(fake.calls_for("join_federation")) == 1
    assert len(fake.calls_for("enable_time_regulation")) == 1
    assert len(fake.calls_for("enable_time_constrained")) == 1
    # Three NER calls, three implicit grants, three internal cycles.
    assert len(fake.calls_for("next_message_request")) == 3
    assert stub.internal_transitions == 3
    assert fed._now == 3.0  # noqa: SLF001
    await fed.aclose()


@pytest.mark.asyncio
async def test_federation_halt_fails_instead_of_being_ignored() -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.append(1.0)  # noqa: SLF001
    fed = _make_federate(stub)
    federate = await fed._ensure_federate()  # noqa: SLF001
    fake.push_event(
        federate.handle,
        FederationHalted(cause="stall", stalled_federate_handle=federate.handle),
    )

    with pytest.raises(RuntimeError, match="federation halted"):
        await fed.step_once()
    with pytest.raises(RuntimeError, match="previous advance failed"):
        await fed.step_once()
    await fed.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize("time_advance", [-1.0, float("inf"), float("nan")])
async def test_model_time_advance_must_be_finite_and_non_negative(
    time_advance: float,
) -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.append(time_advance)  # noqa: SLF001
    fed = _make_federate(stub)

    with pytest.raises(ValueError, match="finite and non-negative"):
        await fed.step_once()
    assert not fake.calls_for("next_message_request")
    with pytest.raises(RuntimeError, match="previous advance failed"):
        await fed.step_once()
    await fed.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize("grant_time", [-1.0, float("inf"), float("nan")])
async def test_invalid_time_grant_fails_closed(grant_time: float) -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.append(1.0)  # noqa: SLF001
    fed = _make_federate(stub)
    federate = await fed._ensure_federate()  # noqa: SLF001
    fake.push_event(federate.handle, TimeAdvanceGrant(grant_time))

    with pytest.raises(ValueError, match="invalid TimeAdvanceGrant"):
        await fed.step_once()
    assert fed._now == 0.0  # noqa: SLF001
    with pytest.raises(RuntimeError, match="previous advance failed"):
        await fed.step_once()
    await fed.aclose()


@pytest.mark.parametrize("lookahead", [-1.0, float("inf"), float("nan")])
def test_time_management_lookahead_is_validated(lookahead: float) -> None:
    stub = StubCoupledModel()
    kwargs = {
        "coupled_model": stub,
        "federation": FederationSpec(name="demo"),
        "federate_name": "alice",
        "port_mapping": PortMapping.from_dict({}),
    }
    with pytest.raises(ValueError, match="lookahead"):
        HLAFederate(**kwargs, lookahead=lookahead)


@pytest.mark.parametrize("timeout", [0.0, float("inf"), float("nan")])
def test_time_management_timeout_is_validated(timeout: float) -> None:
    stub = StubCoupledModel()
    with pytest.raises(ValueError, match="grant_timeout"):
        HLAFederate(
            coupled_model=stub,
            federation=FederationSpec(name="demo"),
            federate_name="alice",
            port_mapping=PortMapping.from_dict({}),
            grant_timeout=timeout,
        )


@pytest.mark.asyncio
async def test_run_reinitializes_time_and_object_declarations() -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.extend([1.0, 1.0])  # noqa: SLF001
    stub._outputs.extend([{}, {}])  # noqa: SLF001
    stub.object_class_publications = lambda: {"State": ["value"]}  # type: ignore[attr-defined]
    fed = _make_federate(stub)

    await fed.run(ticks=1)
    await fed.run(ticks=1)

    assert len(fake.calls_for("enable_time_regulation")) == 2
    assert len(fake.calls_for("enable_time_constrained")) == 2
    assert len(fake.calls_for("publish_object_class")) == 2


@pytest.mark.asyncio
async def test_post_grant_model_failure_commits_clock_and_fails_closed() -> None:
    fake = FakeRtiServer()
    del fake
    stub = StubCoupledModel()
    stub._ta.append(2.0)  # noqa: SLF001

    def broken_output() -> dict[str, object]:
        raise RuntimeError("model output failed")

    stub.output_handler = broken_output  # type: ignore[method-assign]
    fed = _make_federate(stub)
    with pytest.raises(RuntimeError, match="model output failed"):
        await fed.step_once()
    assert fed._now == 2.0  # noqa: SLF001
    with pytest.raises(RuntimeError, match="previous advance failed"):
        await fed.step_once()
    await fed.aclose()


@pytest.mark.asyncio
async def test_run_executes_ticks_and_resigns() -> None:
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.extend([1.0, 1.0])  # noqa: SLF001
    stub._outputs.extend([{}, {}])  # noqa: SLF001
    fed = _make_federate(stub)
    await fed.run(ticks=2)
    assert stub.internal_transitions == 2
    assert len(fake.calls_for("join_federation")) == 1
    assert len(fake.calls_for("resign_federation")) == 1


@pytest.mark.asyncio
async def test_in_flight_receive_interaction_is_buffered() -> None:
    """When a ReceiveInteraction arrives via fed.events() before the
    TimeAdvanceGrant, the bridge buffers it and delivers it via
    external_transition; the cycle skips its internal phase."""
    fake = FakeRtiServer()
    stub = StubCoupledModel()
    stub._ta.append(5.0)  # noqa: SLF001
    fed = _make_federate(stub, {"in_cmd": "Command"})

    # Force the federate to be created so we know its handle.
    federate = await fed._ensure_federate()  # noqa: SLF001
    # Push the interaction BEFORE the auto-grant (events_for will create
    # the queue if needed, but since auto-grant happens after this push
    # via record(), order of dequeue is interaction-first).
    fake.push_event(
        federate.handle,
        ReceiveInteraction(
            class_name="Command", parameters={"_payload": "go"}, timestamp=1.0
        ),
    )
    await fed.step_once()
    assert stub.external_transitions, "expected an external transition"
    assert stub.external_transitions[0].port == "in_cmd"
    assert stub.internal_transitions == 0
    await fed.aclose()


@pytest.mark.asyncio
async def test_run_requires_connection_or_url() -> None:
    stub = StubCoupledModel()
    fed = HLAFederate(
        coupled_model=stub,
        federation=FederationSpec(name="demo"),
        federate_name="alice",
        port_mapping=PortMapping.from_dict({}),
    )
    with pytest.raises(RuntimeError, match="neither connection nor url"):
        await fed.run(ticks=1)


@pytest.mark.asyncio
async def test_aclose_is_idempotent() -> None:
    fake = FakeRtiServer()
    del fake
    stub = StubCoupledModel()
    stub._ta.append(1.0)  # noqa: SLF001
    stub._outputs.append({})  # noqa: SLF001
    fed = _make_federate(stub)
    await fed.step_once()
    await fed.aclose()
    await fed.aclose()  # second call: no-op


@pytest.mark.asyncio
async def test_external_then_internal_in_sequence() -> None:
    """First step_once with a queued external runs external_transition
    only; second call (now empty queue) runs the internal cycle."""
    fake = FakeRtiServer()
    del fake
    stub = StubCoupledModel()
    stub._ta.append(2.0)  # noqa: SLF001
    stub._outputs.append({})  # noqa: SLF001
    fed = _make_federate(stub, {"in_cmd": "Command"})
    await fed.deliver_external("Command", payload="go")
    await fed.step_once()
    assert stub.external_transitions and stub.external_transitions[0].port == "in_cmd"
    assert stub.internal_transitions == 0
    # Second cycle: queue empty, runs internal cycle.
    await fed.step_once()
    assert stub.internal_transitions == 1
    assert fed._now == 2.0  # noqa: SLF001
    await fed.aclose()
