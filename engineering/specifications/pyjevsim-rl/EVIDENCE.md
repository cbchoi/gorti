# Verification Evidence

Document ID: `EVID-PYJEVSIM-RL-001`
Captured: 2026-07-19

## Baseline inventory

- 84 unique SRS requirement identifiers.
- 51 unique milestone task identifiers across MS-RL-00 through MS-RL-08.
- Requirement-to-milestone/task/test mappings are recorded in
  `traceability.yaml`.
- The documents remain a draft extension and have not replaced the
  authoritative gorti v4 baseline.

## Executed evidence

| Evidence | Result |
|---|---|
| RL environment, executor, federation, local pool, and strengthened legacy time bridge tests | 67 passed |
| Real upstream pyjevsim 2.1.2 `SysExecutor` confluent/reset test | 1 passed |
| Upstream pyjevsim 2.1.2 HLA-step/confluent semantic suite | 9 passed |
| Strict type analysis of `pyjevsim_bridge.rl` and `time_advance.py` | success, no issues in 9 source files |
| Ruff check of changed Python modules and tests | all checks passed |
| RL FOM parse and interaction/parameter lookup | included in the 54-test suite |
| SRS-to-traceability audit | 84 requirements, all mapped to milestone, task, and test IDs |
| Dependency lock check | resolved successfully; pyjevsim 2.1.2 at commit `9893099b47aae89a3432c668b18ec4b6b15c043e` |
| Git whitespace/error check | clean |

The real-pyjevsim test used the reviewed upstream source revision directly,
because the already-existing verification environment contains the older
2.0.1 package and 2.1.2 is not yet published to the package index. The locked
optional dependency makes the qualified Git revision reproducible.

## Evidence limits

This evidence qualifies an architectural vertical slice, not every exit gate
in `MILESTONES.md`. No production claim is made yet for cross-process scale,
DDM, bulk artifact exchange, save/restore recovery, federation-wide run
management, security hardening, platform matrix, or multi-node RTI failover.
Those capabilities retain explicit task and acceptance-test IDs so subsequent
developers can execute and audit them without redefining the baseline.

Known qualification deviation: the pinned pyjevsim revision exposes stale
model-local time inside transition/output callbacks. The MVP supports models
that use executor/`StepView` committed time and explicitly does not qualify
models that depend on callback-local `model.global_time`; see `RSK-RL-013`.
