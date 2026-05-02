# Master Plan Changelog

Append-only record of master-plan revisions. The orchestrator updates this after reading milestone status reports from all agents (`docs/reports/M<x>/agent-{a,b,c}.md`).

Entries are most-recent first. Each entry: date, summary of decision, link to the status reports that drove it.

---

## 2026-05-02 (M2 pre-work — orchestrator-frozen stubs + spec tests + wave-based dispatch plan)

Closed M1, opened M2. Pre-work delivered so Agent A can start working through the M2 wave model.

### What landed

- **Frozen-shape stubs** in five new Agent A packages, each with package doc + `Manager`/`Writer`/`Registry`/`Server` type + `Options` struct + constructor + interface-method stubs returning `ErrNotImplemented`. Compile-time `var _ core.Foo = (*X)(nil)` assertions guard against signature drift:
  - `rti/internal/federation/{doc,manager}.go` — implements `core.FederationStore`
  - `rti/internal/eventlog/{doc,format,writer,reader,replayer}.go` — implements `core.EventLog` + `core.EventLogReader`
  - `rti/internal/declaration/{doc,manager}.go` — pure data, no core interface
  - `rti/internal/object/{doc,registry}.go` — implements `core.ObjectRegistry`
  - `rti/internal/transport/grpc/{doc,server}.go` — composes the four core services
- **Orchestrator-frozen spec tests** at `rti/spec/M2/*.go` (NOT `tests/spec/M2/`). 7 files: `doc.go`, `fixtures.go`, `federation_test.go`, `eventlog_test.go`, `declaration_test.go`, `object_test.go`, `replay_test.go`, `grpc_test.go`. Spec tests RED-by-design — every test's first call into a stub fails with the package's `ErrNotImplemented` sentinel. Agent A turns them green per task.
- **`docs/M2_DISPATCH_PLAN.md`** documents the wave model (4 waves, up to 3 sub-agents per wave, total 8 sub-agents). Critical path is Wave 1 → Wave 2 → Wave 3 → Wave 4. Includes per-wave file-ownership table, dependency graph, sentinel-bundling pattern, and dispatch checklist.
- **All 21 M2 task briefs** (`docs/tasks/TASK-020.md` through `TASK-040.md`) gain a Notes-section reference to `docs/M2_DISPATCH_PLAN.md`.
- **Path convention amended**: M2+ spec tests live at `rti/spec/M<x>/`, not `tests/spec/M<x>/`. Reason: Go's `internal` package rule blocks `tests/...` from importing `rti/internal/*`. M1 stays at `tests/spec/M1/` because it imports only public packages (`rti/pkg/fom`, `rti/pkg/encoding`); future milestones whose work is in `rti/internal/` follow the M2 convention.
- **`scripts/check-milestones.sh`** updated: M2 + M3 spec-test directory probes now look at `rti/spec/M<x>/` instead of `tests/spec/M<x>/`. M1 probe unchanged.

### Design-for-testability decisions baked into the stubs

- **Options pattern**: every constructor takes a value-type `Options` struct. Tests substitute `FakeClock`, in-memory `EventLog`, fake `Outbox`, fake `FOMRepository` without touching production wiring.
- **Inline-fake test pattern** (per `docs/TDD.md` §7.5): the spec test fixtures (`rti/spec/M2/fixtures.go` + `grpc_test.go`'s `stubFedStore`/`stubObjectRegistry`) use small struct fakes, not mocking frameworks. Each fake records calls; tests inspect via simple slice comparisons.
- **Compile-time interface assertions**: `var _ core.FederationStore = (*Manager)(nil)` lines at the bottom of each stub file. Removing a required method fails the build at that line, not deep inside Agent A's implementation.
- **Stubs populate their fields**: `New` returns `&Manager{opts: opts}, ErrNotImplemented` rather than `nil, ErrNotImplemented`. Spec tests proceed past construction and fail loudly on the FIRST genuine method call, giving clearer signal about which method needs implementation.

### Wave model (full doc in `docs/M2_DISPATCH_PLAN.md`)

```
Wave 1 (3 parallel) — federation + eventlog + declaration
   ↓
Wave 2 (1–2 parallel) — object registry, then eventlog replayer
   ↓
Wave 3 (3 parallel) — gRPC FederationService / DeclarationService / ObjectService+StreamService
   ↓
Wave 4 (1 sub-agent) — cmd/rtid wiring + go-pingpong example + harnesses (M2 gate)
```

Total: 4 waves, 8 sub-agents. Same proven structure as M1 (which closed in 3 waves + ~9 sub-agents in one session).

### State after this commit

- `M0: DONE`, `M1: DONE`, `M2: IN_PROGRESS (1/4)` — only spec-test directory probe is now green; the other three M2 probes (`go-pingpong/main.go`, determinism harness, replay harness) remain pending Agent A's Wave 4 work.
- No regressions on M1.
- `make verify` (build + lint + tests) passes for everything except the deliberately-RED M2 spec tests.

### Next concrete actions (orchestrator)

1. Dispatch Wave 1: spawn three sub-agents (W1A federation, W1B eventlog, W1C declaration) in one parallel `Agent` call.
2. Review + merge each branch on completion.
3. Re-run milestone-check, expect M2 IN_PROGRESS (still — Wave 4 hasn't run yet).
4. Continue through Waves 2, 3, 4.

---

## 2026-05-02 (M1 follow-ups — canonical MIM landed, issue #1 resolved, octet-pair vectors added, JSON coercion fixes)

Two follow-ups carried over from the M1 closure round, both resolved this same day.

### Canonical MIM (issue #1 → resolved)

Replaced the interim hand-derived MIM with the canonical IEEE 1516.1-2010 standard MIM, sourced from openlvc/portico (CDDL-1.0). The file itself carries an explicit IEEE royalty-free attribution license at its head; that header comment is preserved verbatim. Provenance, blob sha (`713d000…`), content sha256 (`649f008a…`), and retrieval date recorded in `rti/pkg/fom/mim/embed.go`'s package doc.

Two follow-on fixes were needed to integrate the canonical content:

- **`<note>` added to the DIF Annex A whitelist** in `rti/pkg/fom/parser/strict.go`. The canonical MIM uses the singular annotation element which our interim file hadn't needed; all other 64 distinct elements were already covered.
- **`isMIMTypeModule` heuristic widened** in `rti/pkg/fom/parser/mim_merge.go`. The canonical MIM declares `<type>FOM</type>` (not `<type>MIM</type>`) for historical-compat reasons; without widening, `parser.Parse` on the embedded MIM self-collides via FOM-101 on every shared name. The new heuristic also matches modelIdentification names containing "Standard MOM and Initialization Module" or "HLAstandardMIM" (case-insensitive).

`hla-standard-mim.xml` re-classified from "interim approximation" to "empty wrapper" since the canonical MIM is fully self-sufficient for cut-1.

Issue #1 is closed by the orchestrator's commit `0e37c62`. The PAT in this conversation could not close the GitHub issue programmatically — user closes manually.

### HLAoctetPair vectors + JSON coercion

Two coupled changes closed the second M1 follow-up:

- **`tests/conformance/encoding_vectors.json`** gained 6 new vectors covering `HLAoctetPairBE` (zero, mixed `[0xAB, 0xCD]`, max) and `HLAoctetPairLE` (same logical values + asymmetric to exercise byte-swap).
- **`rti/pkg/encoding/byte.go`**: `octetPairBytes` accepts `[]any` (the JSON-array form) in addition to `[2]byte` and `[]byte`. New `coerceOctet` helper narrows each element from float64/int/byte with range checking.
- **`tests/spec/M1/encoding_vectors_test.go`**: `valuesEqual` gains a `[]any` case comparing against `[2]byte` (and `[]byte` for symmetry); both element types reuse the existing float64-coercion path. Frozen-file edit by orchestrator: purely additive, no existing case altered.

Earlier the same day, two additional JSON-coercion fixes had landed for the composite spec test (variant-record discriminator round-trip canonicalization in `variant_record.go`; opaque-data hex-string acceptance in `opaque.go`). All composite vector subtests now pass byte-identical.

### Net M1 state at close

```
M1: DONE
  ✓ 10 bad-FOM fixtures committed
  ✓ TestSpec_M1_BadFOMDiagnostics (all 10 codes including FOM-101)
  ✓ TestSpec_M1_PrimitiveVectorsRoundTrip (53 + 6 octet-pair = 59 subtests)
  ✓ TestSpec_M1_CompositeVectorsRoundTrip (17 subtests)
  ✓ rti/pkg/encoding coverage = 95.9%
```

No regressions. Issue #1 closed. Both M1 follow-ups absorbed. Ready to dispatch M2 once the orchestrator pre-writes `tests/spec/M2/`.

---

## 2026-05-02 (Wave 1 + Wave 2 + Wave 3 dispatch) — M1 driven from 0 to 9/10 BadFOM + full primitive + composite codecs; issue #1 interim resolution

Spawned three waves of orchestrator-driven sub-agents (worktree-isolated `general-purpose` agents role-playing Agent B) to drive M1 toward DONE.

**Wave 1 (4 parallel sub-agents)**: TASK-001 (parser+model skeleton), TASK-010 (integer codecs), TASK-011 (float codecs), TASK-012 (octet/boolean/char codecs). All four merged on `main` at `f2d8ae0`. Outcomes:
- Parser+model package green; spec test `TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics` passes; coverage parser=69.6% / model=73.7%.
- 16 primitive codecs (6 integer + 4 float + 6 byte/bool/char families) byte-identical to golden vectors; coverage on each ≥90%.
- 38 new vectors in `tests/conformance/encoding_vectors.json` (additive-only).
- `PrimitiveByName` refactored from a giant switch to a `primitiveCodecs` map dispatch (gocyclo limit was being exceeded).
- HLAoctetPair vectors NOT added — the orchestrator-frozen spec test's `valuesEqual` helper doesn't handle `[2]byte` vs `[]any{f64,f64}`. Sub-agent flagged for future orchestrator-side helper extension.

**Wave 2 (3 parallel sub-agents)**: parser diagnostics bundle (TASK-003..007 + 086..089 — 9 codes in one branch via the new `diagnoser` registry pattern), strings + arrays + opaque (TASK-013/014/015/018), records (TASK-016/017 with `_disabled` flag dropped from `fixed-record-octet-float64` and the embedded literal-space typo in its `bytes` field corrected by orchestrator). All three merged at `08bf89a`. Outcomes:
- Spec test `TestSpec_M1_BadFOMDiagnostics`: 9/10 subtests green (all except FOM-101 which depends on TASK-009).
- All composite codecs implemented as constructor functions (`NewFixedArray`, `NewVariableArray`, `NewFixedRecord`, `NewVariantRecord`, `NewOpaqueData`).
- 24 more vectors added (string + composite). Total now 88.
- Coverage on `rti/pkg/encoding` package: 96.0%; on `rti/pkg/fom/parser`: 83.3%.
- `diagnoser` registry pattern: each FOM-NNN detector lives in its own file, registers via `init()`, runs from `Parse` after the structural walk. Trades a tiny abstraction for ~9 future-merge-conflict-free additions.

**Issue #1 — interim resolution (orchestrator)**: hand-derived faithful MIM committed at `rti/pkg/fom/mim/standard-mim.xml` and `hla-standard-mim.xml` with strong "INTERIM" provenance comments pointing at issue #1 for canonical sourcing post-M1. `docs/ORTHOGONALITY.md` §2 amended to mark these two specific XML files as orchestrator-vendored; Agent B reads them via `//go:embed` but does not edit. TASK-008 and TASK-009 unblocked (`Status: BLOCKED` → `Status: DISPATCHED`); their Notes record the interim resolution.

**Wave 3 (planned, dispatching next)**: TASK-008+009 bundle (MIM embed + Merge + FOM-101 detection — closes the last red M1 spec subtest) and TASK-019 (CodecFor wiring + composite vector test goes from `t.Skip` to green). Two parallel sub-agents.

After Wave 3 lands, the orchestrator's `scripts/check-milestones.sh` will report **M1: DONE (4/4)** assuming no regressions.

**Process notes**:
- Sub-agents pushed to `origin` directly to enable orchestrator review-and-merge from the main worktree. No agent had write access to `main`.
- Three task-bundle commits ship with their bundle's TASK-NNN sentinels touched together (per the bundled-dispatch decision documented in each commit body); strict one-PR-per-task is relaxed for sub-agent dispatch efficiency, with documentation in the sentinel commit.
- W2A introduced an architectural-pattern improvement (the `diagnoser` registry) that the orchestrator should formalize in `docs/sdd.md` as the standard pattern for "many-validator" components. Tracked as future-doc-update work; not a blocker.
- Pre-existing `fixed-record-octet-float64` vector had a literal space in its `bytes` field. Orchestrator removed the space at merge time as a "fix-broken-placeholder" (the entry was `_disabled` until W2C enabled it; no test had ever exercised the broken bytes), with the rationale that this is not "modifying a working vector" forbidden by additive-only policy but rather "fixing a placeholder typo before activation."

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
