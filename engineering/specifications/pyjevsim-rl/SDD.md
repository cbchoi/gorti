# Software Design Description: pyjevsim Reinforcement Learning Framework

Document ID: PYJEVSIM-RL-SDD

Version: 0.1-draft

Status: Draft

Updated: 2026-07-19

Applies to: `RL-FR-MDL-*`, `RL-FR-GYM-*`, `RL-FR-EXEC-*`,
`RL-FR-LOC-*`, `RL-FR-FED-*`, `RL-FR-RL-*`, `RL-FR-OPS-*`, and
`RL-NFR-*` requirements in the companion SRS

## 1. Purpose and design authority

This document defines the architecture for training reinforcement-learning
policies against pyjevsim models, first on one host and later as a
gorti-managed federation. It is subordinate to the companion SRS and is the
design authority for component ownership, execution semantics, logical time,
data movement, failure behavior, security, and observability.

The design follows these non-negotiable rules:

1. `pyjevsim.system_executor.SysExecutor` is the canonical simulation seam.
   Framework code shall not drive a `BehaviorModel` or `StructuralModel`
   transition directly.
2. `reset()` rebuilds a fresh executor and model graph by default. It does not
   attempt to clean arbitrary model state in place.
3. The local control plane and the federation data/time plane are separate.
   A local environment remains useful without an RTI, while federation mode
   adds distributed ownership and synchronization without changing the model
   contract.
4. HLA traffic carries ordered control, simulation-domain data, metadata, and
   artifact references. Large tensors, replay shards, and model checkpoints
   use a bulk artifact plane.
5. Every run is identified by experiment, federation generation, worker,
   environment, episode, policy version, seed, and model/FOM/artifact hashes.
6. Identical admitted inputs under the same deterministic profile produce the
   same semantic trajectory, independent of transport timing.

## 2. Architectural drivers

| Driver | Design response |
|---|---|
| Existing pyjevsim models must work with minimal changes (`RL-FR-MDL-*`) | A factory builds a normal `SysExecutor`; adapters observe and inject only through executor-facing ports and documented executor methods. |
| Familiar RL interaction (`RL-FR-GYM-*`) | A Gym-like environment exposes `reset` and `step`, including `terminated`, `truncated`, and structured `info`. |
| Correct DEVS execution (`RL-FR-EXEC-*`) | `SysExecutor` initialization, `HLA_TIME` stepping, external-event insertion, generated-output collection, and executor time are the canonical operations. |
| Local parallelism before distributed deployment (`RL-FR-LOC-*`) | A supervisor owns isolated worker processes and assigns one or more complete environment instances per worker. |
| Federation-centred future (`RL-FR-FED-*`) | A gorti federation owns membership, synchronization, routing, time grants, generation fencing, and ordered callbacks. |
| Algorithm independence (`RL-FR-RL-*`) | Actor/learner contracts use framework-neutral transition and policy envelopes; library integrations are replaceable adapters. |
| Operability and reproducibility (`RL-FR-OPS-*`, `RL-NFR-*`) | Immutable run manifests, structured telemetry, checkpoints, fault isolation, and deterministic evidence are first-class components. |

## 3. System context

```text
RL algorithm / user application
        |
        v
Gym-like environment or vector environment
        |
        v
Episode runtime ---- Model factory ---- Model package
        |
        v
SysExecutor adapter ---- pyjevsim SysExecutor ---- registered DEVS graph
        |
        +------ local output/observation/reward adapters
        |
        +------ optional gorti federation gateway
                    |
                    +-- HLA control and simulation data
                    +-- time management and sync points
                    +-- artifact references

Actor workers ---- experience plane ---- learner ---- policy registry
      |                                      |
      +------------ policy snapshots --------+
```

The framework has four planes:

- **Execution plane**: `SysExecutor` instances and their model graphs.
- **Local control plane**: lifecycle, placement, health, cancellation, and
  result collection on one host.
- **Federation data/time plane**: gorti HLA services for distributed state,
  messages, membership, synchronization, and logical-time progress.
- **Learning/artifact plane**: actors, learners, transition batches, policies,
  checkpoints, replay shards, and immutable manifests.

No component may silently substitute one plane's completion boundary for
another. Local queue admission, RTI acceptance, callback delivery, artifact
durability, learner ingestion, and policy activation are distinct events.

## 4. Design element catalogue

| ID | Element | Responsibility | Traces to |
|---|---|---|---|
| `DS-RL-001` | Model package contract | Declares executor factory, spaces, adapters, version, dependencies, and hashes | `RL-FR-MDL-*` |
| `DS-RL-002` | Executor factory | Builds a new `SysExecutor`, registers the model graph, ports, coupling relations, and initial events | `RL-FR-MDL-*`, `RL-FR-EXEC-*` |
| `DS-RL-003` | SysExecutor adapter | The only runtime seam to initialize, inject, advance, stop, inspect time, and collect external outputs | `RL-FR-EXEC-*` |
| `DS-RL-004` | Observation adapter | Converts executor-visible state/events to a validated observation | `RL-FR-GYM-*` |
| `DS-RL-005` | Action adapter | Validates an action and schedules corresponding executor external events | `RL-FR-GYM-*`, `RL-FR-EXEC-*` |
| `DS-RL-006` | Reward/termination evaluator | Computes reward, termination, truncation, and domain metrics from an immutable step view | `RL-FR-GYM-*` |
| `DS-RL-007` | Episode runtime | Implements reset/step lifecycle, seed derivation, step identity, and failure cleanup | `RL-FR-GYM-*`, `RL-FR-OPS-*` |
| `DS-RL-008` | Local supervisor | Starts isolated workers, assigns environments, monitors leases, cancels work, and aggregates status | `RL-FR-LOC-*` |
| `DS-RL-009` | Local worker | Hosts one or more complete episode runtimes without sharing mutable executor state | `RL-FR-LOC-*` |
| `DS-RL-010` | Vector environment facade | Presents ordered batched reset/step while preserving per-environment identity and errors | `RL-FR-LOC-*`, `RL-FR-GYM-*` |
| `DS-RL-011` | Federation coordinator | Creates/joins the run federation, admits workers, assigns roles, publishes manifests, and coordinates phases | `RL-FR-FED-*` |
| `DS-RL-012` | Federation worker gateway | Maps worker lifecycle and environment traffic to gorti SDK calls and ordered callbacks | `RL-FR-FED-*` |
| `DS-RL-013` | FOM profile | Defines control, environment, policy-reference, metric, and fault schemas | `RL-FR-FED-*` |
| `DS-RL-014` | Logical-time coordinator | Implements lockstep, bounded-lag, and independent-rollout modes using gorti time services | `RL-FR-FED-*`, `RL-NFR-DET-*` |
| `DS-RL-015` | Artifact registry | Stores and resolves immutable model, policy, checkpoint, replay, and tensor artifacts | `RL-FR-RL-*`, `RL-FR-OPS-*` |
| `DS-RL-016` | Actor adapter | Runs inference, obtains environment transitions, batches experience, and reports policy provenance | `RL-FR-RL-*` |
| `DS-RL-017` | Learner adapter | Consumes validated batches, updates policy, and publishes immutable policy versions | `RL-FR-RL-*` |
| `DS-RL-018` | Policy distributor | Announces policy references and controls activation/acknowledgement barriers | `RL-FR-RL-*`, `RL-FR-FED-*` |
| `DS-RL-019` | Run manifest/provenance store | Records configuration, hashes, seed tree, membership, versions, and final disposition | `RL-FR-OPS-*`, `RL-NFR-DET-*` |
| `DS-RL-020` | Telemetry and audit | Emits structured logs, metrics, traces, and append-only lifecycle/audit records | `RL-FR-OPS-*`, `RL-NFR-OBS-*` |
| `DS-RL-021` | Checkpoint coordinator | Coordinates simulator, actor, learner, replay, and federation checkpoint boundaries | `RL-FR-OPS-*`, `RL-FR-FED-*` |
| `DS-RL-022` | Security policy enforcement | Authenticates nodes, authorizes roles, validates artifacts, and protects secrets | `RL-NFR-SEC-*` |

## 5. Canonical pyjevsim execution seam

### 5.1 Model package and factory

`DS-RL-001` exposes a factory rather than an already-instantiated model. A
factory invocation receives an immutable `EpisodeBuildContext` containing the
episode seed, environment identity, configuration, time resolution, and
artifact resolver. It returns a fully constructed `SysExecutor` and adapter
bindings. The factory performs the ordinary pyjevsim setup sequence:

1. construct `SysExecutor(time_resolution, simulation_name, execution_mode)`;
2. insert any top-level executor input ports;
3. instantiate and register behavior or structural models;
4. establish all coupling relations;
5. schedule declared initial external events; and
6. initialize the simulation before the first observation is produced.

The model package manifest declares compatible pyjevsim versions, Python
runtime, configuration schema, observation and action spaces, determinism
profile, logical-time capability, snapshot capability, and dependency hashes.
Importing a package shall not start threads, processes, network clients, or a
simulation.

### 5.2 Why rebuild is the default reset

`SysExecutor.simulation_stop()` and relation reset operations are useful
engine functions, but they cannot prove that arbitrary user model objects,
module globals, random-number generators, callbacks, and external resources
have returned to their construction state. Therefore `DS-RL-007.reset()`:

1. stops and disposes the previous executor;
2. releases adapter and external-resource handles;
3. derives the next episode seed from the run seed tree;
4. invokes `DS-RL-002` to build a fresh executor and graph; and
5. returns the initial observation and provenance information.

An optimized snapshot-restore reset may be added only as a separately named
profile after trajectory equivalence, resource cleanup, and seed restoration
are demonstrated against rebuild reset. It shall fail closed to rebuild or
surface an error; it shall not silently return a partially restored episode.

### 5.3 Step transaction

`DS-RL-007.step(action)` is a transaction identified by `(generation,
environment_id, episode_id, step_id)`:

1. reject calls before reset, after terminal state, or with the wrong step ID;
2. validate and normalize the action against the declared space;
3. let `DS-RL-005` translate it to one or more timestamped executor inputs;
4. insert those events through `SysExecutor` APIs;
5. advance the executor for the configured decision interval or until the
   declared decision event;
6. collect externally visible outputs and the committed executor global time;
7. create an immutable `StepView`;
8. derive observation, reward, terminated, truncated, and domain metrics;
9. append provenance and telemetry; and
10. return only after the selected completion boundary is satisfied.

Exceptions do not yield a plausible transition. The environment enters
`FAILED`; `info` or the distributed fault envelope records whether the action
was rejected before admission, admitted with an unknown outcome, or committed
before observation construction failed.

### 5.4 Gym-like API

The synchronous facade follows the familiar shape without making Gymnasium a
mandatory core dependency:

```text
reset(seed=None, options=None) -> (observation, info)
step(action) -> (observation, reward, terminated, truncated, info)
close() -> None
```

`info` contains at least the run/federation generation, environment, episode,
step, policy version, logical time, seed identifier, model hash, FOM hash when
federated, and timing boundaries. `terminated` represents a domain terminal
condition; `truncated` represents a configured horizon, timeout, administrative
cancellation, or other non-domain cutoff. A failure that invalidates transition
semantics is raised or returned by the vector facade as a typed per-slot error,
not disguised as normal truncation.

## 6. Local parallel control plane

### 6.1 Process topology

`DS-RL-008` is a supervisor, not a simulation engine. It maintains a desired
set of worker processes, a work queue, leases, health state, and result
channels. Each `DS-RL-009` worker owns complete episode runtimes. Mutable
`SysExecutor` instances and model objects are never shared across workers.

Process isolation is the default because user models may have native
extensions, global state, non-thread-safe dependencies, or unbounded failure.
Thread-based execution may exist only as an explicit capability profile.

### 6.2 Scheduling and flow control

The local scheduler uses capacity and readiness, not CPU-number assumptions.
Each assignment carries environment count, resource requirements, model hash,
configuration hash, seed range, and lease. A worker accepts or rejects the
whole assignment. The supervisor does not report a slot as ready until the
worker has built its executor and returned its initial observation.

Bounded queues exist at assignment, action, transition, and telemetry
boundaries. Saturation either blocks within a declared timeout or returns a
typed backpressure result. Dropping observations, rewards, terminal markers,
or failures is prohibited.

`DS-RL-010` preserves input slot order in its returned batch even if workers
complete out of order. Per-slot errors are isolated where possible. A worker
crash invalidates only its owned environments; their recovery policy is
restart-from-seed, restart-from-checkpoint, or fail-run as declared in the
manifest.

### 6.3 Local-to-federation migration

The supervisor API deals in environment commands and transition envelopes,
not process handles. In federation mode the same commands are handled by
`DS-RL-012`; therefore a user model and RL algorithm do not change when worker
placement crosses a host boundary.

## 7. GORTI federation data and time plane

### 7.1 Federation lifecycle

`DS-RL-011` creates or joins a named experiment federation and publishes an
immutable run manifest. Workers join with declared role and capabilities.
Synchronization points gate at least `MANIFEST_ACCEPTED`, `ENVIRONMENTS_READY`,
`TRAINING_START`, optional `POLICY_ACTIVATE/<version>`, `CHECKPOINT/<id>`, and
`TRAINING_STOP`.

Normal shutdown stops new assignments, drains or explicitly abandons admitted
steps, checkpoints as configured, resigns all roles, and destroys the empty
federation. Reusing a federation name creates a new gorti generation.

### 7.2 FOM profile and data placement

`DS-RL-013` separates control objects/interactions from bulk artifacts.
Candidate classes include:

- objects: `Experiment`, `Worker`, `Environment`, `Policy`, `Checkpoint`;
- interactions: `WorkerReady`, `AssignEnvironment`, `Reset`, `Action`,
  `Transition`, `ExperienceBatchReady`, `PolicyAvailable`, `ActivatePolicy`,
  `Fault`, `Cancel`, and `Heartbeat`.

The executable `MS-RL-04` vertical slice intentionally implements only
`RLAction`, `RLTransition`, `RLControl`, and `RLPolicyAnnouncement` as TSO
interactions with a canonical envelope. The candidate object/interaction set
above is the versioned expansion target and requires its own compatibility and
conformance gate before it becomes part of the supported FOM.

Small, schema-stable observations/actions may be encoded directly. Large or
variable tensors, policy weights, replay shards, model packages, and
checkpoints are stored by `DS-RL-015`; HLA carries an `ArtifactRef` with URI,
content hash, byte length, media/schema type, encryption/key reference, and
expiry. A receiver verifies the size and hash before acknowledging readiness.

FOM attributes and parameters carry explicit schema version and units. DDM
regions or equivalent subscription partitioning route traffic by experiment,
worker group, environment shard, policy channel, and learner destination.

### 7.3 Logical-time modes

The run manifest chooses exactly one `DS-RL-014` mode. A participant that
cannot support it is rejected before `TRAINING_START`.

#### Lockstep

All participating environments advance decision step `k` before any advances
to `k+1`. Actions and simulation outputs use timestamp order; eligible
callbacks are consumed before the matching grant. This mode provides the
strongest cross-environment alignment and is required for tightly coupled
multi-agent or shared-world experiments. Its cost is straggler sensitivity.

#### Bounded-lag

Each environment may lead the minimum committed cohort time by at most a
declared logical window. The coordinator publishes the cohort watermark and
withholds grants or assignments beyond it. Experience records retain each
environment's logical time and policy version. This mode trades exact barrier
alignment for throughput while bounding staleness.

#### Independent rollout

Environment instances have no step barrier with peers. Each still preserves
its own ordered event/grant semantics, but actors request progress independently
and submit experience asynchronously. This is the default for embarrassingly
parallel on-policy or off-policy rollouts that do not share simulated state.
Policy activation may be immediate-on-next-reset or explicitly barriered.

Logical simulation time is never inferred from wall-clock arrival. Wall time is
used only for leases, operational timeout, metrics, and failure detection.

### 7.4 Policy synchronization

`DS-RL-017` publishes immutable policy version `p` to `DS-RL-015` and announces
its reference through `DS-RL-018`. An actor verifies the artifact, loads it,
and acknowledges readiness. Activation semantics are declared as one of:

- next action after local acknowledgement;
- next episode reset;
- cohort barrier at a named logical step; or
- evaluation-only assignment.

Every transition records the policy version used to choose its action.
Coordinator policy version is not retroactively applied to already-admitted
actions.

## 8. State ownership and consistency

| State | Authoritative owner | Replicas/caches |
|---|---|---|
| pyjevsim model graph and executor clock | Environment worker | None while live; checkpoint is immutable evidence |
| Environment/episode/step lifecycle | Environment worker, fenced by coordinator lease | Coordinator status projection |
| Federation membership and generation | gorti `rtid` | SDK connection state |
| Logical-time requests, queues, and grants | gorti time manager | Participant's last committed grant |
| Worker assignment and lease | Federation coordinator | Worker accepted-assignment record |
| Experience batch | Actor until durable learner/store acknowledgement | In-flight immutable copies |
| Learner optimizer state | Learner | Checkpoint replicas |
| Policy version metadata | Policy registry | Actor verified cache |
| Run manifest and provenance | Manifest store | Read-only participant copy |
| Artifact bytes | Artifact registry | Hash-verified local cache |

Cross-owner operations use validate, reserve, commit, publish order. For
example a transition batch is validated and durably admitted before actor
retention may be released. Metrics never substitute for authoritative state.

## 9. Generation fencing and identity

All framework envelopes contain:

```text
experiment_id
federation_name
federation_generation
participant_id and participant_session
worker_id and lease_epoch
environment_id
episode_id
step_id
policy_version
message_id and causation_id
logical_time
schema_version
```

The federation generation comes from the active gorti federation. A worker
lease epoch increments on reassignment. Receivers reject stale generation,
session, lease, episode, or step values before mutating state. Deduplication by
message ID is scoped to generation and sender session. Recreating a federation
or worker never makes an old message valid again.

## 10. Failure and recovery design

Failures are classified as validation, model, executor, worker, transport,
time-progress, artifact, learner, security, or operator failures. Each failure
records phase and completion boundary.

- Invalid actions are rejected before executor admission.
- Executor/model exceptions fail the environment and trigger its declared
  recovery policy.
- Worker loss expires its lease; assignments are not reused until fenced by a
  higher epoch.
- Transport ambiguity is reported as indeterminate unless a durable message
  ID or server acknowledgement proves the outcome.
- Missing logical-time progress produces a diagnostic with blocking
  participants and queued bounds; it does not synthesize a grant.
- Artifact hash or signature mismatch is a security failure and is never
  retried from the same reference as if transient.
- Learner loss resumes from an accepted checkpoint or fails the run; actors
  retain unacknowledged batches subject to bounded storage.

`DS-RL-021` checkpoint is a coordinated cut containing the run manifest,
federation generation/event-log boundary, active assignment table, environment
snapshots or rebuild coordinates, actor batch watermarks, learner/optimizer
state, replay metadata, policy versions, and hashes. A partial cut is marked
incomplete and is not restorable.

## 11. Security design

`DS-RL-022` applies least privilege by role: coordinator, environment worker,
actor, learner, evaluator, and operator. Production federation connections use
TLS and configured authentication. Authorization binds an authenticated
principal to permitted federation, role, interaction classes, object
attributes, artifact namespaces, and administrative operations.

Model packages and artifacts are untrusted input. The system verifies content
hashes and optional signatures, constrains filesystem/network access according
to deployment policy, isolates execution processes, limits resources, and does
not deserialize untrusted Python objects across trust boundaries. Portable
schemas and safe tensor formats are preferred over pickle. Secrets are
referenced from a secret provider and never enter FOM payloads, manifests,
logs, checkpoints, or model-visible configuration.

## 12. Observability and provenance

`DS-RL-020` uses structured records with the identities in Section 9 plus
service, operation, result, error class, logical time, policy version, and
timing boundary. Required metrics include:

- environment reset and step latency by admission/commit/return boundary;
- simulated-time per wall-time and transitions per second;
- queue depth, backpressure, worker utilization, and straggler lag;
- RTI call, callback, grant, and synchronization latency;
- experience production, durable ingestion, duplication, and rejection;
- policy publication, fetch, verification, activation, and staleness;
- checkpoint duration, size, completion, and restore outcome; and
- failures, retries, lease epochs, fenced messages, and recovery duration.

High-cardinality identifiers belong in traces/logs rather than unbounded metric
labels. Semantic event logs and performance telemetry are distinct. A run is
reproducible only when its manifest captures source revision, dependency lock,
OS/runtime, pyjevsim/gorti/RL adapter versions, FOM bytes and hash, model and
artifact hashes, seed tree, logical-time mode, configuration, membership, and
checkpoint/evidence references.

## 13. Deployment evolution

### Profile A: local single environment

One process contains the Gym-like facade, episode runtime, adapters, and one
`SysExecutor`. No gorti dependency is needed at runtime.

### Profile B: local parallel

The application uses `DS-RL-008` and `DS-RL-010`; worker processes host
isolated executors. Control channels remain local and bounded.

### Profile C: federation distributed

The coordinator, workers, actors, learner, and artifact service may run on
different nodes. gorti provides federation membership, data routing,
synchronization, logical time, and ordered callbacks. The current standalone
`rtid` remains the authoritative middleware process.

### Profile D: managed federation

Future orchestration schedules and monitors multiple federation services and
supporting stores. Clustered/failover `rtid` is not assumed until gorti itself
defines and verifies that capability. Framework recovery must therefore expose
the distinction between worker recovery and RTI recovery.

## 14. Key design decisions and rejected shortcuts

| Decision | Rationale |
|---|---|
| `SysExecutor` is the only canonical execution seam | Preserves pyjevsim scheduling, time, coupling, and event semantics. |
| Reset rebuilds the engine and graph | Arbitrary model cleanup cannot otherwise be proven complete. |
| Process isolation is the default local parallel unit | Contains model failures and avoids hidden mutable-state sharing. |
| HLA control/data metadata is separated from bulk tensors | Prevents the RTI from becoming an artifact transport bottleneck. |
| Three explicit logical-time modes | Avoids imposing lockstep cost where rollouts are independent while retaining precise semantics where needed. |
| Policy versions and generation are carried per transition | Makes stale data visible and rejects cross-run contamination. |
| Completion boundaries remain distinct | Prevents queue admission from being reported as simulation or learning completion. |

Direct calls to model `output`, `int_trans`, or `ext_trans` from the RL facade;
in-place reset as the default; implicit wall-clock synchronization; unbounded
queues; pickle over federation links; silent worker restart with reused
identity; and large policy tensors embedded in routine HLA interactions are
not permitted by this design.

## 15. Verification obligations

The companion STD shall provide executable evidence for at least:

- fresh construction and disposal on every default reset;
- action-to-external-event and executor-output-to-observation contracts;
- Gym-like terminal/truncation and invalid lifecycle behavior;
- identical semantic trajectory under repeated seed and manifest;
- isolation and ordered batching under local worker completion reordering;
- worker crash, lease expiry, reassignment, and stale-message rejection;
- lockstep callback-before-grant, bounded-lag watermark, and independent
  rollout progress;
- FOM schema compatibility and artifact hash verification;
- policy publication, activation mode, and transition provenance;
- checkpoint completeness and restoration equivalence;
- authorization and secret-redaction behavior; and
- scale tests that distinguish local admission, RTI acknowledgement,
  environment transition, learner ingestion, and policy activation latency.

No `DS-RL-*` element is accepted merely because a demonstration completes; it
must trace to an SRS requirement and an STD test/evidence identifier.
