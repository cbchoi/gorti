"""Contract tests for the dependency-free RL executor driver."""

from __future__ import annotations

import math

import pytest

from pyjevsim_bridge.rl.contracts import ExecutorContractError
from pyjevsim_bridge.rl.executor import (
    ExecutorDriver,
    FixedDeltaBoundary,
    NextEventBoundary,
)


class FakeExecutor:
    def __init__(self, *, now: float = 0.0, next_event: float = 2.0) -> None:
        self.now = now
        self.next_event = next_event
        self.targets: list[float] = []
        self.terminate_calls = 0
        self.terminated = False

    def get_global_time(self) -> float:
        return self.now

    def get_next_event_time(self) -> float:
        return self.next_event

    def insert_external_event(
        self, port: str, payload: object, scheduled_time: float = 0
    ) -> None:
        del port, payload, scheduled_time

    def step(self, granted_time: float) -> object:
        self.targets.append(granted_time)
        self.now = granted_time
        return ("event", granted_time)

    def is_terminated(self) -> bool:
        return self.terminated

    def terminate_simulation(self) -> None:
        self.terminate_calls += 1


def test_fixed_boundary_advances_from_committed_global_time() -> None:
    executor = FakeExecutor(now=3.0)
    result = ExecutorDriver(executor).advance(FixedDeltaBoundary(0.5))
    assert result.previous_time == 3.0
    assert result.logical_time == 3.5
    assert result.external_events == ("event", 3.5)
    assert executor.targets == [3.5]


def test_next_event_boundary_uses_executor_schedule() -> None:
    executor = FakeExecutor(now=1.0, next_event=7.25)
    result = ExecutorDriver(executor).advance(NextEventBoundary())
    assert result.logical_time == 7.25


def test_boundary_rejects_time_regression_before_executor_step() -> None:
    executor = FakeExecutor(now=4.0, next_event=3.0)
    with pytest.raises(ExecutorContractError, match="regressed"):
        ExecutorDriver(executor).advance(NextEventBoundary())
    assert executor.targets == []


@pytest.mark.parametrize("delta", [0.0, -1.0, math.inf, math.nan])
def test_fixed_boundary_requires_positive_finite_delta(delta: float) -> None:
    with pytest.raises(ValueError, match="finite and positive"):
        FixedDeltaBoundary(delta)


def test_close_is_idempotent_and_prevents_advance() -> None:
    executor = FakeExecutor()
    driver = ExecutorDriver(executor)
    driver.close()
    driver.close()
    assert executor.terminate_calls == 1
    with pytest.raises(ExecutorContractError, match="closed"):
        driver.advance(FixedDeltaBoundary(1.0))


def test_non_finite_executor_time_is_rejected() -> None:
    executor = FakeExecutor(now=math.inf)
    with pytest.raises(ExecutorContractError, match="global time must be finite"):
        ExecutorDriver(executor).advance(FixedDeltaBoundary(1.0))


def test_executor_must_commit_the_requested_decision_time() -> None:
    executor = FakeExecutor()

    def under_advance(granted_time: float) -> object:
        executor.now = granted_time - 0.5
        return ()

    executor.step = under_advance  # type: ignore[method-assign]
    with pytest.raises(ExecutorContractError, match="stopped before"):
        ExecutorDriver(executor).advance(FixedDeltaBoundary(1.0))


def test_executor_rejects_even_sub_picosecond_boundary_mismatch() -> None:
    executor = FakeExecutor()

    def under_advance(granted_time: float) -> object:
        executor.now = granted_time - 5e-13
        return ()

    executor.step = under_advance  # type: ignore[method-assign]
    with pytest.raises(ExecutorContractError, match="stopped before"):
        ExecutorDriver(executor).advance(FixedDeltaBoundary(1.0))


def test_failed_executor_close_can_be_retried() -> None:
    executor = FakeExecutor()
    attempts = 0

    def flaky_terminate() -> None:
        nonlocal attempts
        attempts += 1
        if attempts == 1:
            raise RuntimeError("terminate failed")

    executor.terminate_simulation = flaky_terminate  # type: ignore[method-assign]
    driver = ExecutorDriver(executor)
    with pytest.raises(RuntimeError, match="terminate failed"):
        driver.close()
    driver.close()
    assert attempts == 2
