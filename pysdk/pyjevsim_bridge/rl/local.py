"""Deterministic, transport-neutral local rollout orchestration."""

from __future__ import annotations

import asyncio
import hashlib
import inspect
import math
from collections.abc import Awaitable, Callable, Mapping
from typing import Protocol, TypeVar, cast

from .records import ResetResult, TransitionRecord


class LocalEnvironment(Protocol):
    """The minimal environment surface consumed by :class:`LocalRolloutPool`."""

    def reset(
        self, *, seed: int | None = None, options: Mapping[str, object] | None = None
    ) -> tuple[object, Mapping[str, object]] | Awaitable[tuple[object, Mapping[str, object]]]: ...

    def step(
        self, action: object
    ) -> (
        tuple[object, float, bool, bool, Mapping[str, object]]
        | Awaitable[tuple[object, float, bool, bool, Mapping[str, object]]]
    ): ...

    def close(self) -> None | Awaitable[None]: ...


EnvironmentProvider = LocalEnvironment | Callable[[], LocalEnvironment]
T = TypeVar("T")


class LocalRolloutBatchError(RuntimeError):
    """One or more workers advanced inconsistently; reset is required."""

    def __init__(self, phase: str, errors: Mapping[str, BaseException]) -> None:
        self.phase = phase
        self.errors = dict(errors)
        details = ", ".join(
            f"{worker_id}: {type(error).__name__}: {error}"
            for worker_id, error in sorted(self.errors.items())
        )
        super().__init__(f"local rollout {phase} failed; reset required ({details})")


def derive_episode_seed(
    run_seed: int | None, worker_id: str, episode_number: int
) -> int | None:
    """Derive a stable 63-bit episode seed.

    Python's built-in ``hash`` is deliberately randomized per process, so the
    derivation uses a versioned, length-framed SHA-256 input.  The result is
    stable across processes, operating systems, Python versions and worker
    completion orders.  A missing run seed remains missing rather than
    silently inventing non-reproducible entropy.
    """

    if run_seed is None:
        return None
    if isinstance(run_seed, bool) or not isinstance(run_seed, int) or run_seed < 0:
        raise ValueError("run_seed must be a non-negative integer or None")
    if not isinstance(worker_id, str) or not worker_id:
        raise ValueError("worker_id must be a non-empty string")
    if (
        isinstance(episode_number, bool)
        or not isinstance(episode_number, int)
        or episode_number < 0
    ):
        raise ValueError("episode_number must be a non-negative integer")

    parts = (
        b"pyjevsim-rl-seed-v1",
        str(run_seed).encode("ascii"),
        worker_id.encode("utf-8"),
        str(episode_number).encode("ascii"),
    )
    digest = hashlib.sha256()
    for part in parts:
        digest.update(len(part).to_bytes(4, "big"))
        digest.update(part)
    return int.from_bytes(digest.digest()[:8], "big") & ((1 << 63) - 1)


def _idempotency_key(
    run_id: str,
    generation: int,
    worker_id: str,
    episode_id: str,
    step_id: int,
    policy_version: int,
) -> str:
    digest = hashlib.sha256()
    for value in (
        "pyjevsim-rl-transition-v1",
        run_id,
        str(generation),
        worker_id,
        episode_id,
        str(step_id),
        str(policy_version),
    ):
        encoded = value.encode("utf-8")
        digest.update(len(encoded).to_bytes(4, "big"))
        digest.update(encoded)
    return digest.hexdigest()


async def _invoke(method: Callable[..., T], /, *args: object, **kwargs: object) -> T:
    """Call sync model code off-loop while accepting native async test doubles."""

    if inspect.iscoroutinefunction(method):
        return cast(T, await method(*args, **kwargs))
    result = await asyncio.to_thread(method, *args, **kwargs)
    if inspect.isawaitable(result):
        return cast(T, await result)
    return result


def _materialize_environment(provider: EnvironmentProvider) -> LocalEnvironment:
    if all(hasattr(provider, name) for name in ("reset", "step", "close")):
        return cast(LocalEnvironment, provider)
    if not callable(provider):
        raise TypeError("each worker requires an environment or environment factory")
    environment = provider()
    if not all(hasattr(environment, name) for name in ("reset", "step", "close")):
        raise TypeError("environment factory must return reset/step/close methods")
    return environment


class LocalRolloutPool:
    """Run isolated environments concurrently and return canonical worker order.

    ``workers`` maps stable worker IDs to either an already-created environment
    or a zero-argument environment factory.  A distinct environment object is
    required for every worker.  Factories are materialized once; each
    ``PyJevSimEnv.reset`` is responsible for rebuilding its episode binding.
    """

    def __init__(
        self,
        workers: Mapping[str, EnvironmentProvider],
        *,
        run_id: str = "local",
        generation: int = 0,
        run_seed: int | None = None,
    ) -> None:
        if not isinstance(workers, Mapping) or not workers:
            raise ValueError("workers must be a non-empty mapping")
        if not isinstance(run_id, str) or not run_id:
            raise ValueError("run_id must be a non-empty string")
        if isinstance(generation, bool) or not isinstance(generation, int) or generation < 0:
            raise ValueError("generation must be a non-negative integer")
        if run_seed is not None and (
            isinstance(run_seed, bool) or not isinstance(run_seed, int) or run_seed < 0
        ):
            raise ValueError("run_seed must be a non-negative integer or None")

        worker_ids = tuple(sorted(workers))
        if any(not isinstance(worker_id, str) or not worker_id for worker_id in worker_ids):
            raise ValueError("worker IDs must be non-empty strings")
        environments = {
            worker_id: _materialize_environment(workers[worker_id])
            for worker_id in worker_ids
        }
        identities = [id(environment) for environment in environments.values()]
        if len(identities) != len(set(identities)):
            raise ValueError("each worker must own a distinct environment instance")

        self._worker_ids = worker_ids
        self._environments = environments
        self._run_id = run_id
        self._generation = generation
        self._run_seed = run_seed
        self._episode_numbers = dict.fromkeys(worker_ids, -1)
        self._episode_ids: dict[str, str] = {}
        self._step_ids: dict[str, int] = {}
        self._logical_times: dict[str, float] = {}
        self._observations: dict[str, object] = {}
        self._done_workers: set[str] = set()
        self._requires_reset = True
        self._closed = False

    @property
    def worker_ids(self) -> tuple[str, ...]:
        return self._worker_ids

    def _ensure_open(self) -> None:
        if self._closed:
            raise RuntimeError("local rollout pool is closed")

    async def reset(self, *, seed: int | None = None) -> list[ResetResult]:
        self._ensure_open()
        if seed is not None:
            if isinstance(seed, bool) or not isinstance(seed, int) or seed < 0:
                raise ValueError("seed must be a non-negative integer or None")
            self._run_seed = seed

        episode_numbers = {
            worker_id: self._episode_numbers[worker_id] + 1
            for worker_id in self._worker_ids
        }
        seeds = {
            worker_id: derive_episode_seed(
                self._run_seed, worker_id, episode_numbers[worker_id]
            )
            for worker_id in self._worker_ids
        }

        calls = [
            _invoke(self._environments[worker_id].reset, seed=seeds[worker_id])
            for worker_id in self._worker_ids
        ]
        raw_results = await asyncio.gather(*calls, return_exceptions=True)
        call_errors = {
            worker_id: result
            for worker_id, result in zip(self._worker_ids, raw_results, strict=True)
            if isinstance(result, BaseException)
        }
        if call_errors:
            self._requires_reset = True
            raise LocalRolloutBatchError("reset", call_errors)

        results: list[ResetResult] = []
        reset_logical_times: dict[str, float] = {}
        active_worker = "unknown"
        try:
            for worker_id, raw in zip(self._worker_ids, raw_results, strict=True):
                active_worker = worker_id
                if not isinstance(raw, tuple) or len(raw) != 2:
                    raise TypeError("environment reset must return (observation, info)")
                observation, raw_info = raw
                if not isinstance(raw_info, Mapping):
                    raise TypeError("environment reset info must be a mapping")
                info = dict(raw_info)
                raw_logical_time = info.get("logical_time")
                if isinstance(raw_logical_time, bool) or not isinstance(
                    raw_logical_time, (int, float)
                ):
                    raise TypeError(
                        f"worker {worker_id} reset info logical_time must be numeric"
                    )
                logical_time = float(raw_logical_time)
                if not math.isfinite(logical_time):
                    raise ValueError(
                        f"worker {worker_id} reset info logical_time must be finite"
                    )
                info["logical_time"] = logical_time
                reset_logical_times[worker_id] = logical_time
                episode_number = episode_numbers[worker_id]
                episode_id = info.get("episode_id")
                if episode_id is None:
                    episode_id = f"{self._run_id}:{worker_id}:{episode_number}"
                if not isinstance(episode_id, str) or not episode_id:
                    raise ValueError("environment reset info episode_id must be non-empty")
                expected = {
                    "run_id": self._run_id,
                    "instance_id": worker_id,
                    "step_id": 0,
                    "seed": seeds[worker_id],
                }
                for key, value in expected.items():
                    if key in info and info[key] != value:
                        raise ValueError(
                            f"worker {worker_id} reset info {key} conflicts with pool"
                        )
                    info[key] = value
                info["episode_id"] = episode_id
                results.append(
                    ResetResult(
                        run_id=self._run_id,
                        generation=self._generation,
                        worker_id=worker_id,
                        episode_id=episode_id,
                        seed=seeds[worker_id],
                        observation=observation,
                        info=info,
                    )
                )
        except BaseException as exc:
            self._requires_reset = True
            raise LocalRolloutBatchError(
                "reset-validation", {active_worker: exc}
            ) from exc

        for result in results:
            worker_id = result.worker_id
            self._episode_numbers[worker_id] = episode_numbers[worker_id]
            self._episode_ids[worker_id] = result.episode_id
            self._step_ids[worker_id] = 0
            self._logical_times[worker_id] = reset_logical_times[worker_id]
            self._observations[worker_id] = result.observation
        self._done_workers.clear()
        self._requires_reset = False
        return results

    async def step(
        self, actions: Mapping[str, object], *, policy_version: int
    ) -> list[TransitionRecord]:
        self._ensure_open()
        if not isinstance(actions, Mapping):
            raise TypeError("actions must be a mapping keyed by worker ID")
        if (
            isinstance(policy_version, bool)
            or not isinstance(policy_version, int)
            or policy_version < 0
        ):
            raise ValueError("policy_version must be a non-negative integer")

        expected = set(self._worker_ids)
        supplied = set(actions)
        missing = sorted(expected - supplied)
        extra = sorted(supplied - expected)
        if missing or extra:
            details = []
            if missing:
                details.append(f"missing actions: {missing}")
            if extra:
                details.append(f"extra actions: {extra}")
            raise ValueError("; ".join(details))
        not_reset = [
            worker_id
            for worker_id in self._worker_ids
            if worker_id not in self._episode_ids
        ]
        if not_reset:
            raise RuntimeError(f"workers must be reset before step: {not_reset}")
        if self._requires_reset:
            raise RuntimeError("local rollout pool requires reset before step")
        if self._done_workers:
            raise RuntimeError(
                f"terminal workers require reset before step: {sorted(self._done_workers)}"
            )

        calls = [
            _invoke(self._environments[worker_id].step, actions[worker_id])
            for worker_id in self._worker_ids
        ]
        raw_results = await asyncio.gather(*calls, return_exceptions=True)
        call_errors = {
            worker_id: result
            for worker_id, result in zip(self._worker_ids, raw_results, strict=True)
            if isinstance(result, BaseException)
        }
        if call_errors:
            self._requires_reset = True
            raise LocalRolloutBatchError("step", call_errors)

        results: list[TransitionRecord] = []
        active_worker = "unknown"
        try:
            for worker_id, raw in zip(self._worker_ids, raw_results, strict=True):
                active_worker = worker_id
                if not isinstance(raw, tuple) or len(raw) != 5:
                    raise TypeError(
                        "environment step must return "
                        "(observation, reward, terminated, truncated, info)"
                    )
                observation, reward, terminated, truncated, raw_info = raw
                if not isinstance(raw_info, Mapping):
                    raise TypeError("environment step info must be a mapping")
                next_step_id = self._step_ids[worker_id] + 1
                episode_id = self._episode_ids[worker_id]
                info = dict(raw_info)
                if "logical_time" not in info:
                    raise ValueError(
                        f"worker {worker_id} step info must contain logical_time"
                    )
                raw_logical_time = info["logical_time"]
                if isinstance(raw_logical_time, bool) or not isinstance(
                    raw_logical_time, (int, float)
                ):
                    raise TypeError(
                        f"worker {worker_id} step info logical_time must be numeric"
                    )
                logical_time = float(raw_logical_time)
                if not math.isfinite(logical_time):
                    raise ValueError(
                        f"worker {worker_id} step info logical_time must be finite"
                    )
                if logical_time < self._logical_times[worker_id]:
                    raise ValueError(
                        f"worker {worker_id} logical_time regressed from "
                        f"{self._logical_times[worker_id]} to {logical_time}"
                    )
                expected_info = {
                    "run_id": self._run_id,
                    "instance_id": worker_id,
                    "episode_id": episode_id,
                    "step_id": next_step_id,
                }
                for key, value in expected_info.items():
                    if key in info and info[key] != value:
                        raise ValueError(
                            f"worker {worker_id} step info {key} conflicts with pool"
                        )
                    info[key] = value
                results.append(
                    TransitionRecord(
                        run_id=self._run_id,
                        generation=self._generation,
                        worker_id=worker_id,
                        episode_id=episode_id,
                        step_id=next_step_id,
                        policy_version=policy_version,
                        idempotency_key=_idempotency_key(
                            self._run_id,
                            self._generation,
                            worker_id,
                            episode_id,
                            next_step_id,
                            policy_version,
                        ),
                        logical_time=logical_time,
                        previous_observation=self._observations[worker_id],
                        action=actions[worker_id],
                        next_observation=observation,
                        reward=float(reward),
                        terminated=terminated,
                        truncated=truncated,
                        info=info,
                    )
                )
        except BaseException as exc:
            self._requires_reset = True
            raise LocalRolloutBatchError(
                "step-validation", {active_worker: exc}
            ) from exc

        for record in results:
            worker_id = record.worker_id
            self._step_ids[worker_id] = record.step_id
            self._logical_times[worker_id] = record.logical_time
            self._observations[worker_id] = record.next_observation
            if record.terminated or record.truncated:
                self._done_workers.add(worker_id)
        return results

    async def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        results = await asyncio.gather(
            *(
                _invoke(self._environments[worker_id].close)
                for worker_id in self._worker_ids
            ),
            return_exceptions=True,
        )
        errors = [result for result in results if isinstance(result, BaseException)]
        if errors:
            raise BaseExceptionGroup(
                "one or more local environments failed to close", errors
            )
