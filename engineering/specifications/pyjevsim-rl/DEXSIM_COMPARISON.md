# DEXSim Architecture Comparison

Document ID: PYJEVSIM-RL-DEXSIM-COMP

Version: 0.1-draft

Status: Draft

Updated: 2026-07-19

## 1. Scope and sources

This document compares the proposed pyjevsim reinforcement-learning framework
with DEXSim, the experimental environment described by Choi, Seo, and Kim in
“DEXSim: an experimental environment for distributed execution of replicated
simulators using a concept of single simulation multiple scenarios,”
*Simulation*, 90(4), 2014, pp. 355–376,
DOI `10.1177/0037549713520251`.

Primary public sources:

- paper: <https://www.sce.carleton.ca/faculty/wainer/papers/SIMULATION-2014-Choi-355-76.pdf>
- DOI: <https://doi.org/10.1177/0037549713520251>
- bibliographic record: <https://dblp.org/rec/journals/simulation/ChoiSK14>

The comparison distinguishes statements reported by the paper from design
inferences for this project. DEXSim is not treated as an RL framework and the
paper's performance result is not directly comparable with an online RL step
benchmark.

## 2. DEXSim architecture

### 2.1 Objective and SSMS model

DEXSim targets faster collection of simulation results by executing replicas
of a simulator over many scenarios. Its “single simulation multiple scenarios”
(SSMS) concept treats simulator replication, scenario distribution, and
concurrent execution as the counterpart of applying one operation to multiple
data items. The useful parallelism is predominantly independent scenario
execution.

DEXSim controls executable simulators rather than requiring the framework to
execute individual simulation-model transitions. This supports legacy and
heterogeneous simulator programs and assigns simulator processes to CPU cores.

### 2.2 Two-tier structure

The paper defines a global/local hierarchy and implements those layers as a
federation DEXSim and one or more federate DEXSims.

```text
Federation DEXSim (global)
  +-- Simulation Manager
  +-- Node Manager
  +-- Synchronization Manager
           |
           | HLA/RTI protocols
           v
Federate DEXSim (one local machine, repeated N times)
  +-- Scenario Manager
  +-- Process Manager
  +-- Run Controller
           |
           +-- simulator processes assigned to CPU cores
```

Federation components:

- **Simulation Manager** generates simulator/scenario jobs, distributes job
  subsets, broadcasts start/stop control, and collects responses.
- **Node Manager** registers machines, records CPU/core identifiers, monitors
  CPU, memory and process status, and arranges results.
- **Synchronization Manager** creates and partitions the scenario list and
  sends executable simulators, scenarios, and partial lists to local machines.

Federate components:

- **Scenario Manager** holds a partial scenario pool and supplies the next
  scenario when requested.
- **Process Manager** bridges commands to available CPU cores, starts simulator
  processes, and asks for a new scenario when a core becomes idle.
- **Run Controller** observes local resource/process health and reports status
  and results to the federation layer.

### 2.3 Protocol and middleware use

DEXSim defines node, synchronization, scenario, and process-management
protocols. Federation and federate DEXSim components are independent HLA/RTI
federates. Declaration Management establishes publication/subscription, and
Object Management is used for control/data exchange, including distribution
of simulators, scenarios, and scenario lists. A new local resource can be
added by starting another federate DEXSim and joining it to the experiment.

The paper's protocol discussion is primarily job, node, file, and process
coordination. It does not present logical-time-controlled RL decision steps,
policy consistency, experience replay, or learner state as architectural
concerns.

### 2.4 Scheduling and reported scaling

The global layer initially partitions the scenario set across machines. The
local Process Manager then keeps cores busy by assigning another scenario to
an idle core instead of waiting for a slower core. This is local dynamic load
balancing over an initially distributed workload.

For its scaling study, the paper reports that 16 quad-core PCs, an ideal 64-way
resource increase, reduced one large experiment by about 55 times relative to
a single core. The authors identify network and task-switching overhead and
state that federation-level balancing across heterogeneous machines remains
future work. These results establish DEXSim's intended scalability but shall
not be used as a baseline claim for the proposed framework without reproducing
the workload and completion boundary.

## 3. Proposed pyjevsim-RL architecture

The proposed architecture retains DEXSim's separation of global coordination
and local resource control but changes the unit of work and the consistency
contract.

```text
Federation Coordinator
  +-- run/worker/assignment ownership
  +-- synchronization and logical-time policy
  +-- policy/checkpoint metadata
          |
          | gorti data/time plane
          v
Local Supervisor / Federation Worker
  +-- isolated environment workers
  +-- Gym-like episode runtimes
  +-- canonical SysExecutor instances
          |
          +-- actor -> experience -> learner
                         |
                         +-- immutable policy/artifact registry
```

The canonical simulation unit is one rebuilt `SysExecutor` graph per
environment episode, not an arbitrary simulator executable plus a static
scenario file. The online work item is an identified action/transition step;
an episode and rollout are higher-level collections of those steps.

## 4. Structural mapping

| DEXSim concept | Proposed element | Relationship |
|---|---|---|
| Federation DEXSim | `DS-RL-011` Federation coordinator | Both own global experiment coordination. The proposed element also owns generation, role admission, logical-time mode, and policy/checkpoint phases. |
| Simulation Manager | `DS-RL-011`, `DS-RL-019` | Job generation becomes run/episode/assignment management with immutable provenance. |
| Node Manager | `DS-RL-008`, `DS-RL-020` | Resource and process monitoring is retained, with leases, fencing, telemetry, and recovery state. |
| Synchronization Manager | `DS-RL-014`, `DS-RL-018`, `DS-RL-021` | File readiness expands to logical-time, policy activation, and checkpoint barriers. |
| Federate DEXSim | `DS-RL-008` plus `DS-RL-012` | A host-level worker joins the federation while retaining a local isolated-process supervisor. |
| Scenario Manager | `DS-RL-007`, assignment queue | Static scenario selection becomes episode configuration, seed, reset, and lease management. |
| Process Manager | `DS-RL-008`, `DS-RL-009` | Core assignment becomes capability/resource-aware scheduling of complete environment runtimes. |
| Run Controller | `DS-RL-020` | Health/result reporting becomes structured logs, metrics, traces, faults, and provenance. |
| Simulator executable | `DS-RL-002`, `DS-RL-003` | Legacy executable invocation is replaced by a normal pyjevsim factory and canonical `SysExecutor` seam. |
| Simulator/scenario files over OM | `DS-RL-013`, `DS-RL-015` | Small control/schema data stays in HLA; large immutable artifacts move by verified reference. |

## 5. Detailed comparison

| Dimension | DEXSim | pyjevsim-RL/gorti design |
|---|---|---|
| Primary goal | Faster collection across many simulator/scenario runs | Correct and scalable policy learning from online environment interaction |
| Parallel unit | Independent simulator/scenario execution | Isolated environment episode/rollout and identified action step |
| Model integration | Launch executable simulator processes | Build standard pyjevsim graphs and execute only through `SysExecutor` |
| Reset | New simulator/scenario execution | Rebuild a fresh executor/model graph for every default reset |
| User API | Experiment/job control protocols | Gym-like reset/step plus vector facade and distributed envelopes |
| Global scheduling | Partition partial scenario lists | Capability-aware assignment with lease epoch and generation fencing |
| Local scheduling | Assign next scenario to idle core | Bounded process pool hosting isolated executor instances |
| Middleware | HLA/RTI for federation/federate control and data | gorti federation lifecycle, pub/sub, DDM, sync points, time grants, ordered callbacks, save/restore evidence |
| Time semantics | Execution synchronization; logical simulation-time control is not the paper's central protocol | Explicit lockstep, bounded-lag, or independent-rollout logical-time mode |
| Learning | Not in scope | Actor, learner, experience batches, replay/checkpoint, immutable policy versions |
| Data plane | Executables, scenarios, lists, status, results | HLA control/small simulation data plus separate tensor/artifact plane |
| Consistency identity | Node/core/job identifiers | Run, generation, session, lease, environment, episode, step, policy, message IDs |
| Dynamic scale | Join another federate DEXSim | Join authenticated/capability-declared workers; rebalance only through fenced assignments |
| Failure handling | Monitor abnormal simulator/resource status and permit control/recovery | Typed phase/completion failures, lease expiry, idempotency, checkpoint cuts, stale-work rejection |
| Reproducibility | Scenario/executable distribution and experimental results | Full manifest with code/dependency/FOM/model/policy hashes, seed tree, event/logical-time evidence |
| Security | Not a primary architecture topic in the paper | TLS/authentication, role authorization, artifact verification, process isolation, secret redaction |

## 6. DEXSim ideas deliberately retained

### 6.1 Hierarchical coordination

DEXSim demonstrates that central experiment intent and per-machine execution
control should be separated. `DS-RL-011` and `DS-RL-008` retain this division.
It limits global scheduling chatter, gives the local supervisor immediate
knowledge of process health, and permits a node to use multiple cores without
exposing core-level details to the federation coordinator.

### 6.2 Simulator replication rather than model partitioning

DEXSim obtains useful speedup without partitioning a simulator's internal
model. The proposed design likewise starts with complete `SysExecutor`
instances. This keeps DEVS coupling and scheduling local and makes parallel
rollouts embarrassingly parallel where the problem permits it. Partitioning a
single coupled model across federates is a different future capability and is
not implied by actor parallelism.

### 6.3 Dynamic resource admission

The ability to join a new federate worker is retained, but strengthened with
authenticated role/capability declaration, immutable manifest acceptance,
leases, and generation fencing. Capacity is not usable merely because a
process joined; it becomes schedulable only after its model and artifact
hashes are verified and its environments are ready.

### 6.4 Protocol-defined extensibility

DEXSim allows third parties to implement components that interoperate through
published protocols. The proposed FOM profile and framework-neutral actor,
learner, model-factory, and artifact interfaces follow the same principle.
No supported RL library is allowed to become the core domain interface.

## 7. Required departures for reinforcement learning

### 7.1 Online causal loop

DEXSim scenarios can be queued and completed as batch jobs. RL actions are
chosen from observations produced by prior committed simulation progress.
Therefore the proposed design must preserve action admission, simulation
commit, observation construction, experience durability, learner ingestion,
and policy activation as separate causal boundaries.

### 7.2 Reset correctness

Starting another simulator process naturally creates fresh state in DEXSim.
An in-process Gym-like environment could accidentally retain model state.
`DS-RL-002` and `DS-RL-007` make process-like freshness explicit by rebuilding
the `SysExecutor` graph. Snapshot reset is an optimization subject to
equivalence tests, not the baseline contract.

### 7.3 Logical-time modes

RL workloads are not uniformly synchronized. Shared-world multi-agent
training may require lockstep, asynchronous actors need independent rollout,
and population training may need bounded policy/environment lag. `DS-RL-014`
therefore uses gorti Time Management and synchronization points through three
declared modes rather than one implicit experiment-wide barrier.

### 7.4 Policy and experience provenance

Scenario identity alone is insufficient. A valid transition must name the
policy that generated its action, the exact environment episode/step, and the
federation generation. Without this, retries or late callbacks can corrupt a
learner with plausible but stale data.

### 7.5 Bulk artifact split

DEXSim's distribution of executable and scenario files through the
experimental protocol is appropriate to its workload and era. Modern policy
weights, replay shards, and tensor observations can be large and frequent.
Routing those bytes through routine RTI object updates would consume ordered
middleware capacity needed for control and time progress. `DS-RL-015` stores
them once and lets HLA carry verified references.

## 8. Architectural advantages obtained from gorti

Relative to the DEXSim paper's HLA use, the current gorti implementation gives
this design more explicit, testable middleware semantics:

- federation generations fence stale work after name reuse;
- declaration and object/interaction services give typed FOM routing;
- DDM can restrict environment-, shard-, and policy-specific delivery;
- sync points coordinate readiness, policy barriers, checkpoint cuts, and
  shutdown;
- Time Management orders timestamped callbacks before grants and supports
  logical rather than wall-clock progression;
- per-federate ordered callbacks and confirmed boundaries expose completion;
- event logging and save/restore support reproducibility and recovery evidence;
- Go, Python, and C++ SDKs share a versioned Protobuf/gRPC contract.

These benefits are useful only if the RL layer preserves them. A local queue
acceptance shall not be called an RTI acknowledgement; an RTI acknowledgement
shall not be called learner durability; and a policy announcement shall not be
called activation.

## 9. Trade-offs and risks

### 9.1 Central bottlenecks

DEXSim and the initial proposed deployment both have central coordination.
Current gorti also documents a standalone authoritative `rtid`. High-frequency
step traffic, global barriers, a single learner, or one artifact registry can
become bottlenecks. Mitigations are local vector execution, step/experience
batching, DDM partitioning, independent rollout where valid, immutable artifact
caching, and explicit measurement of each completion boundary.

### 9.2 Stragglers and heterogeneous resources

DEXSim calls federation-level heterogeneous balancing future work. The
proposed design does not assume equal cores: workers declare capabilities and
the coordinator uses leases and measured throughput. Lockstep still inherits
straggler sensitivity; bounded-lag and independent rollout are semantic
alternatives, not merely scheduler tuning.

### 9.3 Middleware and serialization overhead

An in-process environment will be faster than a network round trip. Federation
mode is selected for scale, coordination, or interoperability, not assumed to
accelerate every model. Benchmarking must compare identical semantics and
distinguish simulator time, serialization, RTI admission/service, callback,
artifact transfer, and learner time.

### 9.4 RTI availability

Worker recovery does not provide RTI high availability. Production
multi-node/failover gorti is outside the current supported baseline. The
framework can checkpoint and diagnose an RTI loss, but shall not claim
transparent federation recovery until gorti defines a verified cluster
contract.

### 9.5 Untrusted model execution

DEXSim's simulator-process boundary is naturally isolating. An in-process
pyjevsim environment can weaken that boundary. The default parallel design
therefore uses isolated worker processes, resource limits, validated
configuration and artifacts, and role-restricted federation credentials.

## 10. Comparison verification plan

A claim-grade comparison requires two separate experiments.

### 10.1 Architectural capability comparison

Use one simulator/model and scenario set to verify:

- single host and multi-host execution;
- dynamic worker join and readiness;
- all assigned scenarios/episodes complete exactly once or have an explicit
  failed disposition;
- local resource utilization and scheduling fairness;
- injected worker failure and recovery; and
- result/provenance completeness.

### 10.2 Performance comparison

Preserve the existing gorti fair-comparison discipline: identical workload and
input hashes, fixed seeds, equivalent process count and logging, warm-up,
balanced run order, completed-delivery accounting, semantic acceptance before
latency claims, and median/tail/confidence-interval reporting.

Report at least:

- wall-clock makespan and speedup versus one worker;
- simulator utilization and scheduling overhead;
- transitions/episodes per second;
- p50/p95/p99 reset and step latency;
- RTI and artifact-plane bytes and latency;
- policy staleness and experience rejection/duplication; and
- efficiency `speedup / allocated parallel capacity`.

The DEXSim paper's approximately 55x result on 64 cores is historical evidence
for its own experiments, not an acceptance threshold for this project.

## 11. Conclusion

DEXSim supplies a strong architectural precedent for hierarchical experiment
management, replication of complete simulators, local core utilization,
dynamic federate addition, and protocol-based extensibility. The proposed
framework keeps those strengths while adding the contracts required by
reinforcement learning: a canonical `SysExecutor` seam, rebuild reset,
Gym-like causal steps, actor/learner and policy provenance, explicit
logical-time modes, generation fencing, coordinated checkpoints, and a split
between HLA control/data metadata and bulk tensors.

The principal architectural advantage is not distribution by itself. It is
that gorti can make distributed environment interaction ordered, inspectable,
fenced, and reproducible. The principal cost is additional coordination and
serialization, which the local execution profile and independent-rollout mode
avoid when those guarantees are unnecessary.
