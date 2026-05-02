# Milestone-Check Loop

A read-only periodic probe that walks `docs/srs.md` §10.2 milestone exit criteria against the actual repo state. Implemented as `scripts/check-milestones.sh`. This document is the design rationale and operational guide.

This document is FROZEN — only the orchestrator may edit. Companion: `docs/srs.md` (exit criteria), `docs/DISPATCH.md` (status semantics), `docs/AGENTS.md` (verification protocol).

---

## 1. Why a recurring check

The project has six milestones (M0..M5), three sandboxed coding agents, ~89 dispatched tasks, and a strict orthogonality policy. State drift is invisible by default — tasks land on agent branches, sentinels accumulate, untracked agent attempts grow, and the orchestrator only sees "is the milestone done?" if someone runs the right test sequence.

The check loop answers a single question on demand: **for each milestone, is every exit criterion in `docs/srs.md` §10.2 currently satisfied?**

It is *not* a CI gate (CI gates are in `.github/workflows/`). It is the orchestrator's at-a-glance dashboard, equivalent to the manual question "where are we?" but executable, deterministic, and grep-friendly.

## 2. What the script reports

For each milestone:

- The exit criteria from SRS §10.2, restated.
- A line per criterion with `✓` / `∘` / `✗`:
  - `✓` PASS — the probe ran and the criterion is satisfied.
  - `∘` PENDING — the probe ran and the criterion is not satisfied (expected RED state for in-flight or unstarted milestones).
  - `✗` FAIL — the probe ran and the criterion was *expected* to be satisfied but isn't (regression).
- A milestone-level rollup: `DONE` / `DONE_WIP` / `IN_PROGRESS` / `NOT_STARTED` / `REGRESSED`.

Plus structural sanity that's not in SRS but governs the protocol (`docs/DISPATCH.md`, `docs/ORTHOGONALITY.md`):

- TASK files committed to `main` (≥85 required for full backlog reachability).
- Sentinels on `main` referencing TASK briefs that exist on `main` (no dangling sentinels).
- Open `contract-change-request` issues that may block work.
- BLOCKED tasks per `docs/DISPATCH.md` §7.2.
- Frozen-path drift on the current branch (per `docs/ORTHOGONALITY.md` §5).

## 3. Status semantics

| Status | Meaning |
|---|---|
| `DONE` | All criteria pass on a clean working tree at HEAD. The milestone is truly DONE. |
| `DONE_WIP` | All criteria pass, but the working tree has uncommitted changes — the apparent DONE may evaporate on commit. The orchestrator should commit, then re-run, before declaring DONE. |
| `IN_PROGRESS` | Some criteria pass, some pending. Normal mid-milestone state. |
| `NOT_STARTED` | No criteria pass. Normal pre-milestone state — fixtures and harnesses don't yet exist. |
| `REGRESSED` | A criterion previously passing has flipped to failing on committed state. Investigation required. |

The script's exit code is `0` unless `REGRESSED` was set. `DONE_WIP` does not trigger non-zero exit because uncommitted-but-passing is a normal mid-development state, not a regression.

## 4. Probe coverage per milestone

Each milestone's probe set maps to the SRS §10.2 exit criteria and to the milestone-gate spec tests that the orchestrator has pre-written (`tests/spec/M<x>/`).

### M0 — Orchestrator scaffolding
- ≥8 proto files in `proto/rti/v1/` (the wire contract).
- ≥10 Go files in `rti/internal/core/` (the frozen interface set).
- `tests/spec/M1/` exists (orchestrator pre-work for the next milestone is a prerequisite of M0 closure).
- `go build ./...` succeeds (compile-clean tree).
- *Manual:* conventions quiz — not auto-checkable; logged as informational.

### M1 — FOM parser + MIM + encoding rules (Agent B)
- ≥10 bad-FOM fixtures under `tests/conformance/foms/bad/` (the "10 malformed FOMs" criterion).
- `TestSpec_M1_BadFOMDiagnostics` passes (every code from FOM-001..FOM-013 + FOM-101 is detected).
- `TestSpec_M1_PrimitiveVectorsRoundTrip` passes (encoder byte-identical against golden vectors).
- Coverage on `rti/pkg/encoding` ≥ 80%.

### M2 — Federation + Declaration + Object + EventLog + gRPC (Agent A)
- `tests/spec/M2/` exists (orchestrator pre-work).
- `examples/go-pingpong/main.go` exists.
- `examples/go-pingpong/determinism_test.go` exists (10× determinism harness).
- `examples/go-pingpong/replay_test.go` exists (replay byte-identical).

### M3 — Time management (Agent A)
- `tests/spec/M3/` exists.
- `examples/go-timed/main.go` exists.
- `examples/go-timed/determinism_test.go` exists (20-scenario harness).
- `examples/go-timed/stall_test.go` exists.

### M4 — Python SDK + pyjevsim bridge (Agent C)
- `tests/spec/M4/` exists.
- `pysdk/pyproject.toml` exists (package bootstrapped).
- `examples/pyjevsim/runner.py` exists.
- `pytest pysdk/tests/test_encoding_conformance.py` is green (Python encoder matches all vectors).
- `mypy --strict pysdk/` is clean.

### M5 — End-to-end + modes + perf baseline (Mixed)
- `tests/spec/M5/` exists.
- `examples/pyjevsim/cross_lang_test.py` exists.
- `pysdk/tests/test_modes.py` exists.
- `docs/reports/M5/agent-a.md` exists with recorded baseline numbers.

## 5. Operational wiring

The script is invoked one of three ways. Pick by cadence; never run more than one mode against the same repo.

### 5.1 On-demand (manual)

```bash
bash scripts/check-milestones.sh
```

Useful before dispatching a task, after merging a PR, or when reviewing a status report. Takes ~2–5 seconds (mostly the Go test invocations).

### 5.2 Local self-paced loop

For active development, run on an interval via Claude Code's `/loop` skill:

```
/loop 60m bash scripts/check-milestones.sh
```

Or self-paced:

```
/loop bash scripts/check-milestones.sh
```

The model picks delays based on what it sees (e.g., shorter when a milestone is close to flipping; longer when nothing is changing).

### 5.3 Scheduled remote agent (recommended for sustained projects)

Via Claude Code's `/schedule` skill, a remote agent runs the check on a cron and posts findings as an issue comment:

```
/schedule

Cron: 0 9 * * 1   # every Monday at 09:00 UTC

Prompt:
  Run `bash /workspace/scripts/check-milestones.sh > /tmp/status.txt`. If exit
  code is non-zero (regression), open a GitHub issue titled
  "milestone-status regression YYYY-MM-DD" with the report as the body and
  apply labels `regression`, `milestone-status:auto`. Otherwise, find the most
  recent issue with label `milestone-status:auto` and append the report as a
  comment; create a fresh issue if none exists. Always include the commit SHA
  the report ran against.
```

The label `milestone-status:auto` (created in the project's label set) keeps the cadence visible without spamming.

### 5.4 CI integration (optional)

A workflow under `.github/workflows/milestone-check.yml` can run the script on every push to `main` and fail the build on regression. Not currently wired up; would belong to an orchestrator-side CI hardening task post-M5.

## 6. What the script does NOT do

- **Dispatch tasks.** That's the orchestrator's call (`docs/DISPATCH.md` §2 step 2).
- **Cancel tasks.** Same.
- **Modify the repo.** Read-only — no `git` mutating commands, no file writes.
- **Run agents.** It reports state; agents are launched separately.
- **Replace milestone status reports** (`docs/reports/M<x>/agent-<a|b|c>.md`). Those are agent-authored narratives with verification findings, recommendations, and risks. The check script provides the *data*; agents provide the *interpretation*.

## 7. Adding a new probe

When a new requirement enters SRS or a new spec test lands:

1. Identify which milestone the criterion belongs to.
2. Add a new line to the corresponding `check_m<x>()` function with a clear `present` / `pending` / `missing` call.
3. Bump `total` for that milestone if the new criterion is required for DONE.
4. Document the new probe in `docs/MILESTONE_CHECK.md` §4 (this section).
5. Run the script and confirm the new line appears.

Probes must be:
- **Cheap** — total runtime under 30 seconds even on a large repo.
- **Deterministic** — no flakes; no network dependency beyond `gh` (which is already wrapped).
- **Read-only** — never mutate the repo or invoke a shell command that does.

## 8. Why this design and not alternatives

| Alternative | Why not |
|---|---|
| One mega test in `go test` | Conflates milestone-gate state with unit test failures. Hard to read. Not orthogonal to TDD. |
| A web dashboard | Over-engineered for a single human + 3 agents. Bash is enough. |
| Just rely on CI workflows | CI runs on push; we want on-demand and scheduled cadences too. The script is a building block CI can wrap. |
| Rich JSON output for tooling | Premature. Plain text is greppable and readable; add JSON only if a downstream consumer materializes. |
| Track results in a file in the repo | Conflates ephemeral state (passing now) with durable artifacts (status reports). Reports go to GitHub via comments; the repo stores only briefs and reports. |

## 9. Limitations and future work

- The script has no notion of "expected NOT_STARTED" — once M2 is supposed to be in progress, M2 NOT_STARTED is a problem, but the script can't tell. Resolution: an orchestrator-edited "expected status" map alongside the script, updated when a milestone opens.
- Coverage probes use `go test -cover` output parsing. Brittle to format changes; replace with a `go tool cover` invocation if the format drifts.
- Python checks assume a venv is active or `pytest`/`mypy` are on PATH. If they aren't, those criteria are reported as PENDING (not FAIL) — defensive but possibly misleading.
- No integration with `make verify` itself (which is the M0 criterion). The script delegates to that target indirectly via `go build` + `go test` + lint exclude. A future iteration could shell out to `make verify` and parse the result, at the cost of run time.
