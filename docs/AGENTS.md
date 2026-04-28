# AGENTS.md — Operating Manual for Coding Agents

**Read this entire file before doing any work.** You will be quizzed on key sections at the M0 gate.

This file is the **operating manual** for the three coding agents (claude-sandbox = Agent A, codex-sandbox = Agent B, gemini-sandbox = Agent C). It tells you **what to read**, **where things live**, **what's frozen**, and **how the team operates**.

It does **NOT** contain code style, TDD, git workflow, ownership, or dispatch rules — those live in:

- **`docs/CODING_CONVENTIONS.md`** — strict, enforceable code rules (Go, Python, determinism, naming, testing).
- **`docs/TDD.md`** — Test-Driven Development methodology (Red-Green-Refactor, commit order, spec tests, mutation sanity).
- **`docs/WORKFLOW.md`** — branching, commits, PR template, review, merge.
- **`docs/ORTHOGONALITY.md`** — exhaustive path-to-owner map; zero co-ownership policy; producer/consumer rules.
- **`docs/DISPATCH.md`** — orchestrator-driven task assignment; agents do not self-select tasks.

If you find a conflict between this file and any of those, the dedicated doc wins. Report the conflict as an issue.

---

## 1. Required Reading (in order)

Before touching code:

1. `docs/srs.md` — Software Requirements Specification. Every feature must trace to a requirement ID (FR-*, NFR-*, IR-*, C-*).
2. `docs/sdd.md` — Software Design Document. Component decomposition, concurrency model, key algorithms.
3. `docs/idd.md` — Interface Design Document. Authoritative signatures for every external and internal interface.
4. `docs/CODING_CONVENTIONS.md` — strict code rules.
5. `docs/TDD.md` — TDD playbook. **All production code is written test-first.** Read before opening any feature PR.
6. `docs/WORKFLOW.md` — git workflow, PR rules.
7. `docs/ORTHOGONALITY.md` — path-to-owner map. **Verify your work touches only your owned paths before opening a PR.**
8. `docs/DISPATCH.md` — task dispatch protocol. **You do not self-select tasks; you wait for `docs/tasks/TASK-NNN.md` from the orchestrator.**
9. **Your own brief**: `docs/agent-a-rti-core.md`, `docs/agent-b-fom-encoding.md`, or `docs/agent-c-pysdk.md`.

For Agent C only: also read pyjevsim source (latest version), particularly `CoupledModel`, `AtomicModel`, `ta`, `output_handler`, `external_transition`, `select`.

## 2. Project Snapshot

- **What**: Open-source IEEE 1516-2010 (HLA Evolved) Run-Time Infrastructure in Go, with a Python federate SDK that wraps pyjevsim DEVS coupled models.
- **License**: MIT.
- **Standard**: IEEE 1516-2010 only — not 1516-2000, not 1.3, not 1516-2025.
- **Development model**: solo human + three sandboxed coding agents running with auto-approve permissions. Claude (orchestrator) writes briefs, reviews PRs, decides milestone advances. **No agent merges; the orchestrator merges.**
- **Working directory isolation**: each agent operates in its own git worktree (`/workspace/gorti-agent-{a,b,c}/`). The orchestrator works in `/workspace/gorti/`. Agents never touch another agent's worktree. See `docs/ORTHOGONALITY.md` §1.4 and `scripts/setup-agent-worktrees.sh`.
- **Task dispatch**: orchestrator writes `docs/tasks/TASK-NNN.md` assigning one task to one agent. Agents wait for dispatch; never self-select. See `docs/DISPATCH.md`.

## 3. Repo Layout

```
/workspace/gorti/
├── proto/rti/v1/        # gRPC contracts (FROZEN)
├── rti/                 # Go RTI server
│   ├── cmd/rtid/        # main binary
│   ├── internal/core/   # FROZEN interfaces (Transport, TimeManager, etc.)
│   ├── internal/        # Federation, Declaration, Object, Time impls (Agent A)
│   └── pkg/             # FOM, encoding (Agent B; importable by anyone)
├── pysdk/               # Python federate SDK (Agent C)
├── examples/            # Reference federates (go-pingpong, go-timed, pyjevsim)
├── tests/conformance/   # Cross-language conformance suite (encoding vectors etc.)
├── docs/                # SRS, SDD, IDD, AGENTS.md, conventions, workflow, briefs (FROZEN)
│   ├── reports/         # Status reports per milestone (each agent writes their own)
│   └── templates/       # Template files (status-report etc.)
├── scripts/             # Build/dev scripts
└── .github/workflows/   # CI (FROZEN)
```

## 4. Frozen Paths (Hard Rule)

The following paths reject writes from agent branches via pre-commit hook. **Do not attempt to edit them.** If your work needs a change here, follow the contract-change-request procedure in `docs/WORKFLOW.md` §4.2.

- `proto/**`
- `rti/internal/core/**`
- `docs/AGENTS.md` (this file)
- `docs/srs.md`
- `docs/sdd.md`
- `docs/idd.md`
- `docs/CODING_CONVENTIONS.md`
- `docs/TDD.md`
- `docs/WORKFLOW.md`
- `docs/ORTHOGONALITY.md`
- `docs/DISPATCH.md`
- `docs/agent-a-rti-core.md`, `docs/agent-b-fom-encoding.md`, `docs/agent-c-pysdk.md`
- `docs/tasks/README.md`, `docs/tasks/TASK-template.md`, `docs/tasks/TASK-*.md` (task briefs; agents read only)
- `docs/tasks/signals/README.md` (frozen format spec; the `TASK-NNN.done` sentinel files inside this directory are the **one exception** — agents create their own task's `.done` file as completion signal per `docs/DISPATCH.md` §10)
- `tests/spec/**` (orchestrator-provided specification tests; agents may add but not weaken)
- `.github/**`
- Top-level `LICENSE`, `README.md`, `CHANGELOG.md`, `CHANGELOG-MASTERPLAN.md`

If you find yourself thinking "I'll just add one field to the proto," STOP. Open a `contract-change-request:` issue and wait for orchestrator decision.

`docs/reports/` and `docs/templates/` are NOT frozen — agents write status reports there.

## 5. Where Things Live (Index)

| Topic | Document |
|---|---|
| What we're building | `docs/srs.md` |
| How it's structured | `docs/sdd.md` |
| What the interfaces look like | `docs/idd.md` |
| How to write code | `docs/CODING_CONVENTIONS.md` |
| How to test (TDD) | `docs/TDD.md` |
| Who owns which path | `docs/ORTHOGONALITY.md` |
| How tasks are assigned | `docs/DISPATCH.md` |
| Active task briefs | `docs/tasks/TASK-*.md` |
| How to commit and PR | `docs/WORKFLOW.md` |
| Status report template | `docs/templates/status-report.md` |
| Your specific tickets | your per-agent brief |

## 6. Milestone Protocol

The team operates in milestones M0..M5 defined in `docs/srs.md` §10.2. At each gate:

### 6.1 Owner role (when applicable)

- Implement deliverables listed in your brief for this milestone.
- Verify your own exit criteria locally before declaring "ready for gate."
- Open the final integration PR for the milestone.

### 6.2 Verifier role (every milestone, every agent)

- Perform the adversarial verification activities listed in your brief at the relevant gate (fuzzers, naughty federates, byte-diff harness, etc.).
- File findings as GitHub issues with label `verification:M<x>`.
- A verification finding ≠ optional. Blockers stop advancement.

### 6.3 Status report (mandatory at every gate, every agent)

When the milestone gate opens (orchestrator declares it):

1. Copy `docs/templates/status-report.md` to `docs/reports/M<x>/agent-<a|b|c>.md`.
2. Fill in honestly: completed deliverables, slips, defects, verification findings, recommendations, risks.
3. Open a PR adding the report (this PR follows normal `WORKFLOW.md` rules — but the orchestrator does NOT block it on coverage gates; it's a doc PR).
4. Link the PR from the milestone gate issue.

The orchestrator reads all three reports together, decides go/no-go, and revises the master plan (`CHANGELOG-MASTERPLAN.md`) before issuing the next milestone's tasks. Reports are how you influence the plan.

### 6.4 Advancement

- Only the orchestrator advances milestones.
- Once advanced, agents rebase on the new `main` and pick up from their brief's next-milestone section.

## 7. What to Do When Stuck

In order:

1. Re-read the SRS section, the SDD component description, and the IDD interface for what you're building.
2. Look at the reference implementation (Portico — Java, CDDL). Understand the *approach*, do not copy code.
3. Open a `question:` or `spec-clarification:` issue on your branch describing what you're stuck on, what you tried, and what you'd recommend.
4. Wait for orchestrator response. Do **not** make speculative architectural decisions while waiting; pick up another ticket from your brief or an open `verification:` issue.

## 8. Anti-Goals (Things You Will Be Tempted to Do — Don't)

These are things that are systematically wrong for this project, regardless of how reasonable they feel:

- **"I'll just add this field to the proto for convenience."** → No. Contract-change-request.
- **"I'll implement Ownership Management while I'm in here."** → No. Deferred per SRS §9.
- **"I'll add a small abstraction layer for future flexibility."** → No. Three similar lines beats premature abstraction. Lean MVP.
- **"I'll add backwards-compat shims because the API might change."** → No. Pre-1.0 freedom — change the code.
- **"I'll skip the determinism test because it's flaky."** → No. Determinism flake = real bug; find it.
- **"I'll write the tests after, this code is straightforward."** → No. Test-first per `docs/TDD.md`. Reviewers walk git history.
- **"I'll relax this spec test in `tests/spec/M<x>/` so my impl passes."** → No. Spec tests are the milestone contract. Make your impl meet the test, not the other way around.
- **"I'll start on the next item in my brief while the orchestrator hasn't dispatched a task yet."** → No. Wait for `docs/tasks/TASK-NNN.md`. See `docs/DISPATCH.md` §4.1. Idle is fine; speculative work is not.
- **"I noticed Agent X has a typo / small bug; I'll fix it since I'm here."** → No. File an issue. Per `docs/ORTHOGONALITY.md`, you write only paths you own.
- **"I'll cd into another agent's worktree to peek at their work-in-progress."** → No. Read merged code on `main` from your own worktree. Agent worktrees are isolated by design.
- **"I opened the PR; that's enough — orchestrator will see it."** → No. The completion sentinel `docs/tasks/signals/TASK-NNN.done` MUST be the final commit on your topic branch. Without it the PR is treated as draft. See `docs/DISPATCH.md` §10.
- **"I'll touch the sentinel before I finish, since I'm about to be done."** → No. The sentinel is the named act of completion. Touch it AFTER `make verify` is green, AFTER all acceptance criteria are met, AFTER the work is actually done. Anything else lies to the orchestrator.
- **"I'll bump this dep to latest."** → No. Separate `deps:` PR with justification.
- **"I'll merge my own PR since it's small."** → No. Orchestrator merges. No exceptions.
- **"I'll edit `docs/sdd.md` because the design changed."** → No. Open a `contract-change-request:` issue; orchestrator updates the SDD.
- **"I'll add a TODO and revisit."** → No. `TODO(#<issue>): ...` only — file the issue first.
- **"I'll skip the status report; the PRs already say what I did."** → No. Reports drive plan revisions; PRs drive code review. Different artifacts.

## 9. Sandbox Safety Reminders

You operate with auto-approved actions. That makes you fast and dangerous. The orchestrator has set up these guardrails — respect them:

- You can only push to your own branch namespace (`agent/{a|b|c}/<topic>`).
- Pre-commit hooks reject writes to frozen paths. Do not attempt to bypass.
- Force-push outside your own branch will fail; force-push to your own branch is permitted only before review starts.
- Dependency changes require a separate `deps:` PR.
- Never run destructive shell commands (`rm -rf`, `git reset --hard`, `git clean -fdx`) without an explicit ticket calling for them.
- Never commit secrets, credentials, or large binaries.

If a guardrail blocks something you genuinely need to do: that's a signal to stop and ask the orchestrator, not to find a workaround.
