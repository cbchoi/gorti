# TASK-069: HLAFederate adapter — port mapping

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-067 |
| Blocks | TASK-070, TASK-071, TASK-072 |

## Goal

Bootstrap `pysdk/pyjevsim_bridge/`. Implement `PortMapping` — explicit port-name → FOM-interaction-class mapping per `docs/agent-c-pysdk.md` §4.4.

## Scope (in)

- Create `pysdk/pyjevsim_bridge/__init__.py` (empty).
- Create `pysdk/pyjevsim_bridge/port_mapping.py`.
- Create `pysdk/tests/test_port_mapping.py`.

## Scope (out)

- Time advance — TASK-070.
- Select preservation — TASK-071.

## Implements

- Requirements: FR-PYJ-2.

## TDD entry point

- Start with: build `PortMapping({"out_port": "demo.Tick"})`, look up `out_port` → expect `"demo.Tick"`.

## Acceptance criteria

- [ ] Test green.
- [ ] Auto-generation of mapping is **not** in scope (deferred per brief §4.4).
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
