# TASK-036: gRPC handlers — ObjectService + StreamService (bidi data plane)

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-032, TASK-033 |
| Blocks | TASK-037 |

## Goal

Implement `ObjectService` (3 RPCs) and `StreamService.Events` server-streaming for the data-plane fanout.

## Scope (in)

- Create `rti/internal/transport/grpc/object.go`: `RegisterObjectInstance`, `UpdateAttributeValues`, `SendInteraction`.
- Create `rti/internal/transport/grpc/stream.go`: `Events` server-streaming — dispatches inbound updates and fans out to subscribed streams.

## Scope (out)

- Other services.

## Implements

- Requirements: IR-PROTO-2, IR-PROTO-3.
- Spec tests: `tests/spec/M2/grpc_object_test.go::TestSpec_M2_GRPC_Object_*`, `tests/spec/M2/grpc_stream_test.go::TestSpec_M2_GRPC_Stream_Fanout`.

## TDD entry point

- Start with: register-update-stream sequence test asserting bidirectional fanout to N>1 subscribers in deterministic order.

## Acceptance criteria

- [ ] Spec tests green.
- [ ] Bidi-stream: one goroutine per federate connection (do not introduce worker pools per `docs/agent-a-rti-core.md` §7).
- [ ] `make verify` green.
