# Task Completion Signals

When an agent finishes a task, it creates a **sentinel file** in this directory as the **final commit** of the topic branch:

```
docs/tasks/signals/TASK-NNN.done
```

The sentinel signals to the orchestrator: *"TASK-NNN is complete; the open PR is ready for review."*

This README is FROZEN — orchestrator-only. The `TASK-NNN.done` files inside this directory are the **one exception** to the frozen-paths rule for `docs/tasks/**`: agents may create their own task's `.done` file, and only that file.

---

## 1. Why a sentinel?

Auto-approve sandboxes work async. The orchestrator is not watching every push. The sentinel is a low-bandwidth, durable, in-tree signal that:

- Survives in git history (audit trail of when each task completed).
- Is visible in the PR's diff so review starts with the agent's self-declaration of done.
- Lets the orchestrator scan one directory to see what's awaiting review without traversing GitHub PR list state.
- Forces the agent to make completion an explicit, named act — not "I think it's done, maybe."

Without the sentinel committed in the PR's diff, the PR is treated as **draft / not ready for review**, even if it is technically open.

---

## 2. Filename

Exact form: `TASK-NNN.done` where NNN matches the task ID exactly (zero-padded, three digits).

Examples: `TASK-001.done`, `TASK-042.done`, `TASK-117.done`.

Forbidden:
- `TASK-1.done` (must be zero-padded)
- `task-001.done` (lowercase)
- `TASK-001-encoding.done` (no suffix; the task ID is the only identifier)
- `TASK-001.DONE` (lowercase extension)
- `TASK-001` (must have `.done` extension)

---

## 3. Content

The file MAY be empty (`touch`-style). The filename alone carries the signal.

If the agent includes content, use this markdown format:

```markdown
- agent: agent-b
- branch: agent/b/encoding-fixed-record
- final-commit: <git-sha-short>
- timestamp: 2026-04-29T14:30:00Z
- pr: https://github.com/.../pull/42
- summary: |
    Implemented HLAfixedRecord codec with explicit padding tests.
    All M1 spec tests for fixed-record vectors pass.
    Coverage on rti/pkg/encoding: 87%.
- acceptance:
    - all criteria from TASK-NNN.md passing (see PR description)
```

Content fields are advisory — useful for audit, not required. The PR description (per `docs/WORKFLOW.md` §3.3) is the canonical record of acceptance criteria. The PR URL is best-effort: if the PR isn't open at commit time, write `pr: pending` and the orchestrator finds it via branch.

---

## 4. Lifecycle

```
1. Agent finishes work in their worktree.
2. Agent runs `make verify` locally; all green.
3. Agent creates docs/tasks/signals/TASK-NNN.done as the FINAL commit on the topic branch.
   Commit message: chore(tasks): signal TASK-NNN done
4. Agent pushes the branch.
5. Agent opens the PR (per docs/WORKFLOW.md).
6. Orchestrator reviews the PR.
7. On merge, the sentinel arrives on `main`.
8. The sentinel is NOT deleted. It accumulates as an audit log.
9. In the next dispatch turn, the orchestrator updates docs/tasks/TASK-NNN.md
   Status: DONE.
```

---

## 5. Constraints

- The sentinel MUST be the **final commit** on the topic branch. If you push more commits after it (e.g. addressing review comments), append a *new* sentinel commit at the end as well — repeat the touch.
- One sentinel per task. Do not create `TASK-NNN.done` for a task assigned to another agent.
- Do not create a sentinel for a task you have not been dispatched (no TASK-NNN.md exists or you are not the assignee).
- Do not delete or modify another agent's sentinel — it is part of the audit log.
- An agent may create only their **own** task's sentinel (assignee match).

The pre-commit hook `scripts/check-frozen-paths.sh` allows this specific path pattern; all other writes under `docs/tasks/` are still rejected from agent branches.

---

## 6. Anti-patterns

| Anti-pattern | Why wrong |
|---|---|
| Creating the sentinel before the work is actually done | Lies to the orchestrator; review starts on incomplete code |
| Sentinel as the *only* commit (no actual work) | Orchestrator catches at review |
| Empty sentinel for a 400-LOC PR | Acceptable but discouraged — a one-line summary helps |
| Force-pushing the topic branch and dropping the sentinel | Forbidden post-review-open per `docs/WORKFLOW.md`; if needed pre-review, re-add sentinel as final commit |
| Touching another task's sentinel "to be helpful" | Per `docs/ORTHOGONALITY.md`, write only what you own |
| Putting acceptance proof in the sentinel instead of the PR description | PR description is canonical; sentinel is signal |
