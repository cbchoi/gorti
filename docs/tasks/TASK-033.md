# TASK-033: ObjectRegistry — interaction send/receive path

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M2 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-031 |
| Blocks | TASK-036 |

## Goal

`SendInteraction` encodes parameters via `rti/pkg/encoding`, writes to event log, fans out `ReceiveInteraction` to subscribers. Symmetric to TASK-032 for interactions.

## Scope (in)

- Create `rti/internal/object/interaction.go`.

## Scope (out)

- Update path — TASK-032.

## Implements

- Requirements: FR-OM-5, IR-PROTO-3.
- Spec tests: `tests/spec/M2/object_test.go::TestSpec_M2_SendReceiveInteraction_*`.

## TDD entry point

- Start with: subscribe to interaction class, send, assert receive arrives at all subscribers with correct parameter bytes.

## Acceptance criteria

- [ ] Spec tests green.
- [ ] `make verify` green.
