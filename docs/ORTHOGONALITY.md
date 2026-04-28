# Orthogonality Policy

This document is the **single source of truth** for path ownership across the three coding agents. Its goal: every file in the repository has exactly **one** owner. Agents implement orthogonally — no overlap, no shared edits, no race for the same path.

This is FROZEN — only the orchestrator may edit. Per-agent briefs and `AGENTS.md` reference this; they do not duplicate the table.

---

## 1. Policy

### 1.1 Zero co-ownership

Every file under version control is owned by exactly one of:

- **Orchestrator** (Claude in the orchestration conversation)
- **Agent A** (claude-sandbox)
- **Agent B** (codex-sandbox)
- **Agent C** (gemini-sandbox)

There is no "shared" or "co-owned" path. If a file appears to need shared edits, that is a sign the design is wrong; raise a `contract-change-request:` issue.

### 1.2 Read-only access for non-owners

A non-owning agent may **read** any file in the repo (compile against it, import it, run its tests, study its source). A non-owning agent may **never write** — not even a one-character fix.

If a non-owner spots a defect in another agent's code, the response is: file an issue (`bug:` for clear defects, `verification:M<x>:` for milestone-gate findings). Do not "helpfully" fix it.

### 1.3 Producer / consumer relationships (NOT co-ownership)

Some artifacts are produced by one agent and consumed by another. These are **not** co-owned — the producer owns the file; the consumer reads it.

Listed in §3.

### 1.4 Working directory isolation

Each agent operates in its own git worktree at a separate filesystem path:

| Agent | Working directory | Long-lived branch |
|---|---|---|
| Orchestrator | `/workspace/gorti/` | `main` |
| Agent A | `/workspace/gorti-agent-a/` | `agent/a/scratch` (topic branches off `main`) |
| Agent B | `/workspace/gorti-agent-b/` | `agent/b/scratch` (topic branches off `main`) |
| Agent C | `/workspace/gorti-agent-c/` | `agent/c/scratch` (topic branches off `main`) |

See `docs/WORKFLOW.md` §2.5 and `scripts/setup-agent-worktrees.sh` for setup.

Filesystem isolation backs the namespace policy: an agent's auto-approved write commands cannot touch another agent's directory because that directory is not in the agent's sandbox view.

### 1.5 Disambiguation rule

If a path is not listed in §2 or §3, the orchestrator owns it by default. Agents propose ownership via `contract-change-request:` issue.

---

## 2. Exhaustive Path Ownership Table

Top-level paths and their owners. `**` is recursive (all descendants).

| Path | Owner | Notes |
|---|---|---|
| `LICENSE` | Orchestrator | Frozen; project-level |
| `README.md` | Orchestrator | Frozen |
| `CHANGELOG.md` | Orchestrator | Frozen (release notes per version) |
| `CHANGELOG-MASTERPLAN.md` | Orchestrator | Frozen (plan-revision log driven by status reports) |
| `Makefile` | Orchestrator | Frozen |
| `go.mod`, `go.sum` | Orchestrator | Frozen for adds; agent PRs go via `deps:` flow |
| `.gitignore`, `.gitattributes` | Orchestrator | Frozen |
| `.golangci.yml`, `ruff.toml`, `buf.yaml`, `buf.gen.yaml` | Orchestrator | Frozen |
| `.pre-commit-config.yaml` | Orchestrator | Frozen |
| **proto/** | | |
| `proto/rti/v1/**` | Orchestrator | FROZEN — wire contracts |
| **rti/** | | |
| `rti/cmd/rtid/**` | **Agent A** | RTI binary entrypoint |
| `rti/internal/core/**` | Orchestrator | FROZEN — core interfaces |
| `rti/internal/federation/**` | **Agent A** | Federation lifecycle |
| `rti/internal/declaration/**` | **Agent A** | Pub/sub bookkeeping |
| `rti/internal/object/**` | **Agent A** | Object/interaction routing |
| `rti/internal/time/**` | **Agent A** | Time mgmt + LBTS |
| `rti/internal/eventlog/**` | **Agent A** | TSO event log writer/reader |
| `rti/internal/transport/grpc/**` | **Agent A** | gRPC service handlers |
| `rti/internal/genproto/**` | (Generated) | Output of `make proto`; gitignored. Edits forbidden; regenerate instead. |
| `rti/pkg/fom/parser/**` | **Agent B** | FOM XML parser; signature stub from M0 (frozen-shape) |
| `rti/pkg/fom/mim/**` | **Agent B** | Embedded MIM + merge logic |
| `rti/pkg/fom/model/**` | **Agent B** | Immutable FOM data structures |
| `rti/pkg/fom/doc.go` | **Agent B** | Top-level package doc |
| `rti/pkg/encoding/**` | **Agent B** | HLA Evolved encoding rules |
| **pysdk/** | | |
| `pysdk/pyproject.toml`, `pysdk/README.md` | **Agent C** | Created at M4 start |
| `pysdk/rti1516e/**` (excluding `_generated/`) | **Agent C** | Python SDK |
| `pysdk/rti1516e/_generated/**` | (Generated) | Output of `make proto`; gitignored |
| `pysdk/pyjevsim_bridge/**` | **Agent C** | DEVS↔HLA bridge |
| `pysdk/tests/**` | **Agent C** | Python tests |
| **examples/** | | |
| `examples/README.md` | Orchestrator | Frozen index |
| `examples/go-pingpong/**` | **Agent A** | M2 example |
| `examples/go-timed/**` | **Agent A** | M3 example |
| `examples/pyjevsim/**` | **Agent C** | M4 example |
| **tests/** | | |
| `tests/spec/**` | Orchestrator | FROZEN — milestone contract tests |
| `tests/conformance/encoding_vectors.json` | **Agent B** (extends; orch seeded) | Producer-consumer; see §3 |
| `tests/conformance/foms/good/**`, `tests/conformance/foms/bad/**` | **Agent B** (extends; orch seeded) | Producer-consumer; see §3 |
| `tests/conformance/foms/vendor/**` | (Vendored) | Third-party schemas; orchestrator-imported, never edited |
| **docs/** | | |
| `docs/srs.md`, `docs/sdd.md`, `docs/idd.md` | Orchestrator | FROZEN |
| `docs/AGENTS.md`, `docs/CODING_CONVENTIONS.md`, `docs/TDD.md`, `docs/WORKFLOW.md`, `docs/ORTHOGONALITY.md`, `docs/DISPATCH.md` | Orchestrator | FROZEN |
| `docs/agent-a-rti-core.md`, `docs/agent-b-fom-encoding.md`, `docs/agent-c-pysdk.md` | Orchestrator | FROZEN |
| `docs/templates/**` | Orchestrator | FROZEN |
| `docs/tasks/README.md`, `docs/tasks/TASK-template.md` | Orchestrator | FROZEN |
| `docs/tasks/TASK-*.md` | Orchestrator | FROZEN — orchestrator-written task briefs |
| `docs/tasks/signals/README.md` | Orchestrator | FROZEN — sentinel format spec |
| `docs/tasks/signals/TASK-NNN.done` | Respective agent | Completion sentinel; agent creates own task's `.done` only. See `docs/DISPATCH.md` §10. |
| `docs/reports/M<x>/agent-<a\|b\|c>.md` | Respective agent | Status reports — agent writes their own only |
| **scripts/** | | |
| `scripts/**` | Orchestrator | Frozen; agents request changes via issue |
| **.github/** | | |
| `.github/**` | Orchestrator | FROZEN |

---

## 3. Producer / Consumer Relationships

These are NOT co-ownership. The producer owns the file; the consumer is read-only.

| Artifact | Producer | Consumer(s) | Rules |
|---|---|---|---|
| `proto/rti/v1/*.proto` | Orchestrator | Agent A (Go server), Agent C (Python client) | Consumers regenerate with `make proto`; never hand-edit generated output. Producer changes = contract-change-request. |
| `rti/internal/core/*.go` (interfaces) | Orchestrator | Agents A and B (Go); Agent C never imports | Consumers implement against; never modify. |
| `rti/pkg/fom/parser` (API surface) | Agent B | Agent A (RTI uses parser at federation create) | Agent A reads only; defects → issue against B. |
| `rti/pkg/fom/model` (FOM data types) | Agent B | Agent A (handle resolution); Agent C (Python mirror — independently re-derived from XML, not from Go) | Agent C MUST NOT import the Go model; consumes the FOM XML and re-parses in Python. |
| `rti/pkg/encoding` (Codec implementations) | Agent B | Agent A (RTI uses codec at attribute encode/decode) | Agent A reads only. |
| `tests/conformance/encoding_vectors.json` | Agent B (orch seeded at M0) | Agent C (Python encoder must match byte-for-byte) | **Additive-only**: Agent B may add new vectors via PR; may NOT modify or delete existing entries. Agent C is strictly read-only. Removals or modifications require a contract-change-request. |
| `tests/conformance/foms/{good,bad}/*.xml` | Agent B (orch seeded at M0) | Orchestrator's `tests/spec/M1/` runner; Agent A's M2 verification (fuzzer); Agent C's M4 example | **Additive-only**, same rules as encoding vectors. Renaming a fixture is a breaking change. |
| `examples/pyjevsim/pyjevsim-bridge.fom.xml` (the actual file used at runtime) | Orchestrator | Agent C | Lives at `tests/conformance/foms/good/pyjevsim-bridge.xml`; the example references that path; no copy. |
| `tests/spec/M<x>/*` | Orchestrator | All three agents (must pass) | Strictly orchestrator-owned. Agents cannot weaken; may add new spec tests via contract-change-request only. |
| `docs/tasks/TASK-*.md` | Orchestrator | Assigned agent only | Agent reads task brief; acks via PR or issue comment; never edits the brief itself. Status updates go in the agent's PR description, status report, or a separate issue comment. |
| Status reports `docs/reports/M<x>/agent-<a\|b\|c>.md` | Respective agent | Orchestrator | Each agent writes ONLY their own report. Reading other agents' reports is allowed (encouraged). |

---

## 4. Resolving Ambiguity

If you (an agent) think your task requires editing a path you do not own:

1. **Stop.** Do not make the edit.
2. Re-read this document and your brief — most cases are answered here.
3. If genuinely ambiguous: open an issue titled `contract-change-request: ownership clarification for <path>`. Describe what you need to change, why, and the alternative you considered.
4. Wait for orchestrator decision. The orchestrator will:
   - Update this document if ownership needs to be reassigned, OR
   - Open the change as an orchestrator PR, OR
   - Reject and explain the alternative.

Common ambiguities and their resolutions:

| Scenario | Resolution |
|---|---|
| "I need to add a method to a `core` interface for my work." | Open contract-change-request. Orchestrator updates `core/` and possibly `idd.md`. Agent does not edit `core/`. |
| "My package needs a small helper that already exists in another agent's package." | Duplicate the helper in your own package (3 lines beats premature shared abstraction — see CODING_CONVENTIONS.md anti-goals). If duplication grows past trivial, contract-change-request to extract a shared utility under `rti/pkg/`. |
| "I want to add a new error code." | Codes are in `proto/rti/v1/errors.proto` (frozen). Contract-change-request. |
| "I noticed a typo in another agent's comment." | Don't touch it. File a low-priority `bug:` issue if it matters. |
| "An example I'm writing needs the FOM another agent uses." | Reference the canonical FOM at `tests/conformance/foms/good/`, do not copy. |
| "My test wants a piece of data from another agent's golden vectors." | Read the file via path, do not copy values into your code. |

---

## 5. Verification

The orchestrator verifies orthogonality at PR review:

- Diff includes only files in the agent's owned paths.
- No imports of frozen paths in feature PRs (only via the contract-change-request flow).
- No edits to other agents' status reports.

The pre-commit hook `scripts/check-frozen-paths.sh` enforces the frozen-path rule mechanically; everything else is review-time.

---

## 6. Why this matters (the short version)

Auto-approve sandboxes are fast and dangerous. Without explicit, exhaustive ownership:

- Two agents converge on the same file, last-write-wins corrupts intent.
- Helpful "drive-by fixes" leak across components and break invariants the original owner relied on.
- Status reports become unreliable because nobody knows who actually authored what.
- Verification at milestone gates becomes impossible — no clean baseline to diff against.

Strict orthogonality + working-directory isolation + orchestrator-driven dispatch (see `DISPATCH.md`) is how this project remains coherent across three autonomous workers.
