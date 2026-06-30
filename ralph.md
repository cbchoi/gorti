# ralph.md — M31 implementation orchestration

**Milestone:** M31 — DLC C++ surface lockfile (RED tests scaffold).

**Source of truth:** `docs/M31_DISPATCH_PLAN.md` (full 532-line dispatch plan), with parent program at `docs/DLC_COMPLIANCE_PROGRAM.md` and divergence catalogue at `docs/DLC_DIVERGENCE_CATALOGUE.md` (153 rows driving the lockfile assertions).

This document is the **5-agent fan-out** for executing M31. Each agent works in
its own git worktree, owns a disjoint slice of M31's tasks (TASK-335..364),
runs unit tests after each task, and commits its work into its worktree branch.
The orchestrator merges back at the end.

---

## 0. M31 in one paragraph

Land **failing tests** that lock the IEEE 1516.1-2010 DLC C++ API surface at
the spec. No implementation code; every lockfile/conformance test is RED by
design. ~200 per-TU `static_assert` lockfile tests with `WILL_FAIL=TRUE`
CTest properties + 27 conformance fixtures + parity harness + 30 header stubs
+ docs (SRS §5.14 + §7.4 + IDD §1.8 skeleton + agent-d brief + spec-coverage
+ traceability lint + CHANGELOG). Honest effort estimate: 5-7 weeks
single-owner; 5-agent fan-out targets ~1-2 weeks calendar.

---

## 1. Agent assignments

| Agent | Worktree branch | Tasks | Scope | Lines (est) |
|---|---|---|---|---|
| **A** | `m31-a-lockfile-core` | TASK-335 | `cppsdk/tests/dlc/lockfile/core/` — 14 lockfile TUs against RTIambassador + service-group locks (FM, DM, OM, OWN, TM, DDM, handle services, MOM) + RTIambassadorFactory + FederateAmbassador + NullFederateAmbassador + CallbackModel enum | ~1200 |
| **B** | `m31-b-lockfile-rest` | TASK-336..340 | `cppsdk/tests/dlc/lockfile/{types,exceptions,encoding,time}/` — ~26 lockfile TUs covering Handle/Typedefs/VLD/RangeBounds/Enums, ~120 exception classes, DataElement + 14 encoders, LogicalTime + factories + 6 concrete time types. Plus `lockfile/CMakeLists.txt` with `WILL_FAIL=TRUE` wiring. | ~1500 |
| **C** | `m31-c-fixtures-4-7` | TASK-341..344, 348 | `cppsdk/tests/dlc/conformance/` — 18 fixtures: §4 federation mgmt (5) + §5 declaration (2) + §6 object mgmt (7) + §7 ownership (4). Plus `_harness/` shared utilities (RtidRunner, normalize, golden_loader). | ~3500 |
| **D** | `m31-d-fixtures-8-11` | TASK-344b, 345..347, 349 | `cppsdk/tests/dlc/conformance/` — 9 fixtures: §8 time mgmt (4) + §9 DDM (2) + §11 MOM (1) + threading re-entrancy (1) + cross-lang Python↔C++ (1). Plus conformance CMakeLists.txt wiring (`WILL_FAIL` per fixture). | ~2000 |
| **E** | `m31-e-infra-docs` | TASK-350..364 | `cppsdk/include/RTI/` — 30 forward-declaration header stubs (§2.5 of dispatch plan). `tests/parity/` is collapsed into per-fixture `parity/` subdirs (§5.2 of program doc) but the parity-mode build scripts + `normalize.py` land here. Docs: SRS §5.14 + §7.4 patches, IDD §1.8 skeleton, `docs/agent-d-cppsdk.md` content draft, `docs/PITCH_PARITY.md` update, `docs/MIGRATION_M17_TO_DLC.md` skeleton, `docs/PITCH_GOLDEN_LICENSING.md` (TASK-363 EULA review). Scripts: `scripts/check-milestones.sh check_m31`, `scripts/gen-spec-coverage.sh`, `scripts/check-spec-traceability.sh`. `CHANGELOG-MASTERPLAN.md` M31 row. | ~1800 |

**Total:** ~10,000 LOC across ~300 files. Parallelization target: 5 agents × 1 week ≈ what would be 5 weeks serial.

---

## 2. Coordination rules (avoid step-on-toes)

1. **Disjoint file ownership.** Each agent owns the directories above; no agent writes a file in another's directory. Agents A-D depend on Agent E's header stubs to make their tests parse; if stubs are absent, tests fail with `'X' is not a member of 'rti1516e'` (one error per TU, before `static_assert` fires) — that's still RED, just not the desired *per-assertion* RED signal. Agent E should land stubs as early as possible.

2. **Stub coordination.** Agent E's 30 RTI/ stubs are forward-declarations (not empty namespaces) per `docs/M31_DISPATCH_PLAN.md §2.5`. The stub class declarations must include enough surface for the `static_assert` checks to instantiate — e.g. `class RTIambassador { /* methods declared but no body */ };` so `decltype(&RTIambassador::connect)` resolves. Agent A negotiates with Agent E on what each stub needs to expose; resolved by Agent E adding skeletons as Agent A's TUs require.

3. **No impl code lands.** This is M31 RED. No `.cpp` file under `cppsdk/src/dlc/`. If an agent finds itself writing impl, stop and check the dispatch plan — the impl belongs to M32+.

4. **Per-task commits.** Each agent commits each TASK-N as a separate commit on its branch so the orchestrator can pick + merge cleanly.

5. **Pitch goldens are blocked.** Agents C and D write golden files as `expected.*.log` skeletons with `// TBD-pitch-capture` markers per task. Actual golden capture is gated on Agent E's TASK-363 (Pitch EULA review). Mark the fixture's `WILL_FAIL TRUE` property accordingly; M31 acceptance is achieved without real goldens.

6. **No conflict on `docs/PITCH_PARITY.md` and `CHANGELOG-MASTERPLAN.md`.** Only Agent E touches these.

7. **Existing tests stay GREEN.** M17 cppsdk tests under `cppsdk/tests/test_*.cpp` are NOT modified. M31 only adds; M32+ deprecates the M17 shim.

---

## 3. Per-task unit test discipline

Every agent runs the relevant verifiable check after each TASK commit. Unit tests
for M31 are unusual — they verify **expected failures**, not passing behavior.

### Agents A, B (lockfile)

After each lockfile TU lands, the agent must:
```bash
cd cppsdk
cmake --build build --target dlc_lockfile 2>&1 | tee /tmp/lockfile.log
# Count failing TUs (not error lines — see §5.1 dispatch plan)
ctest --test-dir build -L lockfile --output-on-failure | tee /tmp/lockfile-tests.log
# Expected: M of N tests passed where M=0 (all tests failing is success).
# Verify the just-added TU shows up in the FAILED list.
grep "FAILED.*test_<just-added>" /tmp/lockfile-tests.log || echo "BUG: my new TU didn't even fail"
```

### Agent C, D (fixtures)

After each fixture lands, the agent must:
```bash
cd cppsdk
cmake --build build --target dlc_conformance 2>&1 | tee /tmp/conf.log
# Expected: link failure with `undefined reference to rti1516e::*` (no impl symbols).
grep -c "undefined reference to.*rti1516e" /tmp/conf.log
# Also verify the fixture's federate.cpp parses against RTI/*.h forward-decl stubs:
g++ -c -std=c++17 -I cppsdk/include cppsdk/tests/dlc/conformance/<fixture>/federate.cpp -o /tmp/<fixture>.o 2>&1
# Parse OK → fail at link is the expected failure mode.
```

### Agent E (infra + docs)

After each task:
```bash
# Lint markdown (if mdlint available)
which markdownlint && markdownlint docs/*.md
# Verify scripts are executable + parse
bash -n scripts/gen-spec-coverage.sh
bash -n scripts/check-spec-traceability.sh
bash -n scripts/check-milestones.sh
# Verify check_m31 probe runs (will report N/M with current state)
bash scripts/check-milestones.sh check_m31 || true  # exit non-zero is fine pre-merge
# Verify header stubs parse standalone
for h in cppsdk/include/RTI/*.h cppsdk/include/RTI/encoding/*.h cppsdk/include/RTI/time/*.h; do
  g++ -c -std=c++17 -I cppsdk/include -x c++ "$h" -o /dev/null 2>&1 || echo "STUB BROKEN: $h"
done
```

### Final per-agent check before reporting "task done":

- All commits are on the agent's branch.
- `git status` is clean (no uncommitted changes).
- `git log --oneline` shows one commit per TASK-N.
- Run agent-specific unit-test command above; capture output as a comment in the
  agent's final commit message.

---

## 4. Definition of done (M31 acceptance — orchestrator merges when all 5 agents report this)

Per `docs/M31_DISPATCH_PLAN.md §3` (14 criteria). The 5-agent fan-out contributes to:

1. ✅ All ~200 lockfile assertions RED (Agents A + B)
2. ✅ All 27 conformance fixtures RED (link failure) (Agents C + D)
3. ✅ Parity harness skips cleanly (Agent E)
4. ✅ 30 RTI/ header stubs land (Agent E)
5. ✅ `docs/agent-d-cppsdk.md` lands with content (Agent E)
6. ✅ SRS §5.14 + §7.4 patches land (Agent E)
7. ✅ IDD §1.8 skeleton lands (Agent E)
8. ✅ PITCH_PARITY.md "Pitch deviations from spec" section + "C++ DLC track" section (Agent E)
9. ✅ `docs/dlc-spec-coverage.md` auto-generated; ≥40 §-sections covered (Agent E)
10. ✅ `docs/RTI_CONFORMANCE_AUDIT.md` already exists (DONE in committed plan, just cross-ref from SRS)
11. ✅ CHANGELOG-MASTERPLAN.md M31 row with `(0/200) GREEN` (Agent E)
12. ✅ `check_m31` probe reports `M31: DONE (11/11)` (Agent E)
13. ✅ No M17 cppsdk regression (every agent verifies before pushing)
14. ✅ No pysdk regression (E verifies; no other agent touches pysdk)

---

## 5. Out of scope for M31 (defer to M32+)

- Implementation code (.cpp under `cppsdk/src/dlc/`)
- Real Pitch-captured goldens for fixture `expected.*.log` files (deferred — see §2 rule 5)
- IVCT integration (M35 per dispatch plan)
- `docs/MIGRATION_M17_TO_DLC.md` full content (M35; M31 just lands a skeleton via Agent E)
- pysdk changes
- Go SDK changes

---

## 6. Reset / failure recovery

If any agent reports a task it couldn't complete:
- Note the partial state in this file (mark task `[BLOCKED: reason]`)
- Other agents continue
- Orchestrator handles the blocked task manually or re-dispatches

If an agent's worktree is broken:
- Use `git worktree remove .claude/worktrees/<agent>` to nuke it
- Re-dispatch the agent with `isolation: "worktree"` for a fresh worktree

---

## 7. Status (updated as agents report)

| Agent | Status | Worktree | Commits | Notes |
|---|---|---|---|---|
| A | dispatched | _(filled by Agent tool)_ | _(filled on report)_ | — |
| B | dispatched | _(filled by Agent tool)_ | _(filled on report)_ | — |
| C | dispatched | _(filled by Agent tool)_ | _(filled on report)_ | — |
| D | dispatched | _(filled by Agent tool)_ | _(filled on report)_ | — |
| E | dispatched | _(filled by Agent tool)_ | _(filled on report)_ | — |

(Orchestrator updates this table as agents complete.)
