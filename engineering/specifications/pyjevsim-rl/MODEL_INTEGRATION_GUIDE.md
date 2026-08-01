# pyjevsim RL Model Integration Guide

Document ID: `GUIDE-PYJEVSIM-RL`
Version: 0.1-draft
Updated: 2026-07-19

## 1. Supported integration seam

Model authors keep their normal pyjevsim `BehaviorModel` and
`StructuralModel` graph. They provide an episode factory that creates a fresh
`SysExecutor` in `HLA_TIME` mode on every reset and returns a
`FunctionalEpisodeBinding`. The binding defines only model-specific semantics:

- how an action is inserted through an executor-facing input port;
- how observation is derived from model state and emitted events;
- how reward and termination are derived from a committed transition;
- optionally, how the next decision time and diagnostic info are produced.

The framework owns episode state, step numbering, deterministic seed
derivation, executor stepping, transition records, local worker ordering, and
gorti transport. A model must not call atomic transition functions directly;
all event semantics remain under `SysExecutor` control.

## 2. Minimal adapter

```python
from pyjevsim.definition import ExecutionType
from pyjevsim.system_executor import SysExecutor
from pyjevsim_bridge.rl import (
    FixedDeltaBoundary,
    FunctionalEpisodeBinding,
    PyJevSimEnv,
)

def build_episode(context):
    model = build_my_model(seed=context.seed)
    executor = SysExecutor(1.0, ex_mode=ExecutionType.HLA_TIME)
    executor.register_entity(model)
    wire_model_ports(executor, model)
    executor.step(0.0)
    return FunctionalEpisodeBinding(
        executor=executor,
        apply_action_fn=lambda ex, action: ex.insert_external_event(
            "action", action, scheduled_time=ex.get_global_time() + 1.0
        ),
        observe_fn=lambda _ex, events: observe(model, events),
        reward_fn=lambda transition: reward(transition),
        terminated_fn=lambda transition: is_terminal(transition),
    )

environment = PyJevSimEnv(
    build_episode,
    boundary=FixedDeltaBoundary(1.0),
)
```

The same environment may be assigned to a `LocalRolloutPool`. Its
`TransitionRecord` can be sent unchanged through `GortiRolloutChannel`; the
channel preserves the model simulation time in `payload.simulation_time` while
selecting a lookahead-safe HLA delivery timestamp.

## 3. Model conformance checklist

1. A reset factory creates a new model graph and executor; state is never
   shared across workers or episodes.
2. The executor exposes `step`, `get_global_time`, and
   `get_next_event_time`, and is bootstrapped before the first observation.
3. Actions enter only through executor-facing ports with an explicit
   simulation timestamp.
4. Observation, reward, termination, and info functions are deterministic for
   the same admitted inputs and seed.
5. Observation/action/info values are canonical-JSON compatible when the gorti
   transport is used, or are converted to an external immutable artifact
   reference.
6. Step info contains finite `logical_time`; reserved provenance keys do not
   conflict with framework values.
7. A same-time internal/external event test proves the intended confluent
   semantics on the pinned pyjevsim version.
8. Repeated reset, terminal-state, close, invalid action, and model-failure
   tests pass before federation qualification.

For the pinned upstream 2.1.2 revision, a model must not read its own
`global_time` from inside `output`, `int_trans`, `ext_trans`, or `con_trans`;
upstream updates the model-local request time after those callbacks. Use the
executor committed time exposed through `StepView.logical_time`. Time-aware
models that cannot follow this restriction are not qualified until an upstream
fix passes the real-executor conformance gate (`RSK-RL-013`).

## 4. Local-to-federation promotion

Promotion does not change model code. Replace the local rollout supervisor
with actor federates and connect canonical records through the RL FOM. A run
manifest fixes the generation, FOM digest, model/package digest, policy
version, seed tree, logical-time mode, lookahead, and artifact endpoints.

The current executable slice provides in-process local concurrency and the
gorti TSO/synchronization transport contract. Process supervision, plugin
discovery/package trust, DDM sharding, artifact storage, checkpoint recovery,
and production multi-node control are the later gates listed in
`MILESTONES.md`; they are not implied by successful MVP conformance.
