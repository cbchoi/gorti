# M3 Dispatch Plan

How the orchestrator dispatches the 9 M3 tasks (TASK-041..049) to maximize parallel sub-agent throughput while keeping every wave orthogonal at the file level.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md` (general protocol), `docs/agent-a-rti-core.md` (Agent A brief), `docs/M2_DISPATCH_PLAN.md` (the proven M2 wave model that this mirrors), `docs/MILESTONE_CHECK.md` (probe definitions), `docs/srs.md` §10.2 (M3 exit criteria).

---

## 1. Why a wave model

M3 has 9 tasks, all assigned to Agent A in `docs/agent-a-rti-core.md`. Compared to M2 (21 tasks, 4 waves), M3 is smaller but more sequential — NER depends on regulation state which depends on the per-federate state machine, and stall detection layers on top. The wave model still wins because the LBTS pure function and the regulation state machine are independent of the lookahead enforcement and the example/harness work.

The same 4-wave shape that drove M2 to DONE in ~30 minutes wall-time applies here, with a smaller fan-out.

## 2. Wave structure

```
                 (M0 + M1 + M2 already DONE)
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 1 (2 parallel sub-agents — no upstream deps)  │
   │   W1A — time/regulation.go   (TASK-041)            │
   │   W1B — time/lbts.go         (TASK-042)            │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 2 (1 sub-agent — depend on Wave 1)            │
   │   W2 — time/ner.go + lookahead.go (TASK-043, 044)  │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 3 (1 sub-agent — depend on Wave 2)            │
   │   W3 — time/stall.go         (TASK-045)            │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 4 (1 sub-agent — integration; M3 gate)        │
   │   W4 — examples/go-timed/* + harnesses             │
   │        (TASK-046, 047, 048, 049)                   │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
                    M3 DONE per srs.md §10.2
```

Critical path: 4 waves. Wall-time ~25–35 minutes vs. ~50 for strict serial.

## 3. File ownership per wave

The TDD-friendly file decomposition. Within each wave, sub-agents touch disjoint files; the only cross-wave shared files are the orchestrator-seeded stubs (which sub-agents EXTEND but do not RESHAPE).

### Wave 1 — two independent components

| Sub-agent | Tasks | Owned files | Spec tests turned green |
|---|---|---|---|
| **W1A** regulation state machine | TASK-041 | `rti/internal/time/manager.go` (extend body of EnableRegulation/DisableRegulation/EnableConstrained/DisableConstrained) + new files for per-federation state at agent's discretion (e.g. `regulation.go`, `state.go`). | `regulation_test.go` (10 tests) |
| **W1B** LBTS pure function | TASK-042 | `rti/internal/time/lbts.go` (extend body). Pure function — no I/O, no concurrency. | `lbts_test.go` (6 property tests) |

W1A and W1B share zero files. Both can run in one parallel batch.

**Note:** W1A's `New(opts Options)` constructor body is shared scaffolding both later waves depend on. W1A owns the constructor and ships it returning `nil` instead of `ErrNotImplemented`.

### Wave 2 — NER request handling + lookahead enforcement

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W2** NER + lookahead | TASK-043, TASK-044 | `rti/internal/time/manager.go` (extend body of NextMessageRequest) + new files (e.g. `ner.go`, `lookahead.go`). | Regulation state (W1A), LBTS function (W1B), `core.Outbox`, `core.EventLog` (consume — already on `main`). |

Single sub-agent because TASK-043 (NER) and TASK-044 (lookahead enforcement) modify the same code path; splitting them would force a stub-on-stub dance. Both turn green together.

Spec tests turned green: `ner_test.go` (6 tests).

### Wave 3 — stall detection

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W3** stall detection | TASK-045 | `rti/internal/time/manager.go` (extend body of CheckStalls) + new file (e.g. `stall.go`). | NER (W2) — stall detection inspects per-federate "last NER timestamp"; that field is added by W2. |

Spec tests turned green: `stall_test.go` (6 tests).

### Wave 4 — integration + M3 gate

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W4** | TASK-046, TASK-047, TASK-048, TASK-049 | `examples/go-timed/main.go`, `examples/go-timed/determinism_test.go`, `examples/go-timed/stall_test.go`, `examples/go-timed/replay_test.go`. |

Single sub-agent because the integration is sequential by nature (build example → run determinism harness → run stall harness → run replay harness).

Spec tests turned green: `replay_test.go`, `determinism_test.go` (both currently scaffold-skips; W4 unskips them).

## 4. Spec test mapping

The orchestrator-frozen `rti/spec/M3/` spec tests target each component. Each wave's sub-agent verifies their work by turning specific tests green:

| Spec test file | Turns green at end of |
|---|---|
| `regulation_test.go` (10 tests) | Wave 1 (W1A) |
| `lbts_test.go` (6 property tests) | Wave 1 (W1B) |
| `ner_test.go` (6 tests) | Wave 2 (W2) |
| `stall_test.go` (6 tests) | Wave 3 (W3) |
| `replay_test.go` (currently 2 skip-scaffolds) | Wave 4 (W4) |
| `determinism_test.go` (currently 2 skip-scaffolds) | Wave 4 (W4) |

All spec tests live in `rti/spec/M3/` (mirroring M2's convention; Go's `internal` package rule blocks `tests/...` from importing `rti/internal/*`). The `scripts/check-milestones.sh` M3 probe will look at `rti/spec/M3/` (orchestrator extends the script as part of dispatch).

## 5. Hard rules per wave

These apply per `docs/DISPATCH.md` §4 (no self-selection, no multi-task PRs, etc.) plus M3-specific:

1. **Stub signature freeze**. Every method declared in the orchestrator-seeded `rti/internal/time/manager.go` (the 5 `core.TimeManager` methods + `CheckStalls`) is part of the M3 contract. Sub-agents may reshape internal helpers but must not change exported signatures without a `contract-change-request:` issue per `docs/WORKFLOW.md` §4.4.

2. **Compile-time interface assertion stays**. `manager.go` ends with `var _ core.TimeManager = (*Manager)(nil)`. Removing it is a contract-change.

3. **No wall-clock calls**. `golangci-lint` `forbidigo` blocks `time.Now()` in `rti/internal/time/`. All wall-clock reads go through `m.opts.Clock`. This is the determinism guarantee — tests inject `core.FakeClock`, and replay byte-identicality depends on every time decision being recorded, not recomputed.

4. **No cross-wave file edits**. A Wave-3 sub-agent must NEVER edit a file owned by Wave 1 (regulation, LBTS) or Wave 2 (NER). If a bug is discovered there, file an issue and continue. The exception: all waves modify `manager.go`'s method bodies (each owns specific methods) — this is allowed because the orchestrator-seeded shape carves clean boundaries.

5. **Sentinels per task**. Each task gets its own `docs/tasks/signals/TASK-NNN.done`. Bundled sub-agent work (e.g. W2 covering TASK-043+044) produces ALL bundled sentinels in the final commit.

6. **CheckStalls is poll-based, not goroutine-based** (the spec choice in `manager.go` doc). W3 must NOT spawn a background goroutine that calls `time.Sleep` and `CheckStalls` itself; that's `cmd/rtid`'s job (Wave 4 wires it). Tests advance `FakeClock` and then call `CheckStalls` directly. Determinism requires this discipline.

## 6. Verification activities (gate-time, not dispatched as TASK-NNN)

Per `docs/AGENTS.md` §6.2 + `docs/agent-{b,c}-*.md` §5:

- **Agent B at M3 gate**: extend the determinism audit to time-management code paths; flag any LBTS computation that iterates a Go map, any NER queue using a non-sorted heap, any stall check using `time.Now()` directly. File `verification:M3` issue.
- **Agent C at M3 gate**: write a Python-side smoke test that joins a federation, enables regulation with a small lookahead, calls NER repeatedly; confirms grants arrive in deterministic order. (This is also Agent C's M4 prep — the same harness gets reused.) File `verification:M3` issue.

These run after Wave 4 completes; they are not part of the wave model.

## 7. Dispatch order checklist (orchestrator's runbook)

Step-by-step the orchestrator follows for M3:

1. **Pre-work confirmation** (this commit):
   - `rti/internal/time/{doc.go,manager.go,lbts.go}` stubs on `main`
   - `rti/spec/M3/*.go` spec tests on `main`, RED for the right reason (`ErrNotImplemented` from `time.New`)
   - `scripts/check-milestones.sh` to be updated to probe `rti/spec/M3/` (orchestrator follow-up before W1 dispatch)
2. **Wave 1**: spawn W1A + W1B in one parallel `Agent` tool call. Wait for both to push branches. Review + merge regulation first (W1A), then LBTS (W1B); minimizes conflicts (none expected — disjoint files).
3. **Wave 2**: spawn W2 (single sub-agent for NER + lookahead).
4. **Wave 3**: spawn W3 (single sub-agent for stall detection).
5. **Wave 4**: spawn W4. This sub-agent does the most work (4 tasks) but it's all integration with no parallel partner.
6. **M3 gate**: re-run `scripts/check-milestones.sh`. Should report `M1: DONE`, `M2: DONE`, `M3: DONE` (green). Push tag `m3` (optional).

## 8. Risk mitigations

| Risk | Mitigation |
|---|---|
| Sub-agent reshapes a stub signature | Pre-commit `check-frozen-paths.sh` + orchestrator review; reject PR if `var _ core.TimeManager = (*Manager)(nil)` is removed or modified |
| Wave 2 introduces a wall-clock call sneaking past forbidigo | CI runs `golangci-lint` per `docs/WORKFLOW.md` §3; PR fails if `time.Now` appears anywhere under `rti/internal/time/` |
| Stall detection becomes flaky because the example wires a real-clock goroutine | W4 brief explicitly: `cmd/rtid` integration uses `time.Ticker` to call `CheckStalls(ctx)`; the example uses `FakeClock` and explicit `Advance + CheckStalls` |
| LBTS test catches a determinism bug only after grant ordering also breaks | `lbts_test.go::OrderIndependent` is a property test — runs base + reverse + sorted-by-handle; W1B PR must pass it before merge |
| Replay test becomes flaky because of timer ordering | M3 replay (TASK-049) covers ONLY logical-time events; FederationHalted records the wall-clock decision so the replayer doesn't recompute it |
