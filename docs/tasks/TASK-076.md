# TASK-076: --mode=verbose vs --mode=best-effort flag at federation create

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M5 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-049 |
| Blocks | TASK-077, TASK-082 |

## Goal

Federation-create RPC accepts `mode` field per `core.Mode` enum (already in M0); the `--mode` CLI flag at `cmd/rtid` plumbs through to `CreateFederationRequest`.

## Scope (in)

- Create `rti/internal/transport/grpc/mode.go`: thin field plumbing in CreateFederation handler + flag wiring in `cmd/rtid`.

## Scope (out)

- Best-effort RO semantics — TASK-077.

## Implements

- Requirements: FR-OM-3 (mode plumbing); NFR-PERF-1..4 (groundwork).
- Spec tests: `tests/spec/M5/mode_flag_test.go::TestSpec_M5_ModeFlag`.

## TDD entry point

- Start with: create federation with `mode: best-effort`; query federation summary; assert mode == best-effort.

## Acceptance criteria

- [ ] Spec test green.
- [ ] `make verify` green.
