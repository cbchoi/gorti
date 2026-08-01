# Software Test Description: pyjevsim RL Framework

Document ID: `PYJEVSIM-RL-STD`  
Version: 0.1-draft  
Status: Design baseline candidate  
Updated: 2026-07-19

## 1. Acceptance policy

A mandatory SRS requirement is accepted only when its design, interface,
implementation task, automated test, review, and retained evidence are linked
in `traceability.yaml`. This test plan is aligned with ISO 9001 process,
documented-information, performance-evaluation, and improvement principles; it
does not claim ISO certification.

Evidence records the commit, operating system, Python, pyjevsim and gorti
versions, FOM/model/policy hashes, run configuration, seed, start/end time, and
semantic result digest. A skipped mandatory test is a failure unless an
approved, expiring deviation names the risk and compensating control.

## 2. Test levels and executable cases

| Test ID | Level | Procedure and acceptance |
|---|---|---|
| `TEST-RL-GYM-001` | Unit/API | Reset returns `(obs, info)`; step returns the five-tuple; rebuild reset isolates state; step-after-done fails; termination and truncation remain distinct. |
| `TEST-RL-EXEC-001` | Contract | A protocol fake and supported pyjevsim `SysExecutor` both inject action, choose a non-regressing decision time, step exactly once, and preserve all boundary messages. |
| `TEST-RL-EXEC-002` | Golden trace | Next-event and fixed-delta boundaries produce the declared logical times and immutable step views. |
| `TEST-RL-EXEC-003` | Conformance | Same-time internal+external input invokes confluent semantics; sigma-zero cascades drain before return; multiple messages on one port are not collapsed; shared messages are not mutated. |
| `TEST-RL-DET-001` | Determinism | Ten fresh rebuild episodes with identical model/FOM/config/seed yield one semantic digest; changing a witness input changes it. |
| `TEST-RL-LOC-001` | Integration | N environments reset and step concurrently, return in worker order, remain isolated, and emit complete transition identities. |
| `TEST-RL-LOC-002` | Fault | One worker failure is attributed to that worker; restart creates a new episode and does not reuse mutable executor state. |
| `TEST-RL-LOC-003` | Flow | Missing/extra actions, bounded-queue overload, cancellation, and close fail without partial batch advancement. |
| `TEST-RL-FED-001` | Integration | Coordinator and worker declare complementary FOM roles, join, bootstrap, and resign cleanly. |
| `TEST-RL-TIME-002` | Distributed time | Time regulation and constrained mode are enabled before NER; TSO action/transition callbacks are visible before the matching grant. |
| `TEST-RL-SYNC-001` | Distributed sync | Bootstrap, policy and shutdown barriers release only after the required set achieves them and report failed participants. |
| `TEST-RL-GEN-001` | Negative | Stale generation, duplicate-conflict, episode regression, step regression, and excessive policy lag are rejected. |
| `TEST-RL-FED-003` | Parity | Local and federation backends yield the same semantic transition projection for the same choreography. |
| `TEST-RL-TRACE-001` | Static | Every normative requirement has a milestone, task and test; every implemented public interface and executable test has a reverse edge. |

## 3. Real-system gates

The following gates cannot be replaced by in-process fakes:

1. build and start the real `rtid` process;
2. run at least two regulating pyjevsim environment federates and one policy
   federate over gRPC;
3. prove time enablement, TSO-before-grant, same-time confluent behavior,
   timeout/halt handling, resign cleanup, and generation fencing;
4. repeat the same seed/choreography ten times and compare semantic digests;
5. checkpoint at an episode boundary, restore, and compare the remaining
   trajectory with an uninterrupted run.

Production multi-node federation failover is a future gate because the current
gorti supported baseline is a standalone authoritative `rtid`; no test result
may describe the present gossip/no-op replication path as production HA.

## 4. Performance and DEXSim comparison

Performance testing first proves semantic equality and complete delivery. Use
the same model, scenario set, seeds, hardware allocation, warm-up, measurement
count, and result projection for each contender. Report environment-steps/s,
episodes/s, p50/p95/p99 decision latency, policy lag, RTI CPU/memory/network,
learner queue depth, failures, and scaling efficiency.

The DEXSim comparison evaluates independent replicated scenarios separately
from a coupled logical-time federation. It shall not attribute a lockstep cost
to DEXSim or an independent-rollout result to gorti federation synchronization.

## 5. Entry, exit, review, and evidence

Entry requires reviewed SRS/SDD/IDD/STD versions, a frozen test configuration,
known risks, and generated FOM/bindings where applicable. Exit requires all
MVP mandatory cases passing, zero open high-severity finding, traceability
validation, and a completed review record. Flaky outcomes are
failures until root cause and corrective action are recorded.
