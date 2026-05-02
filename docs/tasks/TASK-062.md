# TASK-062: Generated gRPC client + Makefile codegen target

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | none |
| Blocks | TASK-063 |

## Goal

Populate `pysdk/rti1516e/_generated/` from `proto/rti/v1/*.proto` via `grpc_tools.protoc`. The directory is gitignored. Provide a build wrapper.

## Scope (in)

- Create `pysdk/rti1516e/_proto.py`: tiny build-script wrapper invoking `python -m grpc_tools.protoc ...`.
- Create `pysdk/Makefile.codegen`: codegen target spec.
- Create `pysdk/tests/test_codegen_smoke.py`: imports a generated symbol and asserts presence.

## Scope (out)

- Hand-editing generated stubs (forbidden — regenerate instead).
- Wiring `Makefile.codegen` into the root `Makefile` — that's an orchestrator-only edit (root Makefile is frozen). The orchestrator merges separately via a `chore(make):` PR.

## Implements

- Requirements: IR-PROTO-1.

## TDD entry point

- Start with: smoke test that imports `_generated.federation_pb2` and asserts the `CreateFederationRequest` message class exists.

## Acceptance criteria

- [ ] `cd pysdk && python -m grpc_tools.protoc ...` produces `_generated/`.
- [ ] Smoke test green.
- [ ] `make verify` green (codegen target is a non-fatal hook in CI; smoke test exercises it).

## Notes / hints

- Generated code is gitignored (`docs/ORTHOGONALITY.md` §2).
