# Milestone Status Report — Template

Each agent fills in a copy of this template at every milestone gate. The file goes in `docs/reports/<milestone>/<agent>.md` (e.g. `docs/reports/M2/agent-a.md`). Linked from the milestone gate issue.

The orchestrator reads all three reports together to decide milestone advance, revise the master plan, and reissue tasks.

**Be honest.** A status report that hides slips, partial work, or known defects damages the project more than the slip itself. The orchestrator will not penalize honest under-delivery; the orchestrator will reject dishonest reports.

---

```markdown
# Milestone <Mx> Status Report — Agent <A|B|C>

**Date**: YYYY-MM-DD
**Author**: agent-<a|b|c> (claude-sandbox / codex-sandbox / gemini-sandbox)
**Milestone**: <Mx — short name>
**Role at this gate**: Owner / Verifier / Both

---

## 1. Owner Section (skip if you are not the M<x> owner)

### 1.1 Deliverables completed
For each deliverable listed in your brief for this milestone:

- [x] **<deliverable name>** — landed in PR #<n>. Notes: <if any>
- [x] **<deliverable name>** — landed in PR #<n>.
- [ ] **<deliverable name>** — NOT DONE. See §1.3.

### 1.2 Exit criteria (from SRS §10.2 / your brief)
For each exit criterion:

- [x] **<criterion>** — evidence: <link to test output, perf number, hash diff>
- [ ] **<criterion>** — NOT MET. See §1.3.

### 1.3 Slips, cuts, and known defects
List anything you did NOT complete vs the plan, with reason:

- **<deliverable / criterion>**: <why slipped — be specific. e.g. "discovered the spec on FixedRecord padding is ambiguous; spent 2 days; raised spec-clarification issue #N">
- **Known defect**: <description, severity, suggested fix path>. File a `bug` issue and link it.

### 1.4 Lines of code shipped (rough)
- Application code: <N> LOC
- Test code: <N> LOC
- Coverage on owned packages: <%>

### 1.5 Time spent (rough)
- Design / spec reading: <hours/days>
- Implementation: <hours/days>
- Testing: <hours/days>
- Verification of others' work: <hours/days>
- Blocked / waiting: <hours/days>

---

## 2. Verifier Section (always fill in; you verify other agents at every gate)

### 2.1 Verification activities completed
For each verification responsibility in your brief at this milestone:

- [x] **<activity>** against agent-<X>'s work — findings filed as issue #<n> with label `verification:M<x>`.
- [ ] **<activity>** — NOT DONE. Reason:

### 2.2 Findings summary
- Critical (blocks gate): <count>
- Recommend (should fix): <count>
- Nit / observation: <count>

Link to the most important finding(s):
- #<n> — <one-line summary>

### 2.3 Confidence in others' work
For each agent you verified:

- **Agent <X>**: high / medium / low confidence that the deliverable meets exit criteria. <one-line reason>

---

## 3. Cross-cutting Observations

### 3.1 What worked well
- <process observation, e.g. "the golden-vector handoff from agent-b unblocked my Python encoder cleanly">

### 3.2 What was painful
- <process observation, e.g. "the Phase 0 proto missed an error-code field; cost a full PR cycle">

### 3.3 Spec / requirements gaps surfaced
- <new ambiguities or missing requirements you found while building>
- File `spec-clarification` issues for each.

---

## 4. Recommendations to the Orchestrator

What should the orchestrator change in the master plan? Be concrete.

### 4.1 Plan revisions you suggest
- **Revise**: <what>. Reason: <why>. Suggested change: <how>.
- e.g. "Revise: M3 stall-timeout test. Reason: my fault-injection harness can also drive M2 verification cheaper. Suggested: pull stall-test scaffolding earlier into M2 verification deliverables."

### 4.2 Scope adjustments you suggest
- Items you want to defer further (out of M<x+1>): <what + reason>
- Items you want to pull earlier: <what + reason>

### 4.3 Tooling / infra you need
- <e.g. "request a benchmark CI job for encoding ahead of M5">

---

## 5. Risks for the Next Milestone

What could go wrong? Be honest about uncertainty.

| Risk | Likelihood (L/M/H) | Impact (L/M/H) | Mitigation you propose |
|---|---|---|---|
| <e.g. pyjevsim API drift on next release> | L | M | Pin exact version; add smoke test |
| <e.g. LBTS algorithm doesn't scale past 50 federates> | M | M | Measure at M5; do not optimize until then |

---

## 6. Asks of the Orchestrator

Anything that requires the orchestrator's action before you can start the next milestone:

- [ ] Decision on issue #<n> (`spec-clarification` / `contract-change-request`)
- [ ] Merge order for PRs #<a>, #<b> (cross-component dependency)
- [ ] Other: <...>

---

## 7. Self-Assessment

In one paragraph: how do you (the agent) think you performed this milestone? Were the briefs clear? Were the conventions enforceable? What would let you ship faster next time?
```

---

## Notes for the Orchestrator

When you read the three reports together:

1. **Cross-check claims**: agent-A says criterion X met → look at the linked evidence yourself.
2. **Cross-check verification findings**: a high-impact finding by one agent against another may surface missing exit criteria, not just a bug.
3. **Watch for systematic gaps**: if all three agents report the same painful process issue, fix the process before issuing M<x+1>.
4. **Re-baseline the plan**: revise SRS exit criteria, milestone scope, or per-agent briefs based on §3 and §4. Document the revision in a top-level `CHANGELOG-MASTERPLAN.md` entry so the master plan history is auditable.
5. **Decide go/no-go**: only advance to the next milestone when blockers are resolved or explicitly accepted-and-deferred.
