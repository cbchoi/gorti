# TASK-043: NER request handling

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a |
| Milestone | M3 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-042 |
| Blocks | TASK-044, TASK-045 |

## Goal

`NextMessageRequest` enqueues a request, recomputes LBTS, grants `TimeAdvanceGrant` when grant condition is met (LBTS ≥ requested time).

## Scope (in)

- Create `rti/internal/time/ner.go`.

## Scope (out)

- TAR (cut 2; explicitly out per `docs/agent-a-rti-core.md` §7).
- Lookahead enforcement — TASK-044.

## Implements

- Requirements: FR-TM-2, FR-TM-4.
- Spec tests: `tests/spec/M3/ner_test.go::TestSpec_M3_NER_*`.

## TDD entry point

- Start with: sequence test with `FakeClock` — enable regulation, request advance, observe grant arrival in deterministic order.

## Acceptance criteria

- [ ] Spec test sequences green.
- [ ] No `time.Now()` calls — use `core.Clock` (forbidigo lint enforces).
- [ ] `make verify` green.

## Notes / hints

- Use sequence tests with `FakeClock`: list of `(action, expected)` tuples, each one assertion. Failures localize per `docs/agent-a-rti-core.md` §5.5.
