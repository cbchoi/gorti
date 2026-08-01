"""Dependency-free contracts for the pyjevsim reinforcement-learning bridge.

The real :mod:`pyjevsim` package is deliberately not imported here.  Runtime
objects are accepted structurally, which keeps model contract tests usable in
environments where the optional pyjevsim dependency is not installed.
"""

from __future__ import annotations

import copy
from collections.abc import Mapping
from dataclasses import dataclass
from types import MappingProxyType
from typing import Protocol, runtime_checkable


def _snapshot(value: object) -> object:
    """Deeply snapshot common record values before plugin callbacks see them."""

    if isinstance(value, Mapping):
        return MappingProxyType(
            {str(key): _snapshot(item) for key, item in value.items()}
        )
    if isinstance(value, (list, tuple)):
        return tuple(_snapshot(item) for item in value)
    if isinstance(value, (set, frozenset)):
        return frozenset(_snapshot(item) for item in value)
    return copy.deepcopy(value)


@runtime_checkable
class ExecutorProtocol(Protocol):
    """Canonical subset of ``pyjevsim.SysExecutor`` used by the RL bridge."""

    def get_global_time(self) -> float:
        """Return the last committed simulation time."""
        ...

    def get_next_event_time(self) -> float:
        """Return the next scheduled simulation-event time."""
        ...

    def insert_external_event(
        self,
        port: str,
        payload: object,
        scheduled_time: float = 0,
    ) -> None:
        """Schedule an external event for a model-facing executor port."""
        ...

    def step(self, granted_time: float) -> object:
        """Commit every event at or before ``granted_time`` and return outputs."""
        ...

    def is_terminated(self) -> bool:
        """Report whether the simulation has reached its domain terminal state."""
        ...

    def terminate_simulation(self) -> None:
        """Release executor resources."""
        ...


@dataclass(frozen=True)
class EpisodeContext:
    """Immutable inputs supplied to the model factory for one reset."""

    episode_id: str
    instance_id: str
    seed: int | None
    options: Mapping[str, object]


@dataclass(frozen=True)
class StepView:
    """Immutable committed simulation view passed to reward and info bindings."""

    episode_id: str
    instance_id: str
    step_id: int
    previous_logical_time: float
    logical_time: float
    action: object
    previous_observation: object
    observation: object
    external_events: object

    def __post_init__(self) -> None:
        object.__setattr__(self, "action", _snapshot(self.action))
        object.__setattr__(
            self, "previous_observation", _snapshot(self.previous_observation)
        )
        object.__setattr__(self, "observation", _snapshot(self.observation))
        object.__setattr__(self, "external_events", _snapshot(self.external_events))


@runtime_checkable
class EpisodeBinding(Protocol):
    """Connect one freshly built executor graph to the generic environment."""

    executor: ExecutorProtocol

    def apply_action(self, action: object) -> None:
        """Validate and translate an action into executor external events."""
        ...

    def next_decision_time(self) -> float:
        """Return a plugin-selected decision boundary when requested."""
        ...

    def observe(self, external_events: object) -> object:
        """Construct an observation from committed executor outputs."""
        ...

    def reward(self, transition: StepView) -> float:
        """Compute the reward for a committed step."""
        ...

    def terminated(self, transition: StepView) -> bool:
        """Evaluate the model-domain terminal condition."""
        ...

    def info(self, transition: StepView) -> Mapping[str, object]:
        """Return model-specific, non-authoritative diagnostic information."""
        ...

    def close(self) -> None:
        """Release binding-owned resources."""
        ...


class EpisodeFactory(Protocol):
    """Build a new binding, executor, and model graph for every episode."""

    def __call__(self, context: EpisodeContext) -> EpisodeBinding:
        """Create a fresh episode binding."""
        ...


class RLEnvironmentError(RuntimeError):
    """Base class for deterministic environment lifecycle failures."""


class EnvironmentClosedError(RLEnvironmentError):
    """An operation was attempted after the environment was closed."""


class EpisodeStateError(RLEnvironmentError):
    """An operation is invalid for the current episode lifecycle state."""


class EpisodeStepError(RLEnvironmentError):
    """A step failed with an explicit simulation completion boundary."""

    def __init__(
        self,
        message: str,
        *,
        phase: str,
        completion_boundary: str,
        episode_id: str,
        step_id: int,
        logical_time: float,
    ) -> None:
        self.phase = phase
        self.completion_boundary = completion_boundary
        self.episode_id = episode_id
        self.step_id = step_id
        self.logical_time = logical_time
        self.recoverable = False
        super().__init__(message)


class ExecutorContractError(RLEnvironmentError):
    """An executor violated the monotonic-time or finite-time contract."""
