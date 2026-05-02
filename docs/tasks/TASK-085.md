# TASK-085: M5 acceptance — full cross-language federation; both modes functional (M5 final gate)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | orchestrator |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-080, TASK-081, TASK-082, TASK-083 |
| Blocks | none |

## Goal

Orchestrator-level integration acceptance task. Synthesizes results across the M5 work into the final `CHANGELOG-MASTERPLAN.md` entry that closes the MVP.

## Scope (in)

- Orchestrator writes the M5 closing entry in `CHANGELOG-MASTERPLAN.md`.
- Confirms all milestone exit criteria (M0..M5) traced to satisfying spec test files.
- No agent-side source code.

## Scope (out)

- N/A — this task is orchestrator-only by ownership.

## Implements

- Project MVP gate.
- Spec tests: all M5 spec tests under `tests/spec/M5/` are green on `main`.

## TDD entry point

- N/A — meta task.

## Acceptance criteria

- [ ] All milestone exit criteria recorded in `CHANGELOG-MASTERPLAN.md`.
- [ ] All three M5 status reports (`docs/reports/M5/agent-{a,b,c}.md`) merged.
- [ ] M5 spec tests all green on `main`.
- [ ] **Project MVP gate passed.**

## Notes / hints

- This task is orchestrator-only because it modifies frozen paths (`CHANGELOG-MASTERPLAN.md`) per `docs/ORTHOGONALITY.md` §2.
- Agents have no work in this task.
