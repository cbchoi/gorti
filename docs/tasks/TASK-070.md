# TASK-070: HLAFederate — DEVS time-advance bridge (NER)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-069 |
| Blocks | TASK-071, TASK-073 |

## Goal

Implement the `time_advance / output_handler / external_transition` cycle from `docs/agent-c-pysdk.md` §4.4 against a `StubCoupledModel` and `FakeRtiServer`.

Rules:
- Bridge calls `coupled_model.time_advance()` → gets DEVS `ta`.
- Issues `next_message_request(now + ta)` to RTI.
- On `TimeAdvanceGrant(t)`:
  - If `t == now + ta` (no external event arrived earlier): run internal cycle (`output_handler` → `internal_transition`).
  - If `t < now + ta` (external arrived first): deliver via `external_transition`, no internal cycle.
- Drain output ports → send as interactions. Loop.

## Scope (in)

- Create `pysdk/pyjevsim_bridge/time_advance.py`.
- Create `pysdk/tests/test_time_advance.py` with `StubCoupledModel`.

## Scope (out)

- Select preservation — TASK-071.

## Implements

- Requirements: FR-PYJ-3.

## TDD entry point

- Start with: `ta=2.0`, no incoming events → assert `next_message_request(now + 2.0)` issued; on grant, `internal_transition` called.

## Acceptance criteria

- [ ] All sequence tests in brief §5.5 green.
- [ ] No blocking calls into pyjevsim (use `asyncio.to_thread` if pyjevsim is sync per brief §7).
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
