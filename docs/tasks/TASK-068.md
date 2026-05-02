# TASK-068: SDK Layer 2 — Rti1516eAmbassador standard-shaped adapter

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-063, TASK-064, TASK-065, TASK-066, TASK-067 |
| Blocks | none |

## Goal

Thin class mirroring the 1516-2010 Java/C++ ambassador callback API (e.g. `connect`, `joinFederationExecution`, `discoverObjectInstance`, `reflectAttributeValues`). Wraps Layer 1 internally.

## Scope (in)

- Create `pysdk/rti1516e/standard.py`.
- Create `pysdk/tests/test_standard.py`.

## Scope (out)

- New SDK functionality.

## Implements

- Requirements: IR-PYAPI-1 (Layer 2).

## TDD entry point

- Start with: subclass `Rti1516eAmbassador`, override `discoverObjectInstance`; trigger via `FakeRtiServer`; assert callback fired with expected args.

## Acceptance criteria

- [ ] Smoke tests green.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.
