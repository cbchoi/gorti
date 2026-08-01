"""A small Gym-like environment over a freshly built pyjevsim executor."""

from __future__ import annotations

import math
from collections.abc import Mapping
from types import MappingProxyType

from pyjevsim_bridge.rl.contracts import (
    EnvironmentClosedError,
    EpisodeBinding,
    EpisodeContext,
    EpisodeFactory,
    EpisodeStateError,
    EpisodeStepError,
    StepView,
)
from pyjevsim_bridge.rl.executor import (
    BindingDecisionBoundary,
    DecisionBoundary,
    ExecutorDriver,
)


class PyJevSimEnv:
    """Dependency-free Gymnasium-shaped facade for one environment instance."""

    def __init__(
        self,
        factory: EpisodeFactory,
        *,
        instance_id: str = "env-0",
        boundary: DecisionBoundary | None = None,
        max_steps: int | None = None,
        run_id: str = "local",
    ) -> None:
        if not instance_id:
            raise ValueError("instance_id must be non-empty")
        if not run_id:
            raise ValueError("run_id must be non-empty")
        if max_steps is not None and max_steps <= 0:
            raise ValueError("max_steps must be positive when provided")
        self._factory = factory
        self._instance_id = instance_id
        self._boundary = boundary
        self._max_steps = max_steps
        self._run_id = run_id
        self._episode_number = 0
        self._binding: EpisodeBinding | None = None
        self._driver: ExecutorDriver | None = None
        self._observation: object = None
        self._seed: int | None = None
        self._step_id = 0
        self._done = False
        self._failed = False
        self._closed = False

    def reset(
        self,
        *,
        seed: int | None = None,
        options: Mapping[str, object] | None = None,
    ) -> tuple[object, dict[str, object]]:
        """Dispose the previous graph and build a fresh episode binding."""

        self._ensure_open()
        self._dispose_episode()
        self._episode_number += 1
        episode_id = f"{self._instance_id}:episode-{self._episode_number}"
        context = EpisodeContext(
            episode_id=episode_id,
            instance_id=self._instance_id,
            seed=seed,
            options=MappingProxyType(dict(options or {})),
        )

        binding: EpisodeBinding | None = None
        driver: ExecutorDriver | None = None
        try:
            binding = self._factory(context)
            driver = ExecutorDriver(binding.executor)
            observation = binding.observe(())
            logical_time = driver.logical_time
        except BaseException as original:
            errors: list[BaseException] = [original]
            if driver is not None:
                try:
                    driver.close()
                except BaseException as cleanup_error:
                    errors.append(cleanup_error)
            if binding is not None:
                try:
                    binding.close()
                except BaseException as cleanup_error:
                    errors.append(cleanup_error)
            if len(errors) == 1:
                raise
            raise BaseExceptionGroup(
                "episode reset failed and cleanup reported errors", errors
            ) from original

        self._binding = binding
        self._driver = driver
        self._observation = observation
        self._seed = seed
        self._step_id = 0
        self._done = False
        self._failed = False
        return observation, self._provenance(logical_time)

    def step(
        self,
        action: object,
    ) -> tuple[object, float, bool, bool, dict[str, object]]:
        """Apply one action and return the Gymnasium-ordered five-tuple."""

        self._ensure_open()
        if self._binding is None or self._driver is None:
            raise EpisodeStateError("reset must be called before step")
        if self._failed:
            raise EpisodeStateError("episode failed during step; reset required")
        if self._done:
            raise EpisodeStateError("step called after terminated or truncated; reset required")

        binding = self._binding
        driver = self._driver
        previous_observation = self._observation
        phase = "apply_action"
        completion_boundary = "pre-commit"
        failure_time = driver.logical_time
        try:
            binding.apply_action(action)
            phase = "resolve_boundary"
            boundary = self._boundary or BindingDecisionBoundary(
                binding.next_decision_time
            )
            phase = "executor_advance"
            committed = driver.advance(boundary)
            completion_boundary = "committed"
            failure_time = committed.logical_time
            phase = "observe"
            observation = binding.observe(committed.external_events)
            next_step_id = self._step_id + 1
            transition = StepView(
                episode_id=f"{self._instance_id}:episode-{self._episode_number}",
                instance_id=self._instance_id,
                step_id=next_step_id,
                previous_logical_time=committed.previous_time,
                logical_time=committed.logical_time,
                action=action,
                previous_observation=previous_observation,
                observation=observation,
                external_events=committed.external_events,
            )
            phase = "reward"
            reward = float(binding.reward(transition))
            if not math.isfinite(reward):
                raise EpisodeStateError(f"reward must be finite, got {reward!r}")
            phase = "termination"
            terminated = bool(binding.terminated(transition) or driver.is_terminated())
            truncated = self._max_steps is not None and next_step_id >= self._max_steps
            phase = "info"
            plugin_info = dict(binding.info(transition))
        except BaseException as exc:
            # Action injection and executor advancement are not generally
            # reversible. Fail closed so stale observation/step identity can
            # never be paired with an already-advanced model.
            self._failed = True
            raise EpisodeStepError(
                f"episode step failed during {phase}: {exc}",
                phase=phase,
                completion_boundary=completion_boundary,
                episode_id=f"{self._instance_id}:episode-{self._episode_number}",
                step_id=self._step_id + 1,
                logical_time=failure_time,
            ) from exc

        self._step_id = next_step_id
        self._observation = observation
        self._done = terminated or truncated
        info = plugin_info
        info.update(self._provenance(committed.logical_time))
        return observation, reward, terminated, truncated, info

    def close(self) -> None:
        """Close the active episode and make future operations invalid."""

        if self._closed:
            return
        self._dispose_episode()
        self._closed = True

    def _dispose_episode(self) -> None:
        binding, driver = self._binding, self._driver
        errors: list[BaseException] = []
        if driver is not None:
            try:
                driver.close()
            except BaseException as exc:
                errors.append(exc)
            else:
                self._driver = None
        if binding is not None:
            try:
                binding.close()
            except BaseException as exc:
                errors.append(exc)
            else:
                self._binding = None
        if errors:
            if len(errors) == 1:
                raise errors[0]
            raise BaseExceptionGroup("episode cleanup failed", errors)
        self._done = False
        self._failed = False

    def _ensure_open(self) -> None:
        if self._closed:
            raise EnvironmentClosedError("environment is closed")

    def _provenance(self, logical_time: float) -> dict[str, object]:
        return {
            "run_id": self._run_id,
            "episode_id": f"{self._instance_id}:episode-{self._episode_number}",
            "instance_id": self._instance_id,
            "step_id": self._step_id,
            "logical_time": logical_time,
            "seed": self._seed,
        }
