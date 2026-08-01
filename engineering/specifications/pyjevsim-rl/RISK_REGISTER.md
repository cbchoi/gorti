# pyjevsim RL Risk Register

Version: 0.1-draft  
Updated: 2026-07-19

| ID | Risk | Initial | Control / verification | Residual target |
|---|---|---:|---|---:|
| `RSK-RL-001` | Driving atomic model methods directly diverges from pyjevsim scheduler semantics | High | Canonical `SysExecutor` seam; confluent/zero-cascade golden tests | Low |
| `RSK-RL-002` | In-place reset leaks model, queue or clock state across episodes | High | Rebuild factory default; ten-run isolation/determinism test | Low |
| `RSK-RL-003` | Existing bridge requests time without enabling regulation/constrained mode | High | Explicit enable handshake and real-rtid integration gate | Low |
| `RSK-RL-004` | Existing adapter collapses multiple same-port messages | High | Preserve executor event batches; multi-message conformance test | Low |
| `RSK-RL-005` | Global lockstep lets a slow independent rollout stall training | High | Independent/bounded-lag modes; use barriers only for phases | Medium |
| `RSK-RL-006` | Large tensors/checkpoints saturate RTI data paths | High | External immutable artifact plane; HLA transfers reference+hash | Low |
| `RSK-RL-007` | Stale worker contaminates a recreated run | High | Federation generation, episode/step/policy identities and fencing | Low |
| `RSK-RL-008` | Single authoritative `rtid` is a bottleneck and failure point | High | Capacity metrics, sharding by independent federation; clustered HA is not claimed | Medium |
| `RSK-RL-009` | Current cluster gossip/no-op replication is mistaken for production HA | High | Explicit scope and future real-failover milestone/test gate | Low |
| `RSK-RL-010` | Unauthenticated worker or artifact executes untrusted code | High | TLS, role authorization, signed/hash-verified artifacts, isolation | Medium |
| `RSK-RL-011` | pyjevsim release drift changes executor behavior | Medium | Version compatibility matrix and semantic golden traces | Low |
| `RSK-RL-012` | Async batching hides loss, duplicates or ambiguous completion | High | Bounded queues, idempotency keys, confirmed boundaries and accounting | Low |
| `RSK-RL-013` | pyjevsim 2.1.2 model-local `global_time` is stale inside output/transition callbacks | High | Bind RL time to executor commit/`StepView`; reject plugins that read model-local callback time until upstream fix and conformance gate | Medium |

Risk owners and due dates are assigned in the milestone task record. A high
residual risk blocks release unless an approving authority records an expiring
deviation and compensating control.
