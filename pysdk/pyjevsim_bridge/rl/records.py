"""Immutable records shared by local and federated RL rollout paths.

The records intentionally depend only on the Python standard library.  They
are transport-neutral: a gorti codec may encode them later without making the
local rollout implementation depend on the RTI SDK.
"""

from __future__ import annotations

import copy
import math
from collections.abc import Mapping
from dataclasses import dataclass, field
from types import MappingProxyType
from typing import cast

SCHEMA_VERSION = 1


def _require_non_empty(name: str, value: str) -> None:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{name} must be a non-empty string")


def _require_non_negative_int(name: str, value: int) -> None:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise ValueError(f"{name} must be a non-negative integer")


def _require_finite(name: str, value: float) -> None:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError(f"{name} must be a finite number")
    if not math.isfinite(float(value)):
        raise ValueError(f"{name} must be a finite number")


def _freeze(value: object) -> object:
    if isinstance(value, Mapping):
        return MappingProxyType({str(key): _freeze(item) for key, item in value.items()})
    if isinstance(value, (list, tuple)):
        return tuple(_freeze(item) for item in value)
    if isinstance(value, (set, frozenset)):
        return frozenset(_freeze(item) for item in value)
    return copy.deepcopy(value)


def _thaw(value: object) -> object:
    if isinstance(value, Mapping):
        return {str(key): _thaw(item) for key, item in value.items()}
    if isinstance(value, tuple):
        return [_thaw(item) for item in value]
    if isinstance(value, frozenset):
        return sorted((_thaw(item) for item in value), key=repr)
    return copy.deepcopy(value)


def _frozen_info(info: Mapping[str, object]) -> Mapping[str, object]:
    if not isinstance(info, Mapping):
        raise TypeError("info must be a mapping")
    frozen = _freeze(info)
    return cast(Mapping[str, object], frozen)


@dataclass(frozen=True, slots=True)
class ActionCommand:
    """A policy action addressed to exactly one environment episode."""

    run_id: str
    generation: int
    worker_id: str
    episode_id: str
    step_id: int
    policy_version: int
    idempotency_key: str
    logical_time: float
    payload: object
    schema_version: int = SCHEMA_VERSION

    def __post_init__(self) -> None:
        _require_non_empty("run_id", self.run_id)
        _require_non_negative_int("generation", self.generation)
        _require_non_empty("worker_id", self.worker_id)
        _require_non_empty("episode_id", self.episode_id)
        _require_non_negative_int("step_id", self.step_id)
        _require_non_negative_int("policy_version", self.policy_version)
        _require_non_empty("idempotency_key", self.idempotency_key)
        _require_finite("logical_time", self.logical_time)
        if self.schema_version != SCHEMA_VERSION:
            raise ValueError(f"unsupported schema_version: {self.schema_version}")
        object.__setattr__(self, "payload", _freeze(self.payload))

    @property
    def action(self) -> object:
        """Local-friendly alias for the transport envelope payload."""

        return self.payload

    def to_dict(self) -> dict[str, object]:
        """Return the version-1 federation envelope shape."""
        return {
            "schema_version": self.schema_version,
            "run_id": self.run_id,
            "generation": self.generation,
            "worker_id": self.worker_id,
            "episode_id": self.episode_id,
            "step_id": self.step_id,
            "policy_version": self.policy_version,
            "idempotency_key": self.idempotency_key,
            "logical_time": self.logical_time,
            "payload": _thaw(self.payload),
        }


@dataclass(frozen=True, slots=True)
class ResetResult:
    """Initial observation and provenance returned for a worker reset."""

    run_id: str
    generation: int
    worker_id: str
    episode_id: str
    seed: int | None
    observation: object
    info: Mapping[str, object] = field(default_factory=dict)
    schema_version: int = SCHEMA_VERSION

    def __post_init__(self) -> None:
        _require_non_empty("run_id", self.run_id)
        _require_non_negative_int("generation", self.generation)
        _require_non_empty("worker_id", self.worker_id)
        _require_non_empty("episode_id", self.episode_id)
        if self.seed is not None:
            _require_non_negative_int("seed", self.seed)
        if self.schema_version != SCHEMA_VERSION:
            raise ValueError(f"unsupported schema_version: {self.schema_version}")
        object.__setattr__(self, "observation", _freeze(self.observation))
        object.__setattr__(self, "info", _frozen_info(self.info))

    def to_dict(self) -> dict[str, object]:
        """Return a JSON-friendly local reset record."""
        return {
            "schema_version": self.schema_version,
            "run_id": self.run_id,
            "generation": self.generation,
            "worker_id": self.worker_id,
            "episode_id": self.episode_id,
            "seed": self.seed,
            "observation": _thaw(self.observation),
            "info": _thaw(self.info),
        }


@dataclass(frozen=True, slots=True)
class TransitionRecord:
    """A committed environment transition with complete learning provenance."""

    run_id: str
    generation: int
    worker_id: str
    episode_id: str
    step_id: int
    policy_version: int
    idempotency_key: str
    logical_time: float
    previous_observation: object
    action: object
    next_observation: object
    reward: float
    terminated: bool
    truncated: bool
    info: Mapping[str, object] = field(default_factory=dict)
    schema_version: int = SCHEMA_VERSION

    def __post_init__(self) -> None:
        _require_non_empty("run_id", self.run_id)
        _require_non_negative_int("generation", self.generation)
        _require_non_empty("worker_id", self.worker_id)
        _require_non_empty("episode_id", self.episode_id)
        _require_non_negative_int("step_id", self.step_id)
        _require_non_negative_int("policy_version", self.policy_version)
        _require_non_empty("idempotency_key", self.idempotency_key)
        _require_finite("logical_time", self.logical_time)
        _require_finite("reward", self.reward)
        if not isinstance(self.terminated, bool):
            raise TypeError("terminated must be bool")
        if not isinstance(self.truncated, bool):
            raise TypeError("truncated must be bool")
        if self.schema_version != SCHEMA_VERSION:
            raise ValueError(f"unsupported schema_version: {self.schema_version}")
        object.__setattr__(
            self, "previous_observation", _freeze(self.previous_observation)
        )
        object.__setattr__(self, "action", _freeze(self.action))
        object.__setattr__(self, "next_observation", _freeze(self.next_observation))
        object.__setattr__(self, "info", _frozen_info(self.info))

    @property
    def observation(self) -> object:
        """Gym-compatible alias for the transition's next observation."""

        return self.next_observation

    def to_dict(self) -> dict[str, object]:
        """Return the version-1 envelope used by ``GortiRolloutChannel``."""
        return {
            "schema_version": self.schema_version,
            "run_id": self.run_id,
            "generation": self.generation,
            "worker_id": self.worker_id,
            "episode_id": self.episode_id,
            "step_id": self.step_id,
            "policy_version": self.policy_version,
            "idempotency_key": self.idempotency_key,
            "logical_time": self.logical_time,
            "payload": {
                "previous_observation": _thaw(self.previous_observation),
                "action": _thaw(self.action),
                "observation": _thaw(self.next_observation),
                "reward": self.reward,
                "terminated": self.terminated,
                "truncated": self.truncated,
                "info": _thaw(self.info),
            },
        }
