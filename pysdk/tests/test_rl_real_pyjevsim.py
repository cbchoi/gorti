"""Real pyjevsim conformance test for the RL executor seam."""

from __future__ import annotations

from typing import Any

import pytest

from pyjevsim_bridge.rl import (
    FixedDeltaBoundary,
    FunctionalEpisodeBinding,
    PyJevSimEnv,
)

pyjevsim = pytest.importorskip("pyjevsim", reason="pyjevsim optional runtime dep")

from pyjevsim.behavior_model import BehaviorModel  # noqa: E402
from pyjevsim.definition import ExecutionType  # noqa: E402
from pyjevsim.system_executor import SysExecutor  # noqa: E402


class _ControlledCounter(BehaviorModel):
    def __init__(self) -> None:
        super().__init__("controlled-counter")
        self.insert_state("active", deadline=1.0)
        self.insert_input_port("action")
        self.insert_output_port("unused")
        self.init_state("active")
        self.value = 0
        self.confluent_calls = 0
        self.confluent_model_times: list[float] = []

    def output(self, deliverer: Any) -> None:
        del deliverer

    def int_trans(self) -> None:
        self.value += 1

    def ext_trans(self, port: str, message: Any) -> None:
        assert port == "action"
        self.value += sum(int(item) for item in message.retrieve())

    def con_trans(self, port_messages: Any) -> None:
        self.confluent_calls += 1
        self.confluent_model_times.append(float(self.global_time))
        super().con_trans(port_messages)


def test_real_executor_preserves_same_time_confluent_semantics() -> None:
    built: list[_ControlledCounter] = []

    def factory(context: object) -> FunctionalEpisodeBinding:
        del context
        model = _ControlledCounter()
        executor = SysExecutor(1.0, ex_mode=ExecutionType.HLA_TIME)
        executor.register_entity(model)
        executor.insert_input_port("action")
        executor.coupling_relation(None, "action", model, "action")
        executor.step(0.0)  # activate waiting entities and seed request times
        built.append(model)
        return FunctionalEpisodeBinding(
            executor=executor,
            apply_action_fn=lambda ex, action: ex.insert_external_event(
                "action", action, scheduled_time=1.0
            ),
            observe_fn=lambda _ex, _events: model.value,
            reward_fn=lambda step: float(step.observation - step.previous_observation),
        )

    env = PyJevSimEnv(factory, boundary=FixedDeltaBoundary(1.0))
    observation, info = env.reset(seed=1516)
    assert observation == 0
    assert info["logical_time"] == 0.0

    observation, reward, terminated, truncated, info = env.step(5)
    assert observation == 6  # default confluent order: internal (+1), external (+5)
    assert reward == 6.0
    assert (terminated, truncated) == (False, False)
    assert info["logical_time"] == 1.0
    assert built[0].confluent_calls == 1
    # Known upstream 2.1.2 deviation (RSK-RL-013): model-local time is
    # updated after transition callbacks. Adapters must use StepView time.
    assert built[0].confluent_model_times == [0.0]

    first_model = built[0]
    env.reset(seed=1516)
    assert built[1] is not first_model
    assert built[1].value == 0
    env.close()
