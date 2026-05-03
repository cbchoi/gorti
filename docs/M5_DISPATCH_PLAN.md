# M5 Dispatch Plan

How the orchestrator dispatches the 9 active M5 tasks (TASK-076..083 + TASK-085; TASK-084 is CANCELLED) plus the closing orchestrator gate. **First multi-agent milestone**: Agents A, B, C all participate concurrently.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md`, `docs/agent-{a,b,c}-*.md`, prior dispatch plans (`docs/M2_DISPATCH_PLAN.md`, `docs/M3_DISPATCH_PLAN.md`, `docs/M4_DISPATCH_PLAN.md`), `docs/srs.md` §10.2 (M5 exit criteria).

---

## 1. Why a wave model

M5 has 9 active tasks across 3 agents:
- Agent A (Go RTI core): TASK-076 + 077 + 078 + 079 + 080 (mode flag, best-effort RO, hardening, perf harness, perf run)
- Agent B (FOM + encoding): TASK-083 (determinism audit; produces issues + report, no source code)
- Agent C (Python SDK + bridge): TASK-081 + 082 (cross-language smoke, modes verification)
- Orchestrator: TASK-085 (M5 close + MVP gate)

Cross-agent path ownership is fully disjoint per `docs/ORTHOGONALITY.md` §2 — A writes only `rti/`, B writes only `rti/pkg/` + `tests/conformance/` + `docs/reports/M5/agent-b.md`, C writes only `pysdk/` + `examples/pyjevsim/` + `docs/reports/M5/agent-c.md`. So the WAVE structure can be aggressively parallel: multiple agents work simultaneously without collision.

Compared to prior milestones (single-agent waves), M5 is the first to use **inter-agent parallelism**.

## 2. Pre-work confirmation (this commit)

Before Wave 1 dispatches, the orchestrator delivers:

- **`rti/spec/M5/`** spec tests (Go-side; orchestrator-frozen):
  - `doc.go`, `fixtures.go` (permissive FOM repo + event log + recording outbox + minimal FOM XML)
  - `mode_flag_test.go` — TASK-076 contract (default=Verbose; BestEffort persists)
  - `best_effort_test.go` — TASK-077 contract (skip-scaffold; W2A flips to GREEN)
  - `perf_test.go` — TASK-079 contract (asserts perf.Manager.RunBaseline schema; skips on stub)
  - `soak_test.go` — TASK-078 contract (build tag `soak`; t.Fatalf scaffold)
  - `cross_lang_test.go` — TASK-081 orchestration scaffold (Agent C-side leadership)
- **`pysdk/tests/spec/m5/`** spec tests (Python-side; orchestrator-frozen):
  - `__init__.py`
  - `test_spec_m5_modes.py` — TASK-082 contract (skip-scaffold)
  - `test_spec_m5_cross_language.py` — TASK-081 contract (skip-scaffold)
- **`rti/internal/perf/`** stubs:
  - `doc.go`, `baseline.go` (Manager + Options + Result + JSON schema FROZEN; constructor returns ErrNotImplemented)
- **`docs/reports/M5/.gitkeep`** so the per-agent status report directory exists
- **`scripts/check-milestones.sh`** M5 probe re-pointed at `rti/spec/M5/` + `pysdk/tests/spec/m5/`

Pre-dispatch state: `go test ./rti/spec/M5/...` shows mode_flag tests RED with `errors.New("federation: ...required")` (existing federation.New validates the Options it now receives correctly) — actually the tests skip via `t.Skip` when `federation.New` returns a nil mgr, so the RED state is "skipped as expected." `perf_test.go` skips with `ErrNotImplemented` from `perf.New`. Skip-scaffolds skip explicitly. `pytest pysdk/tests/spec/m5/` shows skip-scaffolds skipping. M0/M1/M2/M3/M4 stay green.

## 3. Wave structure

```
                 (M0..M4 already DONE; orchestrator pre-work landed)
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 1 (3 PARALLEL — ACROSS THREE AGENTS)                          │
   │   W1A — Agent A — TASK-076 + TASK-077                              │
   │           (mode flag CLI wiring + best-effort RO semantics)        │
   │   W1B — Agent B — TASK-083                                         │
   │           (determinism audit; produces issues + agent-b.md;        │
   │            no source-code changes)                                 │
   │   W1C — Agent C — TASK-081                                         │
   │           (cross-language smoke; needs real-gRPC transport         │
   │            in Python SDK + real-pyjevsim adapter — bundles M4      │
   │            follow-ups into this task as documented)                │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 2 (2 PARALLEL — depend on Wave 1)                             │
   │   W2A — Agent A — TASK-078 + TASK-079 + TASK-080                   │
   │           (hardening soak + perf harness + perf baseline run +     │
   │            agent-a.md report)                                      │
   │   W2B — Agent C — TASK-082                                         │
   │           (modes verification; depends on W1A's TASK-077 +         │
   │            W1C's real-gRPC transport)                              │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────────────────────┐
   │ Wave 3 (orchestrator — M5 final gate)                              │
   │   W3 — TASK-085                                                    │
   │           (orchestrator writes M5 closing CHANGELOG entry,         │
   │            confirms all milestone exit criteria, MVP gate)         │
   └────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
                M5 DONE per srs.md §10.2 → MVP achieved
```

Critical path: 3 waves. Wall-time estimate ~45–60 min sub-agent compute (smaller than M4 because tasks-per-wave is lower; larger than M3 because TASK-081 cross-language work is hefty).

## 4. File ownership per wave

### Wave 1 — three independent components, three agents

| Sub-agent | Owner | Tasks | Owned files |
|---|---|---|---|
| **W1A** mode + best-effort | Agent A | TASK-076, TASK-077 | `rti/cmd/rtid/main.go` (extend mode CLI flag; mode is already in proto + core), `rti/internal/transport/grpc/mode.go` (NEW), `rti/internal/transport/grpc/best_effort.go` (NEW), `rti/internal/object/update.go` (extend with mode-aware delivery decision; W2A from M2 already exists), `rti/internal/object/interaction.go` (same) |
| **W1B** determinism audit | Agent B | TASK-083 | `docs/reports/M5/agent-b.md` (NEW; status report). Issues filed via `gh issue create`; no source code in this task. |
| **W1C** cross-language smoke | Agent C | TASK-081 | `examples/pyjevsim/cross_lang_test.py` (NEW). Plus M4 follow-ups bundled: real-gRPC transport in `pysdk/rti1516e/connection.py` (extend the `_transport.py` registry to dispatch `grpc://` URLs to a real grpc.aio channel using the generated stubs from `pysdk/rti1516e/_generated/`); real-pyjevsim adapter in `examples/pyjevsim/_real_pyjevsim.py` (NEW; adapts `pyjevsim.SysExecutor` + `StructuralModel` to `CoupledModelProtocol`). Also: `examples/pyjevsim/cross_lang_runner.py` (NEW; subprocess orchestrator). |

W1A, W1B, W1C share zero files. All three can run in one parallel batch.

### Wave 2 — perf baseline + Python modes verification

| Sub-agent | Owner | Tasks | Owned files | Dependencies |
|---|---|---|---|---|
| **W2A** hardening + perf | Agent A | TASK-078, TASK-079, TASK-080 | `rti/internal/transport/grpc/load_test.go` (NEW; build tag `soak`), `rti/internal/perf/baseline.go` (extend; orchestrator pre-work landed the stub), `rti/internal/perf/baseline_test.go` (NEW), `examples/go-pingpong/perf_main.go` (NEW; build tag `perf`), `docs/reports/M5/agent-a.md` (NEW) | W1A's TASK-077 best-effort path on main; W1C's cross-lang smoke on main (so the soak test exercises real cross-language load). |
| **W2B** modes verification | Agent C | TASK-082 | `pysdk/tests/test_modes.py` (NEW; agent-owned — note: NOT under `tests/spec/m5/` since the orchestrator-frozen `test_spec_m5_modes.py` already covers the spec contract). Agent C unskips `pysdk/tests/spec/m5/test_spec_m5_modes.py` here. | W1A's TASK-077 + W1C's real-gRPC transport. |

W2A and W2B share zero files. Both can run in one parallel batch.

### Wave 3 — orchestrator close

| Sub-agent | Owner | Tasks | Owned files |
|---|---|---|---|
| **W3** | Orchestrator | TASK-085 | `CHANGELOG-MASTERPLAN.md` (extend with M5 closing entry), `docs/tasks/signals/TASK-085.done` (NEW). Reads (does not modify) `docs/reports/M5/agent-{a,b,c}.md` to synthesize the closing entry. |

Single sub-agent (the orchestrator's own session). Closes M5 → MVP gate passed.

## 5. Spec test mapping

| Spec test file | Turns green at end of |
|---|---|
| `rti/spec/M5/mode_flag_test.go` (2 tests) | Wave 1 (W1A) |
| `rti/spec/M5/best_effort_test.go` (2 skip-scaffolds) | Wave 1 (W1A unskips) |
| `rti/spec/M5/perf_test.go` (2 tests; first skips on stub) | Wave 2 (W2A) |
| `rti/spec/M5/soak_test.go` (build tag `soak`) | Wave 2 (W2A) |
| `rti/spec/M5/cross_lang_test.go` (skip-scaffold) | Wave 1 (W1C unskips) — Go-side orchestration check |
| `pysdk/tests/spec/m5/test_spec_m5_modes.py` (2 skip-scaffolds) | Wave 2 (W2B unskips) |
| `pysdk/tests/spec/m5/test_spec_m5_cross_language.py` (1 skip-scaffold) | Wave 1 (W1C unskips) |

## 6. Hard rules per wave

These apply per `docs/DISPATCH.md` §4 plus M5-specific:

1. **Cross-agent isolation**. Per `docs/ORTHOGONALITY.md` §2, no agent's branch may write to another agent's owned path. This is enforced by `scripts/check-frozen-paths.sh` plus orchestrator-side review. With three agents in parallel, the temptation to "just fix the other side" is highest — DO NOT.

2. **Stub signature freeze**. `rti/internal/perf/Manager` interface + `Result` struct + `Options` struct + JSON schema constants (`SchemaVersion = 1`) are part of the M5 contract. Agent A may add private helpers but must not change exported names or JSON tags without a `contract-change-request:` issue.

3. **Mode plumbing already partial**. The proto + core.Mode + gRPC handler already accept the Mode field at federation create (W1A from M2 wired this). TASK-076's actual scope is just the CLI flag (`--mode=verbose|best-effort` at `rtid`). TASK-077 is the substantive RO-vs-TSO delivery logic.

4. **Real-gRPC + real-pyjevsim are W1C scope**. The Python SDK currently only handles `memory://fake-rti` URLs; real `grpc://` was M4 follow-up. Bundle into TASK-081 since cross-lang fundamentally requires it. Same for the real-pyjevsim adapter — TASK-073 used pure-Python coupled models; TASK-081 exercises real pyjevsim against the running rtid binary, so the adapter lands here.

5. **Determinism audit produces issues, not code**. TASK-083 files `bug:` issues via `gh issue create`. Each issue is the START of a future fix; the orchestrator decides whether to dispatch separate fix tasks or carry forward.

6. **Sentinels per task**. Each task gets its own `docs/tasks/signals/TASK-NNN.done`. Bundled work (W1A: 076+077; W2A: 078+079+080) produces all bundled sentinels in the final commit.

## 7. Verification activities (gate-time, not dispatched as TASK-NNN)

Per `docs/AGENTS.md` §6.2:

- **Agent A at M5 gate**: filed an `verification:M5` issue with the perf baseline numbers + flagging any regressions vs M3 baselines. (Implicit in TASK-080's deliverable.)
- **Agent B at M5 gate**: TASK-083 IS the verification activity. The audit-style task produces the verification record.
- **Agent C at M5 gate**: filed an `verification:M5` issue with the cross-language scenario log + any cross-language inconsistencies observed. (Implicit in TASK-081's deliverable.)

These run as part of the wave model (not separately).

## 8. Dispatch order checklist (orchestrator's runbook)

1. **Pre-work confirmation** (DONE as of this commit):
   - `rti/spec/M5/*.go` + `pysdk/tests/spec/m5/*.py` on `main`, RED for the right reason
   - `rti/internal/perf/{doc,baseline}.go` stubs on `main`
   - `docs/reports/M5/.gitkeep` on `main`
   - `scripts/check-milestones.sh` M5 probe re-pointed
2. **Wave 1**: spawn W1A + W1B + W1C in one parallel `Agent` tool call (3 in parallel, three different agents). Wait for all to push branches. Review + merge in this order: W1B (no source changes; smallest review), W1A (Go-side first; unblocks W2A), W1C (Python-side; unblocks W2B).
3. **Wave 2**: spawn W2A + W2B in parallel. Merge.
4. **Wave 3**: orchestrator session writes the M5 closing CHANGELOG entry; commits TASK-085.done sentinel.
5. **M5 gate**: re-run `scripts/check-milestones.sh`. Should report `M0..M5: DONE`. Push tag `mvp`. **MVP achieved.**

## 9. Risk mitigations

| Risk | Mitigation |
|---|---|
| W1C real-gRPC transport is hefty; could blow timeline | Brief sub-agent that minimal SDK gRPC support is fine: open channel, dispatch RPC calls, drain server streams. Real production-quality (TLS, retries, etc.) deferred to post-MVP. The cross-lang smoke is the test gate, not a production-readiness check. |
| Real-pyjevsim adapter trips on pyjevsim API quirks | Sub-agent budgets ~20 min for the adapter; if it can't get a minimal Producer-Consumer working, fall back to pure-Python coupled models for the smoke (cross-language is the contract; real-pyjevsim is nice-to-have). Document the deferral. |
| TASK-083 audit finds critical determinism bug | Sub-agent files the issue, doesn't fix. Orchestrator triages: blocking → dispatch fix as new TASK-086+; non-blocking → defer to post-MVP. |
| Concurrent agent merges produce semantic conflicts (e.g. both extend `connection.py`) | Per `docs/ORTHOGONALITY.md` §2, this CANNOT happen — paths are disjoint. If a sub-agent reports needing to touch another agent's path, STOP and escalate. |
| Soak test (TASK-078) is flaky in CI | Build tag `soak` keeps it out of default test runs. CI invokes via `-tags=soak` separately. Default `make verify` stays fast. |
| TASK-081 fails because the smoke uses `examples/go-pingpong` binary which expects M2 protocol — version drift between Go and Python sides | Both sides target the same proto contract (frozen at M0). Drift would show up as gRPC error responses, which the smoke test asserts on. If asymmetry surfaces, file `verification:M5` against the offending side. |

## 10. Cross-wave file conflict scan

Pre-dispatch verification — every owned-file set must be disjoint within a wave AND across the bundled tasks within a sub-agent.

| Wave | Sub-agent | Agent | Files (head) |
|---|---|---|---|
| 1 | W1A | A | `rti/cmd/rtid/main.go` (extend), `rti/internal/transport/grpc/mode.go` (NEW), `rti/internal/transport/grpc/best_effort.go` (NEW), `rti/internal/object/update.go` (extend), `rti/internal/object/interaction.go` (extend) |
| 1 | W1B | B | `docs/reports/M5/agent-b.md` (NEW) — only one file |
| 1 | W1C | C | `examples/pyjevsim/cross_lang_test.py` (NEW), `examples/pyjevsim/cross_lang_runner.py` (NEW), `examples/pyjevsim/_real_pyjevsim.py` (NEW), `pysdk/rti1516e/connection.py` (extend; add real-gRPC dispatch alongside the existing memory:// dispatch), `pysdk/rti1516e/_transport.py` (extend; add a `grpc://` codepath alongside register_fake), and the `pysdk/tests/spec/m5/test_spec_m5_cross_language.py` UNSKIP |
| 2 | W2A | A | `rti/internal/transport/grpc/load_test.go` (NEW), `rti/internal/perf/baseline.go` (extend), `rti/internal/perf/baseline_test.go` (NEW), `examples/go-pingpong/perf_main.go` (NEW), `docs/reports/M5/agent-a.md` (NEW) |
| 2 | W2B | C | `pysdk/tests/test_modes.py` (NEW agent-owned), `pysdk/tests/spec/m5/test_spec_m5_modes.py` (UNSKIP) |
| 3 | W3 | Orchestrator | `CHANGELOG-MASTERPLAN.md` (extend), `docs/tasks/signals/TASK-085.done` (NEW) |

No file appears in more than one cell. Cross-agent reads (allowed, non-conflicting):
- W1C reads `proto/rti/v1/*.proto` (codegen input) and `rti/cmd/rtid` (binary invocation)
- W2A reads `examples/pyjevsim/cross_lang_runner.py` (runtime dependence; no write)
- W3 reads everything (orchestrator-only concern)

All other paths are write-isolated.
