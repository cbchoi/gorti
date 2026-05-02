# TASK-072: pyjevsim API drift smoke test

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-069 |
| Blocks | TASK-073 |

## Goal

Add a smoke test that imports specific pyjevsim symbols (`CoupledModel`, `AtomicModel`, `select`, `ta`, `output_handler`) and exercises a small protocol. On version drift that breaks any symbol, test fails loudly with a diagnostic naming the missing symbol. (The pyjevsim version pin itself was added to `pyproject.toml` in TASK-050; this task does not modify `pyproject.toml`.)

## Scope (in)

- Create `pysdk/tests/test_pyjevsim_smoke.py`.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-PYJ-1 (version stability).

## TDD entry point

- Start with: import each symbol; `assert hasattr(...)` per symbol with a meaningful failure message.

## Acceptance criteria

- [ ] Test green.
- [ ] Deliberate local version-bump experiment (uncommitted) makes test fail loudly with the missing-symbol name.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.

## Notes / hints

- Per `docs/agent-c-pysdk.md` §7 anti-goals: do not monkey-patch pyjevsim.
