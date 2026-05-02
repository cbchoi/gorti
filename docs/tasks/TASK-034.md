# TASK-034: gRPC handlers — FederationService

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-023 |
| Blocks | TASK-037 |

## Goal

Implement `FederationService` (CreateFederation, DestroyFederation, JoinFederation, ResignFederation, ListFederations) per `proto/rti/v1/federation.proto`.

## Scope (in)

- Create `rti/internal/transport/grpc/federation.go`.
- Each RPC: maps proto request → core call → maps result/error to proto response.
- Each documented error code reachable from a defined input.

## Scope (out)

- Other services.

## Implements

- Requirements: IR-PROTO-1.
- Spec tests: `tests/spec/M2/grpc_federation_test.go::TestSpec_M2_GRPC_Federation_*`.

## TDD entry point

- Start with: handler-level test — happy CreateFederation produces expected response.

## Acceptance criteria

- [ ] Spec tests green.
- [ ] Each error in `proto/rti/v1/errors.proto` reachable from at least one test input.
- [ ] Handlers use small inline fakes of `core.FederationStore` (NOT mocking frameworks) per `docs/agent-a-rti-core.md` §5.5.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.
