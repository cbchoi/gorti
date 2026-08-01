"""Decision-boundary control for duck-typed pyjevsim ``SysExecutor`` objects."""

from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Protocol

from pyjevsim_bridge.rl.contracts import ExecutorContractError, ExecutorProtocol


class DecisionBoundary(Protocol):
    """Resolve the logical time to which an executor should advance."""

    def resolve(self, executor: ExecutorProtocol) -> float:
        """Return a finite target time no earlier than the executor clock."""
        ...


@dataclass(frozen=True)
class FixedDeltaBoundary:
    """Advance by the same positive simulation-time interval on every step."""

    delta: float

    def __post_init__(self) -> None:
        if not math.isfinite(self.delta) or self.delta <= 0:
            raise ValueError("fixed decision delta must be finite and positive")

    def resolve(self, executor: ExecutorProtocol) -> float:
        return float(executor.get_global_time()) + self.delta


@dataclass(frozen=True)
class NextEventBoundary:
    """Advance through the next scheduled pyjevsim event time."""

    def resolve(self, executor: ExecutorProtocol) -> float:
        return float(executor.get_next_event_time())


@dataclass(frozen=True)
class BindingDecisionBoundary:
    """Use the decision time supplied by an episode binding callback."""

    decision_time: object

    def resolve(self, executor: ExecutorProtocol) -> float:
        del executor
        callback = self.decision_time
        if not callable(callback):
            raise TypeError("decision_time must be callable")
        return float(callback())


@dataclass(frozen=True)
class ExecutorStep:
    """Outputs and time interval committed by one executor advancement."""

    previous_time: float
    logical_time: float
    external_events: object


class ExecutorDriver:
    """Validate monotonic time around the canonical executor ``step`` seam."""

    def __init__(self, executor: ExecutorProtocol) -> None:
        self._executor = executor
        self._closed = False

    @property
    def executor(self) -> ExecutorProtocol:
        return self._executor

    @property
    def logical_time(self) -> float:
        return self._finite_time(self._executor.get_global_time(), "global time")

    def advance(self, boundary: DecisionBoundary) -> ExecutorStep:
        if self._closed:
            raise ExecutorContractError("executor driver is closed")

        previous = self.logical_time
        target = self._finite_time(boundary.resolve(self._executor), "decision time")
        if target < previous:
            raise ExecutorContractError(
                f"decision time regressed from {previous!r} to {target!r}"
            )

        external_events = self._executor.step(target)
        committed = self.logical_time
        if committed < previous:
            raise ExecutorContractError(
                f"executor clock regressed from {previous!r} to {committed!r}"
            )
        if committed > target:
            raise ExecutorContractError(
                f"executor advanced past decision time {target!r} to {committed!r}"
            )
        if committed != target:
            raise ExecutorContractError(
                f"executor stopped before decision time {target!r} at {committed!r}"
            )
        return ExecutorStep(previous, committed, external_events)

    def is_terminated(self) -> bool:
        return bool(self._executor.is_terminated())

    def close(self) -> None:
        if self._closed:
            return
        self._executor.terminate_simulation()
        self._closed = True

    @staticmethod
    def _finite_time(value: float, label: str) -> float:
        result = float(value)
        if not math.isfinite(result):
            raise ExecutorContractError(f"{label} must be finite, got {result!r}")
        return result
