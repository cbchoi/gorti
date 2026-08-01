"""Local pyjevsim RL rollout contract tests."""

from __future__ import annotations

import asyncio
from dataclasses import FrozenInstanceError

import pytest

from pyjevsim_bridge.rl.local import (
    LocalRolloutBatchError,
    LocalRolloutPool,
    derive_episode_seed,
)
from pyjevsim_bridge.rl.records import ActionCommand, TransitionRecord


class _Environment:
    def __init__(self, name: str, delay: float = 0.0) -> None:
        self.name = name
        self.delay = delay
        self.reset_seeds: list[int | None] = []
        self.actions: list[object] = []
        self.closed = 0
        self.episode = 0

    async def reset(self, *, seed: int | None = None):  # type: ignore[no-untyped-def]
        await asyncio.sleep(self.delay)
        self.reset_seeds.append(seed)
        self.episode += 1
        return f"{self.name}-initial", {
            "episode_id": f"{self.name}-episode-{self.episode}",
            "logical_time": 0.0,
        }

    async def step(self, action: object):  # type: ignore[no-untyped-def]
        await asyncio.sleep(self.delay)
        self.actions.append(action)
        return (
            f"{self.name}-observation-{len(self.actions)}",
            float(len(self.actions)),
            False,
            False,
            {"logical_time": float(len(self.actions))},
        )

    async def close(self) -> None:
        self.closed += 1


def test_stable_seed_derivation_is_worker_and_episode_specific() -> None:
    first = derive_episode_seed(1516, "actor-a", 0)
    assert first == derive_episode_seed(1516, "actor-a", 0)
    assert first != derive_episode_seed(1516, "actor-b", 0)
    assert first != derive_episode_seed(1516, "actor-a", 1)
    assert derive_episode_seed(None, "actor-a", 0) is None


@pytest.mark.asyncio
async def test_results_use_worker_id_order_not_completion_order() -> None:
    slow = _Environment("slow", delay=0.02)
    fast = _Environment("fast")
    pool = LocalRolloutPool(
        {"worker-z": fast, "worker-a": slow}, run_id="run-1", generation=4
    )

    resets = await pool.reset(seed=42)
    assert [result.worker_id for result in resets] == ["worker-a", "worker-z"]

    transitions = await pool.step(
        {"worker-z": "right", "worker-a": "left"}, policy_version=7
    )
    assert [record.worker_id for record in transitions] == ["worker-a", "worker-z"]
    assert transitions[0].previous_observation == "slow-initial"
    assert transitions[0].next_observation == "slow-observation-1"
    assert transitions[0].step_id == 1
    assert transitions[0].policy_version == 7
    assert transitions[0].generation == 4
    assert transitions[0].idempotency_key

    await pool.close()
    await pool.close()
    assert slow.closed == fast.closed == 1


@pytest.mark.asyncio
async def test_missing_or_extra_actions_fail_before_any_worker_advances() -> None:
    first = _Environment("first")
    second = _Environment("second")
    pool = LocalRolloutPool({"first": first, "second": second})
    await pool.reset(seed=3)

    with pytest.raises(ValueError, match="missing actions"):
        await pool.step({"first": 1}, policy_version=0)
    with pytest.raises(ValueError, match="extra actions"):
        await pool.step(
            {"first": 1, "second": 2, "unknown": 3}, policy_version=0
        )

    assert first.actions == []
    assert second.actions == []
    await pool.close()


def test_environment_instances_must_be_isolated() -> None:
    shared = _Environment("shared")
    with pytest.raises(ValueError, match="distinct environment"):
        LocalRolloutPool({"first": shared, "second": shared})


@pytest.mark.asyncio
async def test_each_reset_advances_stable_episode_seed() -> None:
    environment = _Environment("only")
    pool = LocalRolloutPool({"worker": environment}, run_seed=99)

    first = await pool.reset()
    second = await pool.reset()
    assert first[0].seed == derive_episode_seed(99, "worker", 0)
    assert second[0].seed == derive_episode_seed(99, "worker", 1)
    assert first[0].seed != second[0].seed
    await pool.close()


def test_action_record_is_immutable_and_validated() -> None:
    command = ActionCommand(
        run_id="run",
        generation=0,
        worker_id="worker",
        episode_id="episode",
        step_id=0,
        policy_version=1,
        idempotency_key="key",
        logical_time=0.0,
        payload={"move": 1},
    )
    assert command.action == {"move": 1}
    with pytest.raises(FrozenInstanceError):
        command.step_id = 2  # type: ignore[misc]


@pytest.mark.asyncio
async def test_partial_worker_failure_requires_full_batch_reset() -> None:
    healthy = _Environment("healthy")
    failing = _Environment("failing")
    pool = LocalRolloutPool({"healthy": healthy, "failing": failing})
    await pool.reset()

    async def fail_step(action: object):  # type: ignore[no-untyped-def]
        del action
        raise RuntimeError("model failed")

    failing.step = fail_step  # type: ignore[method-assign]
    with pytest.raises(LocalRolloutBatchError, match="reset required"):
        await pool.step({"healthy": 1, "failing": 1}, policy_version=0)
    with pytest.raises(RuntimeError, match="requires reset"):
        await pool.step({"healthy": 2, "failing": 2}, policy_version=0)
    await pool.close()


def test_transition_deep_freezes_nested_state_and_thaws_for_transport() -> None:
    source = {"values": [{"value": 1}]}
    record = TransitionRecord(
        run_id="run",
        generation=0,
        worker_id="worker",
        episode_id="episode",
        step_id=1,
        policy_version=0,
        idempotency_key="key",
        logical_time=1.0,
        previous_observation=source,
        action={"move": [1]},
        next_observation={"values": [2]},
        reward=1.0,
        terminated=False,
        truncated=False,
    )
    source["values"][0]["value"] = 99

    assert record.previous_observation["values"][0]["value"] == 1  # type: ignore[index]
    assert record.to_dict()["payload"]["previous_observation"] == {  # type: ignore[index]
        "values": [{"value": 1}]
    }


@pytest.mark.asyncio
async def test_worker_logical_time_must_not_regress() -> None:
    environment = _Environment("worker")
    pool = LocalRolloutPool({"worker": environment})
    await pool.reset()
    await pool.step({"worker": 1}, policy_version=0)

    async def regressing_step(action: object):  # type: ignore[no-untyped-def]
        del action
        return "observation", 0.0, False, False, {"logical_time": 0.5}

    environment.step = regressing_step  # type: ignore[method-assign]
    with pytest.raises(LocalRolloutBatchError, match="regressed"):
        await pool.step({"worker": 2}, policy_version=0)
    with pytest.raises(RuntimeError, match="requires reset"):
        await pool.step({"worker": 3}, policy_version=0)
    await pool.close()
