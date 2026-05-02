# TASK-020: FederationStore.CreateFederation

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-019 |
| Blocks | TASK-021, TASK-022, TASK-023, TASK-034 |

## Goal

Implement `core.FederationStore.CreateFederation` — calls `rti/pkg/fom` for parsing/validation; rejects on diagnostics; persists federation metadata; returns deterministic federation handle.

## Scope (in)

- Create `rti/internal/federation/manager.go` with `Manager` type implementing `core.FederationStore`. For this task, implement only `CreateFederation`; stub the other interface methods with `core.ErrNotImplemented` (or panic with `"TASK-021"` etc. — clearly attributed).

## Scope (out)

- `JoinFederation` — TASK-021.
- `ResignFederation` — TASK-022.
- `DestroyFederation` — TASK-023.
- gRPC-handler wiring — TASK-034.

## Implements

- Requirements: FR-FM-1.
- Spec tests: `tests/spec/M2/federation_test.go::TestSpec_M2_CreateFederation_*` (orchestrator pre-work).

## TDD entry point

- Start with: orchestrator-provided spec test for `CreateFederation` happy path + at-least-one rejection-on-bad-FOM case.

## Acceptance criteria

- [ ] M2 federation create spec tests pass.
- [ ] `go test ./rti/internal/federation/...` green.
- [ ] Coverage on owned files ≥ 75%.
- [ ] `make verify` green.

## Notes / hints

- **Pre-dispatch prerequisite:** `tests/spec/M2/` must exist on `main` (orchestrator pre-work). Until then, this TASK is dispatched but blocked.
- Use `rti/pkg/fom/parser.Parse` + `rti/pkg/fom/mim.Merge` to validate the FOM modules supplied in `CreateFederationRequest`.
