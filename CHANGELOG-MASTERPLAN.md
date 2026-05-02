# Master Plan Changelog

Append-only record of master-plan revisions. The orchestrator updates this after reading milestone status reports from all agents (`docs/reports/M<x>/agent-{a,b,c}.md`).

Entries are most-recent first. Each entry: date, summary of decision, link to the status reports that drove it.

---

## 2026-05-02 — Backlog committed; lint unblocked; M1 spec extended; discipline drift recorded

Material reconciliation between planned and actual state. No agent status reports yet (M1 still in flight); this revision is orchestrator-driven from observed working-tree drift.

### What landed on `main`

- **89 TASK files committed** to `docs/tasks/TASK-001.md` … `TASK-089.md`. The full M1..M5 backlog is now reachable via `git log` on `main`. Until this commit, agents had been working off untracked TASK files — the protocol requirement that "orchestrator commits TASK file to `main`" (see `docs/DISPATCH.md` §2 step 3) was not being honored.
- **TASK-084 cancelled** (per its own decision rule — TASK-080 perf baseline absent; do not optimize speculatively per `docs/agent-b-fom-encoding.md` §4 anti-goal). File retained for traceability per `docs/DISPATCH.md` §7.1; ID-084 will not be reused.
- **TASK-008 and TASK-009 marked `BLOCKED`** by [issue #1](https://github.com/cbchoi/gorti/issues/1) (canonical MIM XML sourcing). Agent B should not progress these until orchestrator resolves the contract-change-request and lands canonical MIM content.
- **`.golangci.yml` amended** to exclude `rti/internal/core/clock.go` from `forbidigo`'s `time.Now` ban. That file is the deliberate single sanctioned wrapper around `time.Now` (the whole reason `core.Clock` exists); without this exclude every PR fails `make verify`.
- **`.gitignore` extended** with `.tools/` and `.tmp/` — ad-hoc local toolchain caches (one local cache was 333 MB) that must never enter the repo.
- **`tests/spec/M1/parser_diagnostics_test.go`** extended for FOM-003, FOM-005, FOM-012, FOM-013 (the 4 codes the M1 exit criterion of "10 malformed FOMs" requires beyond the original 6). Pairs with 4 new bad-FOM fixtures under `tests/conformance/foms/bad/`. Unblocks TASK-086..089 dispatch.
- **`tests/spec/M1/encoding_vectors_test.go` composite extension deferred** — the upgrade (lifting composite vector `{kind, ...}` Type descriptors into `model.DataType` values to drive `encoding.CodecFor`) imports `rti/pkg/fom/model`, a package that does not yet exist on `main`. Landing the test now would break `go test ./...`. The extension stays in the stash and lands together with TASK-019 (Agent B's M1 exit task) so the test moves from `t.Skip` to passing in a single coherent step.
- **`docs/DISPATCH.md` §3 + new §7.2**: `BLOCKED` added to the canonical Status enumeration. New §7.2 distinguishes task-graph dependencies (`Depends-on:`) from external-artifact blockers (BLOCKED).

### Discipline drift recorded (not penalised, but called out)

- **Sentinel-without-merged-TASK** on `agent/c/codegen-setup`: 14 commits including TASK-050..062 sentinels were created on a topic branch while the corresponding `docs/tasks/TASK-NNN.md` briefs were not yet on `main`. Per `docs/DISPATCH.md` §10, sentinels reference the TASK file as their durable signal; without the brief on `main` the sentinel is dangling. Recommended remediation: rebase that branch onto the new `main` (this commit), so the sentinels land alongside their briefs.
- **Multiple IN_PROGRESS per agent** (Agent C did TASK-050..062 in 14 sequential commits without orchestrator review/merge between each). `docs/DISPATCH.md` §4.4 caps at one IN_PROGRESS per agent. The branch will need staged review (sentinel-by-sentinel) before merging.
- **Substantial uncommitted Agent B work** for TASK-001..009 + TASK-086..089: ~30 untracked Go source files. Not lost — preserved in stash + working-tree fragments — but never committed via TDD-discipline. Agent B should redo the work properly with red-green commit pattern per `docs/TDD.md` §3, since the existing fragments lack the test-first commit history reviewers walk.
- **Frozen-path violation (cosmetic only)**: `rti/internal/core/errors.go` and `rti/internal/core/federation.go` had local gofmt alignment changes from someone running `make fmt` over the whole tree. No semantic change. The pre-commit hook should have rejected if anyone tried to commit on an agent branch; this commit absorbs the cosmetic fix on `main`.

### What is NOT in this commit

- Agent C's pysdk encoding/codegen work on `agent/c/codegen-setup` — left for review-and-merge cycle per `docs/DISPATCH.md` §10.
- Agent B's parser/model/MIM/encoding work in the working tree — left for proper test-first redo on a clean topic branch.
- Resolution of issue #1 (canonical MIM XML) — pending orchestrator decision on sourcing path.

### Next concrete actions (orchestrator)

1. Resolve [issue #1](https://github.com/cbchoi/gorti/issues/1): pick a sourcing path (Portico CDDL is the recommendation) and commit canonical MIM XML to `rti/pkg/fom/mim/`. Flip TASK-008 and TASK-009 back to `DISPATCHED`.
2. Triage `agent/c/codegen-setup`: rebase onto this `main`, then merge sentinels in order with review per `docs/DISPATCH.md` §10.
3. Re-dispatch TASK-001 to Agent B on a clean topic branch off this `main`.

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
