# M2 Dispatch Plan

How the orchestrator dispatches the 21 M2 tasks (TASK-020..040) to maximize parallel sub-agent throughput while keeping every wave orthogonal at the file level.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md` (general protocol), `docs/agent-a-rti-core.md` (Agent A brief), `docs/MILESTONE_CHECK.md` (probe definitions), `docs/srs.md` §10.2 (M2 exit criteria).

---

## 1. Why a wave model

M2 has 21 tasks, all assigned to Agent A in `docs/agent-a-rti-core.md`. Naive serial dispatch takes ~21 sub-agent rounds even though many tasks touch disjoint files. By grouping tasks into **waves** where every wave's sub-agents own non-overlapping files, the orchestrator can fire 2–3 sub-agents in parallel per wave.

The same pattern was proven on M1 — Wave 1 had 4 parallel agents (parser-skeleton + 3 codec families); Wave 2 had 3 parallel agents (parser diagnostics + composite codecs + records). Total wall time was ~3× faster than serial.

## 2. Wave structure

```
                 (M0 + M1 already DONE)
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 1 (3 parallel sub-agents — no upstream deps)  │
   │   W1A — federation/      (TASK-020..023)           │
   │   W1B — eventlog/        (TASK-024..025)           │
   │   W1C — declaration/     (TASK-027..029)           │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 2 (1–2 parallel — depend on Wave 1)           │
   │   W2A — object/          (TASK-030..033)           │
   │   W2B — eventlog/replayer.go (TASK-026)            │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 3 (3 parallel — depend on Wave 2)             │
   │   W3A — grpc/federation.go        (TASK-034)       │
   │   W3B — grpc/declaration.go       (TASK-035)       │
   │   W3C — grpc/object.go + stream.go (TASK-036)      │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
   ┌────────────────────────────────────────────────────┐
   │ Wave 4 (1 sub-agent — integration; M2 gate)        │
   │   W4 — cmd/rtid wiring + go-pingpong example       │
   │        + determinism harness + replay harness      │
   │        (TASK-037, 038, 039, 040)                   │
   └────────────────────────────────────────────────────┘
                          │
                          ▼
                    M2 DONE per srs.md §10.2
```

Critical path: 4 waves. With sub-agent runtime ~5–10 minutes each, M2 completes in roughly 20–40 minutes wall-time vs. 100+ for strict serial dispatch.

## 3. File ownership per wave

The TDD-friendly file decomposition. Within each wave, sub-agents touch disjoint files; the only cross-wave shared files are the orchestrator-seeded stubs (which sub-agents EXTEND but do not RESHAPE).

### Wave 1 — three independent components

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W1A** federation | TASK-020/021/022/023 | `rti/internal/federation/manager.go` (extend body) + new files for impl details (`join.go`, `roster.go`, etc.) at the agent's discretion. |
| **W1B** eventlog write/read | TASK-024/025 | `rti/internal/eventlog/format.go` (extend), `writer.go` (extend), `reader.go` (extend). NOT replayer.go (W2B). |
| **W1C** declaration | TASK-027/028/029 | `rti/internal/declaration/manager.go` (extend body) + new files for matrix internals at agent's discretion. |

W1A, W1B, W1C share zero files. All three can run in one parallel batch.

### Wave 2 — depend on Wave 1 outputs

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W2A** object registry | TASK-030/031/032/033 | `rti/internal/object/registry.go` (extend) + new files. | Federation (consume `*federation.Manager` for federate-membership lookup), EventLog Writer (write-ahead), Declaration Manager (`SubscribersFor`/`PublishersFor`). |
| **W2B** eventlog replayer | TASK-026 | `rti/internal/eventlog/replayer.go` (extend). | EventLog Writer/Reader (Wave 1) + Object Registry (Wave 2A — to drive Update/Send during replay). |

W2A and W2B do NOT share files — but W2B has a transitive dependency on W2A's API (Replayer drives `core.ObjectRegistry`). Sequence them: W2A first, then W2B. Or run in parallel if W2B uses only the `core.ObjectRegistry` interface (which exists from M0); it can be wired against a stub during dev.

### Wave 3 — three gRPC handlers

| Sub-agent | Tasks | Owned files | Dependencies |
|---|---|---|---|
| **W3A** | TASK-034 (FederationService handlers) | `rti/internal/transport/grpc/federation.go` | `*federation.Manager` (W1A) |
| **W3B** | TASK-035 (DeclarationService handlers) | `rti/internal/transport/grpc/declaration.go` | `*declaration.Manager` (W1C) |
| **W3C** | TASK-036 (ObjectService + StreamService) | `rti/internal/transport/grpc/object.go`, `stream.go` | `*object.Registry` (W2A) + `core.Outbox` |

All three sub-agents touch disjoint files; all three can run in one parallel batch.

`server.go` (composing all three) is touched by EXACTLY ONE of them — assign it to W3A as part of TASK-034 to avoid race. Document this in the TASK file.

### Wave 4 — integration (M2 gate)

| Sub-agent | Tasks | Owned files |
|---|---|---|
| **W4** | TASK-037/038/039/040 | `rti/cmd/rtid/main.go` (extend M0 stub), `rti/cmd/rtid/metrics.go` (new), `examples/go-pingpong/main.go`, `examples/go-pingpong/determinism_test.go`, `examples/go-pingpong/replay_test.go`. |

Single sub-agent because the integration is sequential by nature (wire → smoke → harness → gate test).

## 4. Spec test mapping

The orchestrator-frozen `rti/spec/M2/` spec tests target each component. Each wave's sub-agent verifies their work by turning specific tests green:

| Spec test file | Turns green at end of |
|---|---|
| `federation_test.go` | Wave 1 (W1A) |
| `eventlog_test.go` (Reader/Writer parts) | Wave 1 (W1B) |
| `declaration_test.go` | Wave 1 (W1C) |
| `object_test.go` | Wave 2 (W2A) |
| `eventlog_test.go` (Replayer parts) | Wave 2 (W2B) |
| `replay_test.go` | Wave 4 (W4 — needs all components) |
| `grpc_test.go` (Server compose) | Wave 3 (W3A wires server.go) |
| `grpc_test.go` (per-service handlers) | Wave 3 (W3A/B/C) |

All spec tests live in `rti/spec/M2/` (not `tests/spec/M2/`) because Go's `internal` package rule blocks `tests/...` from importing `rti/internal/*`. The script's M2 probe at `scripts/check-milestones.sh` looks at `rti/spec/M2/` accordingly. M3 follows the same convention.

## 5. Hard rules per wave

These apply per `docs/DISPATCH.md` §4 (no self-selection, no multi-task PRs, etc.) plus M2-specific:

1. **Stub signature freeze**. Every method declared in the orchestrator-seeded stubs (e.g. `federation.Manager.CreateFederation(ctx, req)`) is part of the M2 contract. Sub-agents may reshape internal helpers but must not change exported signatures without a `contract-change-request:` issue per `docs/WORKFLOW.md` §4.4.

2. **Compile-time interface assertions stay**. Each stub file ends with `var _ core.FederationStore = (*Manager)(nil)` style assertions. Sub-agents must keep these — removing one is a contract-change.

3. **No cross-wave file edits**. A Wave-3 sub-agent must NEVER edit a file owned by Wave 1 (federation/declaration/eventlog body). If a bug is discovered there, file an issue and continue.

4. **Wave-2 sub-agents wait for Wave-1 sentinels**. W2A's `Depends-on:` lists TASK-020..025 + 027..029. The agent confirms the sentinels are on `main` before starting (per `docs/DISPATCH.md` §6).

5. **Sentinels per task**. Each task gets its own `docs/tasks/signals/TASK-NNN.done`. If a sub-agent bundles multiple tasks (e.g. W1A handles TASK-020..023), it produces ALL bundled sentinels in the final commit — same pattern as M1's parser-diagnostics bundle.

## 6. Verification activities (gate-time, not dispatched as TASK-NNN)

Per `docs/AGENTS.md` §6.2 + `docs/agent-{b,c}-*.md` §5:

- **Agent B at M2 gate**: write a gRPC + malformed-FOM fuzzer; assert no panics, all errors carry codes from `proto/rti/v1/errors.proto`. File `verification:M2` issue.
- **Agent C at M2 gate**: write a "naughty federate" in Go (out-of-order, double-join, mid-update resign, unsubscribed-class interaction); confirm graceful handling. File `verification:M2` issue.

These run after Wave 4 completes; they are not part of the wave model.

## 7. Dispatch order checklist (orchestrator's runbook)

Step-by-step the orchestrator follows for M2:

1. **Pre-work confirmation** (already done as of `<dispatch-day>`):
   - `rti/internal/federation/`, `rti/internal/eventlog/`, `rti/internal/declaration/`, `rti/internal/object/`, `rti/internal/transport/grpc/` stubs on `main`
   - `rti/spec/M2/*.go` spec tests on `main`, RED for the right reason
   - `scripts/check-milestones.sh` updated to probe `rti/spec/M2/`
2. **Wave 1**: spawn W1A + W1B + W1C in one parallel `Agent` tool call. Wait for all three to push branches. Review + merge in this order (federation → eventlog → declaration; minimizes conflicts as none should exist anyway).
3. **Wave 2**: spawn W2A. After it lands, optionally spawn W2B in parallel with starting Wave 3 (if W2B uses only `core.ObjectRegistry`).
4. **Wave 3**: spawn W3A + W3B + W3C in one parallel call. Reserve `server.go` as W3A's; W3B/W3C only own per-service handlers.
5. **Wave 4**: spawn W4. This sub-agent does the most work but it's all integration with no parallel partner.
6. **M2 gate**: re-run `scripts/check-milestones.sh`. Should report `M1: DONE` and `M2: DONE` (green). Push tag `m2` (optional).

## 8. Risk mitigations

| Risk | Mitigation |
|---|---|
| Sub-agent reshapes a stub signature | Pre-commit `check-frozen-paths.sh` + orchestrator review; reject PR if `git diff main -- '*.go' | grep -E '^[-+] ?func.*FederationStore'` shows signature changes |
| Wave 1 components diverge on shared semantics (e.g. error codes) | Shared sentinel error set lives in `rti/internal/core/errors.go` (frozen-shape M0); sub-agents reuse rather than redeclare |
| Replay determinism fails because of map iteration | Spec tests in `rti/spec/M2/` cover deterministic-order assertions explicitly; CI gate catches before merge |
| `cmd/rtid` integration touches files Wave 3 expected to own | Wave 4's TASK-037 brief explicitly carves out: rtid wiring touches only `cmd/rtid/main.go` and `metrics.go`; if it needs a change in `transport/grpc/`, file a follow-up bug |
