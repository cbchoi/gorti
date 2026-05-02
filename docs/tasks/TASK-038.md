# TASK-038: examples/go-pingpong — two in-process Go federates exchange 1000 interactions

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-037 |
| Blocks | TASK-039, TASK-040 |

## Goal

Reference example: two Go federates "ping" and "pong" exchange 1000 interactions; runs to completion in <5s.

## Scope (in)

- Create `examples/go-pingpong/main.go`.
- Pingpong logic: federate "ping" sends interaction, "pong" responds, repeat 1000 times.
- Use a small inline FOM (loaded from `tests/conformance/foms/good/minimal.xml` or a new pingpong-specific FOM stored under `examples/go-pingpong/`).

## Scope (out)

- Determinism harness — TASK-039.
- Replay harness — TASK-040.

## Implements

- All M2 FR-* end-to-end.
- Spec tests: `tests/spec/M2/pingpong_test.go::TestSpec_M2_PingpongCompletes`.

## TDD entry point

- Start with: spec test that runs the example as a subprocess (or in-process) and asserts it exits 0 in <5s with 1000 interactions exchanged.

## Acceptance criteria

- [ ] Example runs <5s; exits 0.
- [ ] `make verify` green.

## Notes / hints

- May embed a minimal FOM under `examples/go-pingpong/` if needed; per `docs/ORTHOGONALITY.md` §4 reference the canonical FOM in `tests/conformance/foms/good/` rather than copying.
