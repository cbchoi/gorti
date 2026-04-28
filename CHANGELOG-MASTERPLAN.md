# Master Plan Changelog

Append-only record of master-plan revisions. The orchestrator updates this after reading milestone status reports from all agents (`docs/reports/M<x>/agent-{a,b,c}.md`).

Entries are most-recent first. Each entry: date, summary of decision, link to the status reports that drove it.

---

## 2026-04-28 (later) — M0 deliverables produced; orthogonality + dispatch + sentinel locked

Built out M0 contracts and scaffolding under `/workspace/gorti/`:

- **Proto contracts**: 8 `.proto` files in `proto/rti/v1/` (common, errors, federation, declaration, object, time, stream, eventlog) covering all five gRPC services + the event log binary format.
- **Go core interfaces**: 12 files in `rti/internal/core/` — frozen, orchestrator-only (Transport, FederationStore, ObjectRegistry, TimeManager, EventLog, FOMRepository, Codec, Outbox, Clock + typed handles + sentinel errors).
- **Stub agent packages**: `rti/pkg/fom/parser` and `rti/pkg/encoding` contain minimum API surfaces (Parse / Result / Diagnostic / Codec / CodecFor / PrimitiveByName) returning `ErrNotImplemented`. Signatures are part of the M0 contract; bodies are Agent B's M1 work.
- **M1 specification tests**: `tests/spec/M1/` (orchestrator-written, frozen) — `parser_diagnostics_test.go` covering FOM-001/002/004/009/011/101 and 2 good-FOM accepts; `encoding_vectors_test.go` covering 16 primitive vectors.
- **Conformance fixtures**: `tests/conformance/encoding_vectors.json` (16 vectors + 1 disabled composite example), 2 good FOMs, 6 bad FOMs.
- **CI + tooling**: Makefile, `.golangci.yml` (depguard isolating `pkg/` from `internal/`, forbidigo blocking `time.Now`/`fmt.Println`), `ruff.toml`, `buf.yaml`/`buf.gen.yaml`, `.pre-commit-config.yaml`, `scripts/check-{frozen-paths,no-emojis,no-debug-prints}.sh`, `.github/workflows/ci.yml`.
- **Skeleton main**: `rti/cmd/rtid/main.go` with flags wired and `TODO(#1)` for services.

Three governance documents added on top of the original plan:

- **`docs/ORTHOGONALITY.md`** — exhaustive path-to-owner table; zero co-ownership policy; producer/consumer rules; working-directory isolation via git worktrees (`/workspace/gorti-agent-{a,b,c}/`).
- **`docs/DISPATCH.md`** — orchestrator-driven task assignment; agents do not self-select; one IN_PROGRESS task per agent; idle protocol; cancellation; orchestrator commitments.
- **`docs/tasks/signals/README.md`** — completion sentinel: agents create `docs/tasks/signals/TASK-NNN.done` as the FINAL commit on the topic branch; without it the PR is treated as draft. Pre-commit hook allow-lists this specific path while keeping all other writes under `docs/tasks/**` frozen.

Plus `scripts/setup-agent-worktrees.sh` to initialize the three sibling worktrees from `main`.

**State**: `/workspace/gorti/` is NOT yet git-init'd. Next action: user runs `git init -b main` + initial commit, then `./scripts/setup-agent-worktrees.sh`, then orchestrator dispatches TASK-001 to agent-b (minimal parser skeleton accepting `good/minimal.xml`). No agent status reports yet — M1 has not started.

---

## 2026-04-28 — Initial plan locked

Initial plan and doc set established by orchestrator-driven conversation. Walking-skeleton MVP, milestones M0..M5, three sandboxed coding agents (claude-sandbox / codex-sandbox / gemini-sandbox), TDD methodology with orchestrator-written spec tests as milestone contracts.

See:
- `docs/srs.md` — SRS
- `docs/sdd.md`, `docs/idd.md` — design + interfaces
- `docs/AGENTS.md`, `docs/CODING_CONVENTIONS.md`, `docs/TDD.md`, `docs/WORKFLOW.md` — operating rules
- `docs/agent-{a,b,c}-*.md` — per-agent briefs

No prior status reports (this is the starting point).
