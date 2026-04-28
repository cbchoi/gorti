# Git & PR Workflow (Strict)

This document is the **single source of truth** for branching, commits, and pull requests. `AGENTS.md` references it; per-agent briefs reference it. No duplication.

Violations of these rules cause PR rejection. The orchestrator is the only entity that merges to `main`.

---

## 1. Branch Namespaces (Hard Rule)

Each agent operates in its own namespace:

| Agent | Namespace | Worktree |
|---|---|---|
| Agent A (claude-sandbox) | `agent/a/<topic>` | `/workspace/gorti-agent-a/` |
| Agent B (codex-sandbox) | `agent/b/<topic>` | `/workspace/gorti-agent-b/` |
| Agent C (gemini-sandbox) | `agent/c/<topic>` | `/workspace/gorti-agent-c/` |
| Orchestrator | `main`, `release/*`, `hotfix/*` | `/workspace/gorti/` |

- `<topic>` is kebab-case, ≤ 40 characters, descriptive: `agent/a/federation-lifecycle`, `agent/b/encoding-fixed-record`, `agent/c/pyjevsim-bridge-time-advance`.
- Reserved namespaces (agents may NOT push here):
  - `main` — orchestrator-merged only.
  - `release/*`, `hotfix/*` — orchestrator only.
- Agents may NOT push outside their own namespace, even read-only.
- `git push --force` to your own branch is permitted **only before the orchestrator opens review**. After review starts, force-push is forbidden — append fix commits instead.

### 1.1 Worktree setup (one-time)

Run from the orchestrator's checkout, after `git init` + first commit on `main`:

```bash
./scripts/setup-agent-worktrees.sh
```

This creates `/workspace/gorti-agent-{a,b,c}/` as siblings, each on a long-lived `agent/<x>/scratch` branch. Agent sandboxes are configured to operate ONLY in their respective worktree directory. The filesystem isolation is the backstop for the namespace policy: an agent's auto-approved write commands cannot touch another agent's directory because it isn't visible to the sandbox.

### 1.2 Per-task branch lifecycle

When a task is dispatched (per `docs/DISPATCH.md`):

```bash
# In your agent worktree, e.g. /workspace/gorti-agent-b/
git fetch origin
git checkout -b agent/b/<topic> origin/main   # branch off latest main
# ... TDD cycle, commits ...
git push -u origin agent/b/<topic>
# Open PR per §3.
```

After PR merges, orchestrator deletes the topic branch. Agent rebases onto new `main` for the next task.

---

## 2. Commit Format (Conventional Commits)

Every commit message follows [Conventional Commits](https://www.conventionalcommits.org/) with project conventions overlaid.

### Subject line
```
<type>(<scope>): <imperative summary>
```

- **type**: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `deps`, `ci`, `perf`.
- **scope**: package or component, lowercase, single word: `federation`, `encoding`, `time`, `pysdk`, `bridge`, `eventlog`, `fom`.
- **summary**: imperative mood, no trailing period, ≤ 72 chars total subject.

Good:
```
feat(encoding): add HLAfixedRecord codec
fix(time): correct LBTS tie-break on equal handles
test(eventlog): add 10x determinism harness
```

Bad:
```
Added new feature                       (no type/scope, past tense)
feat: stuff                             (vague summary)
feat(encoding): Added HLAfixedRecord codec for the encoding package.  (past tense, period, redundant)
```

### Body
- Wrap at 80 characters.
- Explain **why**, not what.
- Reference SRS requirement IDs that the commit advances:
  ```
  Implements: FR-ENC-1, FR-ENC-2
  ```
- Reference issues if applicable: `Refs: #42`.

### Footer
- Breaking changes: `BREAKING CHANGE: <description>` (pre-1.0 we still flag these explicitly so reviewers notice).
- Co-authors: omit unless human pair work.

### Single Logical Change Per Commit
- One commit = one reviewable change. Refactor first in its own commit, then add the feature in another. Tests in the same commit as the code under test.
- "feat + small unrelated fix" = two commits.

---

## 3. PR Lifecycle

### 3.1 Before opening

Run the self-check (see `CODING_CONVENTIONS.md` §6):

```bash
make fmt && make lint && make typecheck && make test && make determinism
```

If any fail, fix before opening the PR. Red PRs are auto-closed.

### 3.2 Opening the PR

- Title format: `[agent-X] <scope>: <subject>` matching the lead commit.
  Examples:
  - `[agent-a] federation: deterministic handle assignment`
  - `[agent-b] encoding: HLAvariableArray codec`
  - `[agent-c] bridge: pyjevsim ta to NER mapping`
- PR description follows the template in §3.3.
- Target branch: `main`.
- Cap: **400 LOC of changed application/test code** (excludes generated, fixtures, docs additions). Larger = split.
- Draft PRs are fine for early signal, but mark Ready for Review only when CI is green.

### 3.3 PR Description Template

Use this template verbatim (also in `docs/templates/pr-description.md` once that file exists; for now copy from here):

```markdown
## Closes
Closes: TASK-NNN

## What
<1–3 sentences: what this PR changes>

## Why
<the user/spec motivation; cite SRS requirement IDs>

## Implements
<comma-separated list of requirement IDs>
Implements: FR-ENC-1, FR-ENC-2

## Tests
- <list new/changed tests, classified per docs/TDD.md §4 — Specification / Property / Regression / Conformance / Integration / Determinism>
- Coverage: before X% / after Y% (on owned packages)

## TDD
- Commit pattern used (per docs/TDD.md §3): A (strict) / B (pragmatic) / C (bulk)
- [ ] Each new behavior has at least one test written before its implementation
- [ ] Test names describe behavior, not implementation
- [ ] No `time.Sleep` / wall-clock dependence in tests
- [ ] Removing any single new impl line breaks at least one new test (mutation sanity)
- [ ] Spec tests in `tests/spec/M<x>/` (if any) are not weakened or skipped

## Determinism
- [ ] No new wall-clock reads in core path
- [ ] No new unsorted-map iterations affecting output
- [ ] Determinism harness still green
- [ ] Or: not applicable (PR is outside core path) — explain

## Risk
<Low / Medium / High + 1 sentence>

## Out of scope (explicitly NOT in this PR)
<things you noticed but deliberately deferred>
```

### 3.4 CI Gates (mandatory pre-merge)

The orchestrator does not merge red. The PR must pass:

1. `make fmt` — formatting clean.
2. `make lint` — `golangci-lint` + `ruff` clean.
3. `make typecheck` — `mypy --strict` clean (Python only).
4. `make test` — `go test ./...` + `pytest` green.
5. `make determinism` — 10× determinism harness on touched core packages.
6. Coverage gate: PRs may not lower coverage below the per-package threshold in `CODING_CONVENTIONS.md` §2.5 / §3.7.
7. Frozen-paths hook: pre-commit hook rejects writes to `proto/**`, `rti/internal/core/**`, `docs/AGENTS.md`, `docs/srs.md`, `docs/sdd.md`, `docs/idd.md`, `docs/CODING_CONVENTIONS.md`, `docs/WORKFLOW.md`, `.github/**`.

### 3.5 Review

- Orchestrator review SLA: within 24 hours of "Ready for Review."
- Review categories:
  - **Blocker**: must fix before merge.
  - **Recommend**: should fix; orchestrator decides if it blocks.
  - **Nit**: optional; agent's call.
- Agent addresses comments by adding fix commits (`fix(scope): address review`); no force-push during review.
- Once all blockers resolved and CI re-green, orchestrator squashes (or merges, decision per PR) and closes.

### 3.6 Merge Strategy

- **Squash merge** is default for feature PRs (one commit lands on `main`).
- **Merge commit** for cross-cutting milestone integrations (preserves component history).
- Orchestrator decides per PR. Agents do not merge.

### 3.7 After Merge

- Branch deletion: orchestrator deletes after merge.
- Agent rebases their next branch on the new `main` before starting fresh work.

---

## 4. Special PR Types

### 4.1 Dependency PRs (`deps:`)

- `go.mod` / `go.sum`, `pyproject.toml` / `requirements.txt`, `package.json` changes go in their own PR.
- Title: `[agent-X] deps: <add|update|remove> <package>`.
- Description must include:
  - Why the dep is needed.
  - License of the dep (MIT-compatible only).
  - Whether it requires CGo or system deps.
  - Alternative considered (if any).
- Pinned exact version. No version ranges.
- Orchestrator may reject without alternative.

### 4.2 Contract Change Requests

If your work needs a frozen-path change (proto, core interfaces, AGENTS.md, ORTHOGONALITY.md, DISPATCH.md, etc.), do **not** edit the file. Instead:

1. Open an issue titled `contract-change-request: <one-line>`.
2. Body describes: what change, why, impact on other agents, alternatives considered.
3. Orchestrator decides; if approved, orchestrator opens the PR (or assigns it explicitly).
4. Wait. Do not start dependent work until contract change is merged. Per `docs/DISPATCH.md` §5, idle is acceptable — do not speculatively start.

### 4.3 Task Dispatch (Reference)

The full protocol is in `docs/DISPATCH.md`. Quick reference for PR authors:

- One PR closes one task (`docs/tasks/TASK-NNN.md`).
- PR description must include `Closes: TASK-NNN` so orchestrator can update the task file's Status.
- Acceptance criteria from the task brief are the PR's exit criteria; check each off in the PR description.
- **Completion sentinel mandatory**: PR's final commit MUST be `docs/tasks/signals/TASK-NNN.done` (`touch`-style file). Spec: `docs/tasks/signals/README.md`. Without the sentinel in the PR diff, the PR is draft.

### 4.3 Hotfix PRs

- Reserved for post-milestone, pre-release defects.
- Branch namespace: `agent/<x>/hotfix-<topic>`.
- Same gates apply; orchestrator may expedite review.

### 4.4 Verification PRs

- During milestone gates, verification artifacts (fuzz harnesses, naughty federates, scenario generators) live under `tests/verification/<milestone>/`.
- PR title: `[agent-X] verification(M2): naughty federate harness`.
- Implements clause cites the gate, not feature requirements: `Implements: M2 verification`.

---

## 5. Issue Conventions

| Label | Use |
|---|---|
| `verification:M0`..`verification:M5` | Findings from adversarial verification at milestone gates |
| `contract-change-request` | Requested edit to a frozen path |
| `question` | Agent stuck and waiting for orchestrator input |
| `bug` | Defect in merged code |
| `spec-clarification` | IEEE 1516 ambiguity needs resolution |
| `perf` | Performance concern (only relevant after M5 baseline) |

Issue title prefix matches the label: `verification(M2): RTI panics on duplicate federate name`.

---

## 6. Working Cadence

- **Daily**: agent opens at most 1–2 PRs per day. Quality > quantity.
- **Per-milestone**: agent opens a final integration PR if needed, then writes a status report (`docs/templates/status-report.md`) and links it from the milestone gate issue.
- **Idle agent**: pick from open `verification:` issues, or improve test coverage on owned packages, or refactor toward smaller files (per U-11). Do **not** start speculative new features.

---

## 7. What Agents NEVER Do

- Merge their own PR.
- Force-push during review.
- Edit frozen paths.
- Change dependencies in a feature PR.
- Skip CI ("CI is broken, but my code works locally").
- Bypass the orchestrator on contract decisions.
- Open a PR larger than 400 LOC of code without prior orchestrator approval.
- Use `git rebase -i` or `git filter-branch` on shared history.
- Commit secrets, credentials, large binaries, or generated artifacts not under `_generated/`.
