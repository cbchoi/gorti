# TASK-032: ObjectRegistry — update/reflect path

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-031 |
| Blocks | TASK-026, TASK-036 |

## Goal

`UpdateAttributeValues` encodes attribute values via `rti/pkg/encoding`, writes to event log, fans out `ReflectAttributeValues` to subscribed federate streams in deterministic order.

## Scope (in)

- Create `rti/internal/object/update.go`.

## Scope (out)

- Interaction path — TASK-033.

## Implements

- Requirements: FR-OM-3, FR-OM-4, IR-PROTO-2.
- Spec tests: `tests/spec/M2/object_test.go::TestSpec_M2_UpdateReflect_*`.

## TDD entry point

- Start with: register, update, assert reflect arrives at all subscribers with correct attribute bytes.

## Acceptance criteria

- [ ] Spec tests green.
- [ ] Encode happens via `rti/pkg/encoding.CodecFor(...)` — no inline encoding.
- [ ] Write-ahead: event log entry committed before fanout.
- [ ] `make verify` green.

## Notes / hints

- **Wave dispatch**: this task is part of `docs/M2_DISPATCH_PLAN.md` — confirm the wave (W1A/W1B/W1C/W2A/W2B/W3A/W3B/W3C/W4) and respect file ownership for parallel orthogonality.
