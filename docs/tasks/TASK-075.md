# TASK-075: M4 lint/coverage gate (M4 milestone gate)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-074 |
| Blocks | TASK-078, TASK-081 |

## Goal

Bring the entire pysdk to acceptance-criteria lint/coverage state: `mypy --strict` clean, `ruff check` clean, coverage ≥ 80% on owned packages, SDK importable from a fresh venv.

## Scope (in)

- Create `pysdk/tests/test_lint_strict.py` (CI smoke: invokes mypy + ruff and fails the suite on any error).
- Add unit tests as needed across owned files to reach coverage threshold.

## Scope (out)

- Cross-language smoke — TASK-081.
- Mode tests — TASK-082.

## Implements

- M4 exit criteria #4..7.

## TDD entry point

- Start with: run `mypy --strict pysdk/` locally; fix any errors. Run `ruff check pysdk/`; fix.

## Acceptance criteria

- [ ] `mypy --strict pysdk/` clean.
- [ ] `ruff check pysdk/` clean.
- [ ] `pytest pysdk/` coverage ≥ 80% on owned packages.
- [ ] SDK importable from fresh venv (no missing deps).
- [ ] **This is the M4 milestone gate task.** Passing it triggers `verification:M4` activities by Agent A and Agent B.
- [ ] `make verify` green.
