# Interface Design Description: pyjevsim RL Framework

Document ID: `PYJEVSIM-RL-IDD`  
Version: 0.1-draft  
Status: Design baseline candidate  
Updated: 2026-07-19

## 1. Scope and compatibility policy

This document defines the public Python and gorti federation contracts for the
companion SRS. The core contract has no mandatory dependency on a particular RL
library. A Gymnasium adapter may add library-specific spaces without changing
the model or transport contracts. Breaking changes require a major interface
version and a migration note.

`SysExecutor` is the canonical pyjevsim seam. The legacy
`CoupledModelProtocol` remains a compatibility interface, not the normative RL
execution path.

## 2. Python interfaces

### IF-RL-001: executor protocol

```python
class ExecutorProtocol(Protocol):
    def get_global_time(self) -> float: ...
    def get_next_event_time(self) -> float: ...
    def insert_external_event(
        self, port: str, payload: object, scheduled_time: float = 0
    ) -> None: ...
    def step(self, granted_time: float) -> object: ...
    def is_terminated(self) -> bool: ...
    def terminate_simulation(self) -> None: ...
```

`step(t)` completes only after all pyjevsim events at or before `t`, including
same-time confluent transitions and zero-time cascades, have completed. The
returned external events are an immutable step input to downstream adapters.
Implementations shall reject time regression and any committed time that is
not exactly the declared decision boundary. A future non-binary time domain
must declare its quantization in the run manifest rather than relying on an
implicit floating-point tolerance.

Traces: `RL-FR-EXEC-001..006`, `DS-RL-003`, `TEST-RL-EXEC-001..003`.

### IF-RL-002: episode factory and binding

```python
@dataclass(frozen=True)
class EpisodeContext:
    episode_id: str
    instance_id: str
    seed: int | None
    options: Mapping[str, object]

class EpisodeFactory(Protocol):
    def __call__(self, context: EpisodeContext) -> "EpisodeBinding": ...

class EpisodeBinding(Protocol):
    executor: ExecutorProtocol
    def apply_action(self, action: object) -> None: ...
    def next_decision_time(self) -> float: ...
    def observe(self, external_events: object) -> object: ...
    def reward(self, transition: "StepView") -> float: ...
    def terminated(self, transition: "StepView") -> bool: ...
    def info(self, transition: "StepView") -> Mapping[str, object]: ...
    def close(self) -> None: ...
```

Every reset invokes the factory and obtains a new model graph and executor.
In-place reuse is not part of version 1. Snapshot restore may be added only for
a profile with a parity test against rebuild reset.

Traces: `RL-FR-MDL-*`, `RL-FR-GYM-002`, `DS-RL-001..007`.

### IF-RL-003: environment

```python
class PyJevSimEnv:
    def reset(
        self, *, seed: int | None = None,
        options: Mapping[str, object] | None = None,
    ) -> tuple[object, dict[str, object]]: ...

    def step(
        self, action: object,
    ) -> tuple[object, float, bool, bool, dict[str, object]]: ...

    def close(self) -> None: ...
```

The return tuple follows Gymnasium ordering. Model termination sets
`terminated`; framework time limits, transport loss, invalid numeric state, and
operator cancellation set `truncated`. `step` after either flag is an error
until reset. `info` includes episode, instance, step, logical time, and seed.

Traces: `RL-FR-GYM-001..007`, `DS-RL-007`, `TEST-RL-GYM-001`.

### IF-RL-004: local rollout pool

```python
class LocalRolloutPool:
    async def reset(self, *, seed: int | None = None) -> list[ResetResult]: ...
    async def step(
        self, actions: Mapping[str, object], *, policy_version: int,
    ) -> list[TransitionRecord]: ...
    async def close(self) -> None: ...
```

One factory and mutable environment belong to one worker. Returned results are
ordered by configured worker ID, not completion order. Seeds are derived from
the run seed, worker ID, and episode number with a stable hash. Missing or extra
actions fail before any environment advances.

Traces: `RL-FR-LOC-*`, `DS-RL-008..010`, `TEST-RL-LOC-001..003`.

## 3. Rollout records

### IF-RL-005: action and transition envelopes

All distributed records contain:

| Field | Type | Rule |
|---|---|---|
| `schema_version` | positive integer | Version 1 for this baseline |
| `run_id` | non-empty string | Immutable training-run identity |
| `generation` | non-negative integer | Reject mismatch before payload use |
| `worker_id` | non-empty string | Target/source environment worker |
| `episode_id` | non-empty string | Changes on reset |
| `step_id` | non-negative integer | Strictly monotonic within episode |
| `policy_version` | non-negative integer | Policy used for the action |
| `idempotency_key` | non-empty string | Unique for semantic operation |
| `logical_time` | finite float | HLA delivery timestamp when encoded for TSO |
| `payload` | JSON-compatible value | Codec validated before dispatch |

`TransitionRecord` additionally contains previous observation, action, next
observation, reward, `terminated`, `truncated`, and info. A duplicate
idempotency key with identical content is ignored; different content is a
protocol error. Stale generation, episode, step, or policy records are rejected
and counted.

For a local `TransitionRecord`, `logical_time` is the committed pyjevsim
simulation time. On a TSO send, the channel retains that value as
`payload.simulation_time` and sets the envelope `logical_time` to a distinct
delivery timestamp satisfying `current_gorti_time + lookahead`. Receivers use
the envelope time for ordering and the payload time for trajectory provenance.

Traces: `RL-FR-RL-002..007`, `RL-FR-FED-003`, `DS-RL-016..018`.

## 4. gorti interface

### IF-RL-006: FOM profile

Version 1 uses TSO interaction classes `RLAction`, `RLTransition`,
`RLControl`, and `RLPolicyAnnouncement`. The shared parameters are the fields
in IF-RL-005. `payload` carries canonical UTF-8 JSON for the MVP. Large tensors,
model packages, policies, replay segments, and checkpoints are external
immutable artifacts; the FOM carries only URI, SHA-256, size, media type, and
version.

This four-interaction FOM is the executable `MS-RL-04` vertical slice. The
additional typed objects and interactions listed in `SDD` section 7.2 are the
target federation profile for later milestones, not claims about this MVP FOM.

The coordinator publishes action/control/policy and subscribes to transition.
An environment worker subscribes to action/control/policy and publishes
transition. DDM regions may shard by worker or environment ID. Publication and
subscription shall complete before the bootstrap synchronization point.

### IF-RL-007: logical-time channel

```python
class GortiRolloutChannel:
    async def declare(self, role: str) -> None: ...
    async def enable_time(self, *, lookahead: float) -> None: ...
    async def send_action(self, command: ActionCommand) -> None: ...
    async def send_transition(self, record: TransitionRecord) -> None: ...
    async def send_control(self, command: object) -> None: ...
    async def send_policy_announcement(self, announcement: object) -> None: ...
    async def advance_to(self, logical_time: float) -> GrantedBatch: ...
    async def rendezvous(self, label: str, *, register: bool = False) -> None: ...
```

`enable_time` enables regulation and constrained mode before any advance.
Timestamped sends shall satisfy lookahead. `advance_to` issues NER and returns
only after all eligible TSO callbacks preceding the matching grant have been
decoded. Federation halt, event-stream exhaustion, timeout, schema failure, and
generation mismatch are explicit failures; they are never silently dropped.
Callback-consuming operations are serialized per channel so an advance and a
rendezvous cannot compete for the same federate event stream. A callback or
post-grant protocol failure terminally fails that channel; recovery requires a
fresh joined channel rather than a duplicate NER.

`rendezvous` uses gorti synchronization points for bootstrap, reset, policy
activation, checkpoint, evaluation, and shutdown barriers. Per-step ordering
uses logical time, not a global synchronization point.

Traces: `RL-FR-FED-001..012`, `DS-RL-011..014`,
`TEST-RL-FED-001..003`, `TEST-RL-SYNC-001`, `TEST-RL-TIME-002`.

## 5. Completion, errors, and security

- Local calls complete when the executor decision boundary is committed.
- gorti sends complete at the confirmed SDK boundary; receipt is proven only
  by the recipient transition/acknowledgement.
- Queue admission is not learner consumption. Bounded queues expose overload.
- Public failures use typed exceptions with run, worker, episode, step, and
  recoverability metadata; secrets and raw proprietary payloads are redacted.
- Production federation connections use TLS and authenticated identities.
  Role authorization and artifact integrity checks precede model or policy
  loading.

## 6. Versioning and provenance

Every run records framework, gorti, pyjevsim, model, FOM, policy, and schema
versions plus configuration and seed-tree hashes. Version 1 targets the
pyjevsim 2.x `SysExecutor` profile; each supported release requires the STD
compatibility suite. The reviewed upstream 2.1.2 Git revision is the current RL
qualification target because it preserves the required HLA-time confluent
semantics. Version 2.0.1 remains usable by the legacy bridge but is not an RL
semantic qualification target.
