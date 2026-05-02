# TASK-073: examples/pyjevsim — 2 coupled models + 1 RTI runner

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-002, TASK-059, TASK-071, TASK-072 |
| Blocks | TASK-074 |

## Goal

Reference example: two pyjevsim coupled models (`Producer` and `Consumer`); shared FOM at `tests/conformance/foms/good/pyjevsim-bridge.xml` (no copy); runner script that starts RTI, joins both federates, runs N ticks, prints final state.

## Scope (in)

- Create `examples/pyjevsim/runner.py`.
- Create `examples/pyjevsim/producer.py`.
- Create `examples/pyjevsim/consumer.py`.

## Scope (out)

- Determinism harness — TASK-074.
- Cross-language smoke — TASK-081.

## Implements

- Requirements: FR-PYJ-1.

## TDD entry point

- Start with: `python examples/pyjevsim/runner.py` runs end-to-end against live RTI; exits 0.

## Acceptance criteria

- [ ] Example runs end-to-end; exits 0.
- [ ] FOM is referenced from `tests/conformance/foms/good/pyjevsim-bridge.xml` (do not copy file).
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
