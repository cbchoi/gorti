from __future__ import annotations

import json
from collections import deque
from pathlib import Path

import pytest

from pyjevsim_bridge.rl.federation import (
    ACTION_CLASS,
    TRANSITION_CLASS,
    EventStreamExhaustedError,
    FederationHaltedError,
    GortiRolloutChannel,
    canonical_json,
    validate_envelope,
)
from pyjevsim_bridge.rl.records import TransitionRecord
from rti1516e.events import (
    FederationHalted,
    FederationSynchronized,
    ReceiveInteraction,
    SynchronizationPointAnnounced,
    TimeAdvanceGrant,
)
from rti1516e.fom import parse

RL_FOM = Path(__file__).resolve().parents[1] / "pyjevsim_bridge" / "rl" / "pyjevsim-rl-fom.xml"


def envelope(**updates: object) -> dict[str, object]:
    value: dict[str, object] = {
        "schema_version": 1,
        "run_id": "run-1",
        "generation": 3,
        "worker_id": "worker-1",
        "episode_id": "episode-1",
        "step_id": 0,
        "policy_version": 0,
        "idempotency_key": "episode-1:0",
        "logical_time": 2.0,
        "payload": {"action": 1},
    }
    value.update(updates)
    return value


class FakeSync:
    def __init__(self, calls: list[tuple[object, ...]]) -> None:
        self.calls = calls

    async def register_synchronization_point(self, label: str) -> None:
        self.calls.append(("register_sync", label))

    async def synchronization_point_achieved(self, label: str) -> None:
        self.calls.append(("achieve_sync", label))


class FakeFederate:
    def __init__(self, batches: list[list[object]] | None = None) -> None:
        self.calls: list[tuple[object, ...]] = []
        self._batches = deque(batches or [])
        self.sync = FakeSync(self.calls)

    async def publish_interaction_class(self, name: str) -> None:
        self.calls.append(("publish", name))

    async def subscribe_interaction_class(self, name: str) -> None:
        self.calls.append(("subscribe", name))

    async def enable_time_regulation(self, lookahead: float) -> None:
        self.calls.append(("regulating", lookahead))

    async def enable_time_constrained(self) -> None:
        self.calls.append(("constrained",))

    async def send_interaction(
        self,
        name: str,
        parameters: dict[str, object],
        *,
        timestamp: float,
    ) -> None:
        self.calls.append(("send", name, parameters, timestamp))

    async def next_message_request(self, time: float) -> None:
        self.calls.append(("ner", time))

    async def events(self):  # type: ignore[no-untyped-def]
        for event in (self._batches.popleft() if self._batches else []):
            yield event


def test_canonical_codec_and_validation() -> None:
    assert canonical_json({"b": 2, "a": 1}) == b'{"a":1,"b":2}'
    assert validate_envelope(envelope(), generation=3)["logical_time"] == 2.0
    with pytest.raises(ValueError, match="stale federation generation"):
        validate_envelope(envelope(), generation=4)


@pytest.mark.asyncio
async def test_role_declaration_and_explicit_time_enable() -> None:
    fake = FakeFederate()
    channel = GortiRolloutChannel(fake, "coordinator", generation=3)
    await channel.declare()
    await channel.enable_time(lookahead=1.0)
    assert ("publish", ACTION_CLASS) in fake.calls
    assert ("subscribe", TRANSITION_CLASS) in fake.calls
    assert fake.calls[-2:] == [("regulating", 1.0), ("constrained",)]


@pytest.mark.asyncio
async def test_all_roles_declare_and_send_only_authorized_interactions() -> None:
    learner_federate = FakeFederate()
    learner = GortiRolloutChannel(learner_federate, "learner", generation=3)
    await learner.declare()
    await learner.enable_time(lookahead=1.0)
    await learner.send_policy_announcement(envelope())
    assert ("publish", "RLPolicyAnnouncement") in learner_federate.calls
    assert learner_federate.calls[-1][1] == "RLPolicyAnnouncement"

    evaluator_federate = FakeFederate()
    evaluator = GortiRolloutChannel(evaluator_federate, "evaluator", generation=3)
    await evaluator.declare()
    await evaluator.enable_time(lookahead=1.0)
    await evaluator.send_control(envelope())
    assert ("subscribe", TRANSITION_CLASS) in evaluator_federate.calls
    assert evaluator_federate.calls[-1][1] == "RLControl"

    actor = GortiRolloutChannel(FakeFederate(), "actor", generation=3)
    await actor.enable_time(lookahead=1.0)
    with pytest.raises(RuntimeError, match="learner"):
        await actor.send_policy_announcement(envelope())


@pytest.mark.asyncio
async def test_tso_send_and_interactions_precede_grant() -> None:
    wire = canonical_json(envelope(payload={"simulation_time": 1.0}))
    fake = FakeFederate(
        [[
            ReceiveInteraction(TRANSITION_CLASS, {"payload": wire}, 2.0),
            TimeAdvanceGrant(2.0),
        ]]
    )
    channel = GortiRolloutChannel(fake, "coordinator", generation=3)
    await channel.enable_time(lookahead=1.0)
    await channel.send_action(envelope())
    batch = await channel.advance_to(2.0)
    assert fake.calls[-2][0:2] == ("send", ACTION_CLASS)
    assert batch.granted_time == 2.0
    assert [item.interaction_class for item in batch.interactions] == [TRANSITION_CLASS]


@pytest.mark.asyncio
async def test_halt_and_stream_exhaustion_are_errors() -> None:
    halted = FakeFederate([[FederationHalted("stall", 9)]])
    with pytest.raises(FederationHaltedError):
        await GortiRolloutChannel(halted, "worker", generation=0).advance_to(1.0)
    with pytest.raises(EventStreamExhaustedError):
        await GortiRolloutChannel(
            FakeFederate([[]]), "worker", generation=0
        ).advance_to(1.0)


@pytest.mark.asyncio
async def test_sync_rendezvous() -> None:
    fake = FakeFederate([
        [SynchronizationPointAnnounced("ready", b"")],
        [FederationSynchronized("ready")],
    ])
    channel = GortiRolloutChannel(fake, "coordinator", generation=0)
    await channel.rendezvous("ready", register=True)
    assert fake.calls == [
        ("register_sync", "ready"),
        ("achieve_sync", "ready"),
    ]


@pytest.mark.asyncio
async def test_transition_record_round_trips_with_separate_delivery_time() -> None:
    fake = FakeFederate()
    channel = GortiRolloutChannel(fake, "actor", generation=3)
    await channel.enable_time(lookahead=1.0)
    record = TransitionRecord(
        run_id="run-1",
        generation=3,
        worker_id="worker-1",
        episode_id="episode-1",
        step_id=1,
        policy_version=0,
        idempotency_key="episode-1:1",
        logical_time=0.25,
        previous_observation={"nested": [0]},
        action=1,
        next_observation={"nested": [1]},
        reward=1.0,
        terminated=False,
        truncated=False,
    )

    await channel.send_transition(record)

    send = fake.calls[-1]
    assert send[0:2] == ("send", TRANSITION_CLASS)
    assert send[3] == 1.0
    payload = send[2]["payload"]
    assert isinstance(payload, bytes)
    decoded = validate_envelope(json.loads(payload))
    assert decoded["logical_time"] == 1.0
    assert decoded["payload"]["simulation_time"] == 0.25


@pytest.mark.asyncio
async def test_transition_rejects_conflicting_simulation_time() -> None:
    channel = GortiRolloutChannel(FakeFederate(), "worker", generation=3)
    await channel.enable_time(lookahead=1.0)
    value = envelope(payload={"simulation_time": 0.5})
    with pytest.raises(ValueError, match="conflicts"):
        await channel.send_transition(value)


@pytest.mark.asyncio
async def test_advance_preserves_unrelated_sync_callback_for_rendezvous() -> None:
    fake = FakeFederate([
        [SynchronizationPointAnnounced("ready", b""), TimeAdvanceGrant(1.0)],
        [FederationSynchronized("ready")],
    ])
    channel = GortiRolloutChannel(fake, "coordinator", generation=0)

    await channel.advance_to(1.0)
    await channel.rendezvous("ready")

    assert ("achieve_sync", "ready") in fake.calls


@pytest.mark.asyncio
async def test_rendezvous_preserves_time_grant_for_next_advance() -> None:
    fake = FakeFederate([
        [TimeAdvanceGrant(1.0), SynchronizationPointAnnounced("ready", b"")],
        [FederationSynchronized("ready")],
    ])
    channel = GortiRolloutChannel(fake, "coordinator", generation=0)

    await channel.rendezvous("ready")
    batch = await channel.advance_to(1.0)

    assert batch.granted_time == 1.0


def test_rl_fom_parses_and_declares_the_vertical_slice_interactions() -> None:
    result = parse([RL_FOM])
    assert not result.diagnostics, result.diagnostics
    assert result.fom is not None
    for class_name in (ACTION_CLASS, TRANSITION_CLASS, "RLControl", "RLPolicyAnnouncement"):
        interaction = result.fom.find_interaction_class(class_name)
        assert interaction is not None
        assert interaction.order == "TimeStamp"
        assert interaction.transportation == "HLAreliable"
        assert len(interaction.parameters) == 1
        assert interaction.parameters[0].name == "payload"
        assert interaction.parameters[0].data_type == "HLAopaqueData"
