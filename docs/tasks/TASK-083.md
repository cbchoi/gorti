# TASK-083: Determinism audit — full-repo grep + issues for findings

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-b |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-075 |
| Blocks | none |

## Goal

Audit the entire repo for non-deterministic patterns:
- Map iteration without sorted keys.
- `time.Now()` calls (golangci-lint forbidigo should already block in `rti/internal/time/`; verify elsewhere).
- RNG without explicit seed.
- `select` over multiple channels without arbitration.
- Goroutine output ordering that depends on scheduler.

For each finding, file a `bug:` GitHub issue with file/line + recommended fix. Summary recorded in `docs/reports/M5/agent-b.md`.

## Scope (in)

- The audit and its report — produces no source-code changes.
- Issues filed via `gh issue create`.
- Status report at `docs/reports/M5/agent-b.md`.

## Scope (out)

- Implementing fixes — done by the owning agent in separate tasks.

## Implements

- Requirements: NFR-DET-1, NFR-DET-2 (verification side).

## TDD entry point

- Start with: `git grep -nE 'time\.Now|map\['` and triage results.

## Acceptance criteria

- [ ] ≥1 audit pass over each top-level dir (`proto/`, `rti/`, `pysdk/`, `examples/`, `tests/`).
- [ ] Report committed to `docs/reports/M5/agent-b.md`.
- [ ] All findings have issues with reproduction context.
- [ ] `make verify` green (no source changes).
