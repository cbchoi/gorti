# TASK-035: gRPC handlers — DeclarationService

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-029 |
| Blocks | TASK-037 |

## Goal

Implement all 8 declaration RPCs per `proto/rti/v1/declaration.proto`.

## Scope (in)

- Create `rti/internal/transport/grpc/declaration.go`.

## Scope (out)

- Other services.

## Implements

- Requirements: IR-PROTO-1.
- Spec tests: `tests/spec/M2/grpc_declaration_test.go::TestSpec_M2_GRPC_Declaration_*`.

## TDD entry point

- Start with: PublishObjectClassAttributes happy path.

## Acceptance criteria

- [ ] All 8 RPCs implemented.
- [ ] Each error code reachable from a defined input.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.
