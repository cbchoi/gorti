# TASK-081: Cross-language smoke

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-075, TASK-078 |
| Blocks | none |

## Goal

One pyjevsim federate (Python) + one Go federate (reuse `examples/go-pingpong` test federate) joined to the same federation. Exchange interactions; both observe consistent state.

## Scope (in)

- Create `examples/pyjevsim/cross_lang_test.py`. Python side launches a pre-built `rtid` binary as subprocess + invokes the Go pingpong federate as a subprocess via flags.

## Scope (out)

- Editing any Go source — Agent C does not modify Agent A's files. Reuse `examples/go-pingpong/main.go` as a binary; no source change.

## Implements

- End-to-end M5 cross-language goal.
- Spec tests: `tests/spec/M5/cross_lang_test.go::TestSpec_M5_CrossLanguage_*` (orchestrator pre-work; Go-side; this task's Python test is the consumer side).

## TDD entry point

- Start with: launch RTI; launch Go pingpong as subprocess; have Python pyjevsim federate join; exchange 100 interactions; assert event log shows both federate handles.

## Acceptance criteria

- [ ] Test green.
- [ ] Test does NOT modify any Go source — invokes built binaries only.
- [ ] mypy/ruff clean on Python side.
- [ ] `make verify` green.

## Notes / hints

- Per `docs/ORTHOGONALITY.md` §1.2, runtime dependence on Go binaries does not cross orthogonality boundary; it is consumer-only access.
