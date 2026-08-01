"""Behavior tests for rebuild reset and the Gym-like RL environment."""

from __future__ import annotations

from dataclasses import dataclass

import pytest

from pyjevsim_bridge.rl.contracts import (
    EnvironmentClosedError,
    EpisodeContext,
    EpisodeStateError,
    EpisodeStepError,
    StepView,
)
from pyjevsim_bridge.rl.environment import PyJevSimEnv
from pyjevsim_bridge.rl.executor import FixedDeltaBoundary


class FakeExecutor:
    def __init__(self) -> None:
        self.now = 0.0
        self.terminate_calls = 0
        self.terminated = False
        self.action: object = None

    def get_global_time(self) -> float:
        return self.now

    def get_next_event_time(self) -> float:
        return self.now + 1.0

    def insert_external_event(
        self, port: str, payload: object, scheduled_time: float = 0
    ) -> None:
        del port, scheduled_time
        self.action = payload

    def step(self, granted_time: float) -> object:
        self.now = granted_time
        return {"time": self.now, "action": self.action}

    def is_terminated(self) -> bool:
        return self.terminated

    def terminate_simulation(self) -> None:
        self.terminate_calls += 1


@dataclass
class FakeBinding:
    context: EpisodeContext

    def __post_init__(self) -> None:
        self.executor = FakeExecutor()
        self.closed = 0
        self.transitions: list[StepView] = []

    def apply_action(self, action: object) -> None:
        if not isinstance(action, int):
            raise ValueError("action must be an int")
        self.executor.insert_external_event("action", action)

    def next_decision_time(self) -> float:
        return self.executor.now + 1.0

    def observe(self, external_events: object) -> object:
        if external_events == ():
            return {"time": 0.0, "seed": self.context.seed}
        return external_events

    def reward(self, transition: StepView) -> float:
        self.transitions.append(transition)
        return float(transition.action)

    def terminated(self, transition: StepView) -> bool:
        return transition.action == 9

    def info(self, transition: StepView) -> dict[str, object]:
        return {
            "model": "fake",
            "step_id": -100,
            "logical_time": -100.0,
        }

    def close(self) -> None:
        self.closed += 1


class FakeFactory:
    def __init__(self) -> None:
        self.contexts: list[EpisodeContext] = []
        self.bindings: list[FakeBinding] = []

    def __call__(self, context: EpisodeContext) -> FakeBinding:
        self.contexts.append(context)
        binding = FakeBinding(context)
        self.bindings.append(binding)
        return binding


def test_reset_rebuilds_executor_and_records_episode_provenance() -> None:
    factory = FakeFactory()
    env = PyJevSimEnv(factory, instance_id="worker-2", run_id="run-a")
    first_observation, first_info = env.reset(seed=41, options={"difficulty": 2})
    first = factory.bindings[0]
    second_observation, second_info = env.reset(seed=42)

    assert first_observation == {"time": 0.0, "seed": 41}
    assert second_observation == {"time": 0.0, "seed": 42}
    assert first_info["episode_id"] == "worker-2:episode-1"
    assert second_info["episode_id"] == "worker-2:episode-2"
    assert second_info["run_id"] == "run-a"
    assert factory.bindings[0] is not factory.bindings[1]
    assert first.executor.terminate_calls == 1
    assert first.closed == 1
    assert factory.contexts[0].options["difficulty"] == 2
    env.close()


def test_step_returns_gym_five_tuple_and_authoritative_provenance() -> None:
    factory = FakeFactory()
    env = PyJevSimEnv(
        factory,
        instance_id="env-7",
        boundary=FixedDeltaBoundary(0.25),
        run_id="run-z",
    )
    env.reset(seed=8)
    observation, reward, terminated, truncated, info = env.step(3)

    assert observation == {"time": 0.25, "action": 3}
    assert reward == 3.0
    assert terminated is False
    assert truncated is False
    assert info == {
        "model": "fake",
        "run_id": "run-z",
        "episode_id": "env-7:episode-1",
        "instance_id": "env-7",
        "step_id": 1,
        "logical_time": 0.25,
        "seed": 8,
    }
    transition = factory.bindings[0].transitions[0]
    assert transition.previous_logical_time == 0.0
    assert transition.previous_observation == {"time": 0.0, "seed": 8}
    env.close()


def test_step_after_termination_requires_reset() -> None:
    env = PyJevSimEnv(FakeFactory())
    env.reset()
    _, _, terminated, truncated, _ = env.step(9)
    assert terminated is True
    assert truncated is False
    with pytest.raises(EpisodeStateError, match="reset required"):
        env.step(1)
    env.reset()
    assert env.step(1)[2:4] == (False, False)
    env.close()


def test_max_steps_sets_truncated_and_blocks_further_steps() -> None:
    env = PyJevSimEnv(FakeFactory(), max_steps=1)
    env.reset()
    assert env.step(1)[2:4] == (False, True)
    with pytest.raises(EpisodeStateError, match="reset required"):
        env.step(1)
    env.close()


def test_step_requires_reset_and_valid_action_is_delegated() -> None:
    env = PyJevSimEnv(FakeFactory())
    with pytest.raises(EpisodeStateError, match="reset must be called"):
        env.step(1)
    env.reset()
    with pytest.raises(EpisodeStepError, match="action must be an int") as failure:
        env.step("invalid")
    assert failure.value.phase == "apply_action"
    assert failure.value.completion_boundary == "pre-commit"
    env.close()


def test_close_is_idempotent_and_rejects_future_operations() -> None:
    factory = FakeFactory()
    env = PyJevSimEnv(factory)
    env.reset()
    binding = factory.bindings[0]
    env.close()
    env.close()
    assert binding.closed == 1
    assert binding.executor.terminate_calls == 1
    with pytest.raises(EnvironmentClosedError, match="closed"):
        env.reset()
    with pytest.raises(EnvironmentClosedError, match="closed"):
        env.step(1)


def test_post_advance_failure_requires_rebuild_reset() -> None:
    factory = FakeFactory()
    env = PyJevSimEnv(factory)
    env.reset()
    binding = factory.bindings[0]

    def broken_observe(external_events: object) -> object:
        del external_events
        raise RuntimeError("observation failed after commit")

    binding.observe = broken_observe  # type: ignore[method-assign]
    with pytest.raises(EpisodeStepError, match="observation failed") as failure:
        env.step(1)
    assert failure.value.phase == "observe"
    assert failure.value.completion_boundary == "committed"
    assert failure.value.logical_time == 1.0
    with pytest.raises(EpisodeStateError, match="episode failed"):
        env.step(1)

    env.reset()
    assert factory.bindings[1] is not binding
    assert env.step(1)[1] == 1.0
    env.close()


def test_step_view_deeply_snapshots_plugin_inputs() -> None:
    factory = FakeFactory()
    env = PyJevSimEnv(factory)
    env.reset()
    action = {"values": [1]}
    binding = factory.bindings[0]
    binding.apply_action = lambda value: binding.executor.insert_external_event(  # type: ignore[method-assign]
        "action", value
    )
    binding.reward = lambda transition: 0.0  # type: ignore[method-assign]
    captured: list[StepView] = []
    binding.info = lambda view: captured.append(view) or {}  # type: ignore[method-assign]

    env.step(action)
    action["values"][0] = 99
    assert captured[0].action["values"][0] == 1  # type: ignore[index]
    with pytest.raises(TypeError):
        captured[0].observation["time"] = -1  # type: ignore[index]
    env.close()


def test_reset_failure_attempts_all_cleanup_and_preserves_causes() -> None:
    factory = FakeFactory()

    def broken_factory(context: EpisodeContext) -> FakeBinding:
        binding = FakeBinding(context)
        factory.bindings.append(binding)

        def fail_observe(events: object) -> object:
            del events
            raise RuntimeError("initial observation failed")

        def fail_terminate() -> None:
            raise RuntimeError("terminate failed")

        binding.observe = fail_observe  # type: ignore[method-assign]
        binding.executor.terminate_simulation = fail_terminate  # type: ignore[method-assign]
        return binding

    env = PyJevSimEnv(broken_factory)
    with pytest.raises(BaseExceptionGroup) as failure:
        env.reset()
    assert [str(error) for error in failure.value.exceptions] == [
        "initial observation failed",
        "terminate failed",
    ]
    assert factory.bindings[0].closed == 1
