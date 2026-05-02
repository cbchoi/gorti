# Task Dispatch Protocol

The orchestrator decides which agent works on what, and when. Agents do not self-select tasks.

This document is FROZEN — only the orchestrator may edit. Companion: `docs/ORTHOGONALITY.md` (path ownership), `docs/WORKFLOW.md` (git/PR rules), `docs/AGENTS.md` (operating manual).

---

## 1. Why explicit dispatch

Agents run with auto-approve. Without explicit task assignment:

- Two invocations of the same agent might pick up the same brief item and produce duplicate or conflicting PRs.
- Agents drift across milestones (e.g. starting M3 work while M2 is still red).
- Status reports become noisy because work order is non-deterministic.
- The orchestrator loses visibility into "what is in flight right now."

Explicit dispatch is a small overhead that buys clear coordination across three autonomous workers. The mechanism is intentionally lightweight — one task brief per task, one PR per task, no elaborate sign-offs.

---

## 2. The flow

```
+-------------------+
| Orchestrator      |  1. Picks a task from the milestone backlog.
| writes TASK-NNN   |  2. Writes docs/tasks/TASK-NNN.md naming the assigned agent.
+--------+----------+  3. Commits + pushes to main.
         |
         v
+-------------------+
| Agent (assigned)  |  4. Pulls latest main into its worktree.
| picks up task     |  5. Reads the task brief.
+--------+----------+  6. Acks via first commit on topic branch.
         |
         v
+-------------------+
| Agent works       |  7. Creates topic branch agent/X/<topic>.
| TDD per docs/     |  8. Implements test-first per docs/TDD.md.
| TDD.md            |
+--------+----------+
         |
         v
+-------------------+
| Agent SIGNALS     |  9. Runs `make verify` locally; all green.
| completion        | 10. Creates docs/tasks/signals/TASK-NNN.done as the FINAL
+--------+----------+     commit. (See §10 + docs/tasks/signals/README.md)
         |             11. Pushes branch; opens PR per docs/WORKFLOW.md.
         v
+-------------------+
| Orchestrator      | 12. Sees the sentinel in the PR diff -> review starts.
| reviews + merges  | 13. Reviews PR; sends back, or merges.
+--------+----------+ 14. Sentinel arrives on main; updates TASK-NNN.md DONE.
         |             15. Dispatches the next task.
         v
+-------------------+
| Cycle continues   |
+-------------------+
```

The cycle is small. A task is sized to ~1–3 days of agent work, not a whole milestone.

---

## 3. Task brief format

Each task lives in its own file under `docs/tasks/TASK-NNN.md` (zero-padded 3-digit ID). Use the template at `docs/tasks/TASK-template.md`.

Required fields:

```markdown
# TASK-NNN: <short title>

| Field | Value |
|---|---|
| Status | DISPATCHED / IN_PROGRESS / IN_REVIEW / DONE / CANCELLED / BLOCKED |
| Assignee | agent-a / agent-b / agent-c |
| Milestone | M0 / M1 / M2 / M3 / M4 / M5 |
| Created | YYYY-MM-DD |
| Updated | YYYY-MM-DD |
| Depends-on | list of TASK-NNN, comma-separated; or "none" |
| Blocks | list of TASK-NNN, or "none" |

## Goal
<1-3 sentences: what behavior must exist when this task is DONE>

## Scope (in)
- <bullet list of concrete deliverables>

## Scope (out)
- <bullet list of things explicitly NOT in this task>

## Implements
<comma-separated SRS requirement IDs (FR-*, NFR-*, IR-*, C-*)>
<and/or spec test files in tests/spec/M<x>/ this task makes pass>

## TDD entry point
<which spec test or behavior is the first red test the agent should target>

## Acceptance criteria
- [ ] <objective, testable; e.g. "tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics passes">
- [ ] <e.g. "go test ./rti/pkg/fom/parser/... green">
- [ ] <e.g. "coverage on rti/pkg/fom/parser ≥ 75%">

## Notes / hints
<optional: pointers to spec sections, gotchas, references>
```

The orchestrator commits this file on `main`. Agent reads it from their worktree after `git pull`.

---

## 4. Agent constraints

These are HARD rules. Violations cause PR rejection regardless of code quality.

### 4.1 No self-selection
Agents may not start work on a deliverable from their per-agent brief without an open `TASK-NNN.md` assigning it. The brief lists what an agent *can* be asked to do; it does not authorize starting.

### 4.2 No reassignment
If a task is dispatched to Agent A but Agent B notices it should belong to them, Agent B does NOT pick it up. Agent B opens a `question:` issue. Orchestrator reassigns by closing TASK-NNN and opening TASK-MMM under the right assignee.

### 4.3 No multi-task PRs
One PR closes one task. Conventional Commits already require a single logical change per commit (`docs/WORKFLOW.md` §2); this rule extends to PRs.

### 4.4 Capacity
At most **one IN_PROGRESS task per agent** at any time. If you finish task X and there is no dispatched task Y, you are idle (see §5). Do not start unassigned work to "stay productive."

### 4.5 Acknowledgement window
Within ~1 day of a task being dispatched (Status: DISPATCHED), the assigned agent must either:
- Move to IN_PROGRESS by opening a draft PR (or issue comment per task notes), OR
- Open a `question:` issue if the brief is unclear.

If neither happens, orchestrator may reassign or cancel.

### 4.6 No silent abandonment
If an agent decides a task cannot be completed as specified (e.g. spec ambiguity, missing dependency, scope creep), the agent opens a `question:` or `spec-clarification:` issue. The orchestrator decides whether to revise the task, reassign, or cancel. **Do not** silently move on to other work.

---

## 5. Idle agent protocol

When an agent has no IN_PROGRESS task:

1. **Check open verification issues** assigned to them (`verification:M<x>` label, your-agent assignee). Address those — verification work does not require a TASK-NNN; it is part of the milestone gate.
2. **Improve test coverage** on owned packages (per `CODING_CONVENTIONS.md` thresholds). Open a small `chore: improve coverage in <pkg>` PR.
3. **Refactor toward smaller files** if any owned file exceeds the limits in `CODING_CONVENTIONS.md` U-11. Open a `refactor:` PR.
4. **Wait.** If none of the above apply, do nothing. Idle is an acceptable state. Do NOT speculatively start new feature work; do NOT touch other agents' code; do NOT submit "while I'm here" cleanups.

---

## 6. Dependency sequencing

Tasks may have `Depends-on:` listing prerequisite tasks. The agent MUST NOT start a task whose dependencies are not all DONE. The brief lists this explicitly so it is unambiguous.

If a dependency is dispatched to a different agent, the dependent task waits. The orchestrator sequences the milestone backlog so dependency chains do not block any agent indefinitely (see §7).

Example dependency chain for M1:

```
TASK-001 (Agent B): minimal parser skeleton — passes ParseMinimalGoodFOM
   depends-on: none
TASK-002 (Agent B): FOM-001 detection
   depends-on: TASK-001
TASK-003 (Agent B): FOM-002 detection
   depends-on: TASK-001
TASK-004 (Agent B): primitive encoders for HLAinteger32BE / HLAinteger64BE
   depends-on: none (parallel with TASK-001 etc.)
TASK-005 (Agent C): Python decoder against orchestrator's seed vectors
   depends-on: TASK-004 (needs working Go encoder to validate against)
```

Agent B works TASK-001 through TASK-004 sequentially or interleaved by orchestrator dispatch; Agent C waits until TASK-004 is DONE.

---

## 7. Cancellation and blocking

### 7.1 Cancellation

Tasks can be cancelled by setting `Status: CANCELLED` and explaining in the file's Notes section. Reasons include:

- Spec clarification reversed an assumption.
- A dependency made the task irrelevant.
- A defect in another agent's code requires re-scoping.

Cancelled tasks are not deleted; they remain in `docs/tasks/` for traceability. The orchestrator opens a replacement TASK-NNN if needed.

### 7.2 Blocking

A task whose `Depends-on:` chain is satisfied may still be unsafe to start because of an external blocker — most commonly an open `contract-change-request:` issue. In that case the orchestrator sets `Status: BLOCKED` and writes a `## Blocked (YYYY-MM-DD)` section in the task file referencing the blocker (issue number, PR, or other artifact).

Agents MUST NOT pick up a BLOCKED task. Once the external blocker is resolved, the orchestrator flips the status back to `DISPATCHED` (and updates the `Updated:` field) before the agent acks. Blocked tasks count against the milestone backlog the same as other tasks; they are not cancelled and their ID is not reused.

The distinction:
- `Depends-on:` is a **task-graph** dependency (must wait for upstream `TASK-MMM.done` to land on `main`).
- `BLOCKED` is an **external-artifact** dependency (must wait for a non-task event — issue resolution, contract decision, third-party data arrival).

---

## 8. Naming and numbering

- IDs are zero-padded three digits: `TASK-001`, `TASK-002`, etc.
- IDs are append-only — never reuse a cancelled ID.
- The first task is `TASK-001`. Numbering does not reset per milestone.
- A TASK file's name MUST match its ID exactly: `docs/tasks/TASK-007.md`.

---

## 10. Completion sentinel (mandatory)

When an agent completes a task and is ready for review, they MUST create a sentinel file:

```
docs/tasks/signals/TASK-NNN.done
```

as the **final commit** of the topic branch, *before* opening the PR (or as part of the same push). The full spec is in `docs/tasks/signals/README.md`.

### Why

- Asynchronous coordination: the orchestrator does not watch every push; the sentinel is a durable, in-tree signal that says "TASK-NNN is ready for review."
- Audit trail: the sentinel commits accumulate on `main`, providing a permanent record of when each task completed.
- Forces explicit completion: the agent cannot drift on "kind of done"; the sentinel commit is a named act.

### Rules

- Filename is exact: `TASK-NNN.done` (zero-padded, three digits, lowercase extension).
- File MAY be empty (`touch`-style); content is optional advisory metadata.
- The sentinel MUST be the **final commit** on the topic branch when the PR is opened.
- If review feedback requires more commits, push them, then **re-touch** the sentinel as a new final commit.
- Do not create sentinels for tasks not assigned to you; do not delete or modify other agents' sentinels.
- Without a sentinel in the PR's diff, the PR is treated as draft / not ready for review.

The pre-commit hook (`scripts/check-frozen-paths.sh`) allows the specific pattern `^docs/tasks/signals/TASK-[0-9]+\.done$`. All other writes under `docs/tasks/**` remain frozen for agent branches.

---

## 11. What the orchestrator commits to

Reciprocally, the orchestrator commits to:

- **Review PRs within 24 hours** of "Ready for Review" (per `WORKFLOW.md`).
- **Dispatch the next task within 1 day** of an agent finishing the previous one (when the milestone has more work for that agent).
- **Resolve `question:` and `spec-clarification:` issues within 1 day** so agents are not blocked.
- **Update master plan** (`CHANGELOG-MASTERPLAN.md`) after every milestone gate, integrating status reports.

If the orchestrator fails to meet these, agents document it in their next status report; this informs whether the multi-agent workflow is sustainable for the project.

The "orchestrator commits to" section above was previously §9 — it is now §11, as §10 documents the completion sentinel.
