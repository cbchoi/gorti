# Test-Driven Development Playbook

This document defines how the project applies TDD. It is the methodology binding for all production code in `rti/` (Go) and `pysdk/` (Python). Generated code, vendored data, and infrastructure scripts are exempt — see §6.

Read in addition to `CODING_CONVENTIONS.md`. Where the two overlap, this document governs **methodology**; CODING_CONVENTIONS governs **style**.

---

## 1. Why TDD for this project

- **Determinism is the contract.** SRS NFR-DET-1 requires byte-identical replay. Test-first forces you to specify behavior precisely before implementation, catching ambiguity that "I'll write tests after" hides.
- **Multi-agent setup.** Three agents work in parallel. Tests are how their work composes — Agent A's interface tests are part of Agent C's contract, and vice versa. Tests are a faster, more reliable contract than prose.
- **Spec-dense domain.** IEEE 1516.1-2010 has hundreds of normative behaviors. Each becomes a specification test. The standard becomes executable.
- **Auto-approve sandboxes.** Tests bound the blast radius. An agent that follows TDD cannot drift far without breaking tests; tests act as the safety harness when the orchestrator is not watching every keystroke.

## 2. The cycle: Red → Green → Refactor

Strict cycle, one piece of behavior at a time:

1. **Red.** Write a failing test that specifies one externally observable behavior. Run it. Confirm it fails for the **right reason** (the missing behavior), not for a typo or import error.
2. **Green.** Write the minimum implementation that makes the test pass. Other tests must remain green.
3. **Refactor.** With all tests green, improve structure (extract a helper, rename, dedupe). No behavior changes; the green test suite is your safety net.

Each cycle is small — minutes to ~30 minutes — and ends with a runnable, green codebase. If a cycle is taking hours, the test slice is too big; back up and pick a smaller behavior.

## 3. Commit order rule (enforced)

Within a PR, the git history MUST show **tests committed before the implementation that satisfies them**. Reviewers verify by walking commits.

Permitted patterns:

- **Pattern A — strict**: one commit adds the failing test (must fail at that commit), next commit adds the implementation, next commit refactors. Reviewer can `git checkout <test-commit>` and confirm red.
- **Pattern B — pragmatic**: one commit adds test + minimal impl together, but the PR description explicitly notes test was written first (and the test name reads like a specification, not a coverage check). Use only for tiny additions.
- **Pattern C — bulk feature**: a series of (test, impl, test, impl, …) commits, each one Red-Green for a distinct sub-behavior. Most feature PRs land this way.

Forbidden:

- One mega-commit with all code and all tests (no evidence of test-first).
- Tests added in a follow-up PR after the impl was merged.
- Tests obviously written by reading the implementation (matching internal control flow rather than externally observable behavior).
- Re-ordering history with `git rebase -i` to hide test-after.

The orchestrator inspects git history at review and rejects PRs that violate the order. WORKFLOW.md §3.3 has a checklist confirming you obeyed.

## 4. Test classification

Every test in the repo belongs to exactly one category. The category is visible from location and naming.

| Category | Purpose | Lives in | Naming |
|---|---|---|---|
| **Specification** | Encodes a requirement (FR-*, NFR-*, IR-*) | next to code, or `tests/spec/M<x>/` | Go: `TestSpec_<Behavior>`; Python: `test_spec_<behavior>` |
| **Property** | Holds for any input from a generator | next to code | `TestProperty_<Invariant>` / `test_property_<invariant>` |
| **Regression** | Pins a fixed bug | next to code | `TestRegression_Issue<N>` |
| **Characterization** | Locks current behavior of code you can't safely change yet | `tests/characterization/` | only when wrapping third-party code |
| **Conformance** | Cross-language byte-equality (encoding) | `tests/conformance/` | `TestConformance_<Thing>` |
| **Integration** | Component + neighbor through real interface | `*_integration_test.go` (build tag `integration`) | `TestIntegration_<Scenario>` |
| **Determinism** | 10× repeated, byte-equal output | `*_determinism_test.go` (build tag `determinism`) | `TestDeterminism_<Flow>` |

Coverage targets in `CODING_CONVENTIONS.md` §2.5 / §3.7 are necessary but not sufficient. The real bar:

> **Mutation sanity:** if I delete or alter any single new line of implementation, at least one new test fails with a meaningful message.

A test that doesn't pull its weight by this standard is decoration, not specification. Rewrite it.

## 5. Spec tests as contract (orchestrator-provided)

For each milestone M1..M5, the orchestrator pre-writes **specification tests** that encode the milestone's exit criteria from `srs.md` §10.2. These tests:

- Live under `tests/spec/M<x>/`.
- Are written test-first by the orchestrator and committed red **before** the milestone is announced.
- Treated as effectively read-only by agents. Agents may add to `tests/spec/M<x>/` but may not delete or weaken existing assertions; doing so triggers an automatic `verification:contract` issue.
- Their passing is **necessary** for milestone advancement.

Agent-written tests cover detailed unit behavior in TDD style. Spec tests cover the milestone contract. Both must be green at the gate.

This is the primary mechanism by which the orchestrator constrains auto-approve agents. The agent's ability to drift is bounded by the spec tests it cannot weaken.

## 6. When TDD is exempt

These categories do NOT require test-first; tests can come after, or be omitted:

- **Generated code** — `*_generated.go`, `*_pb2.py`, anything from a code generator. Test the generator, not its output.
- **Vendored data** — embedded MIM XML, third-party schemas under `tests/conformance/foms/vendor/`.
- **CI scripts** — `.github/workflows/*`, top-level `scripts/*`. Tested by being run in CI.
- **Trivial constructors** that just assign fields. Cover indirectly via callers.
- **Logging / metrics emission**. Cover via the integration test of the path that emits, not by asserting log lines.
- **`main` packages** (`rti/cmd/rtid`, example mains). Cover via end-to-end tests.

When in doubt, write the test. Exemptions are exceptions, not defaults.

## 7. Domain-specific patterns

These are the patterns each agent will use most. Per-agent briefs include domain-specific TDD examples; this section is the cross-cutting reference.

### 7.1 FOM parser & validation (Agent B)

Each `FOM-NNN` diagnostic owns one or more **bad-FOM** fixtures plus a **positive** accept fixture. Cycle:

1. Add `tests/conformance/foms/bad/FOM-NNN-<short>.xml` (a FOM that should be rejected).
2. Add a spec test that parses it and asserts `FOM-NNN` is reported.
3. Run — fails (parser doesn't yet detect this).
4. Implement the validation — passes.
5. Refactor.

### 7.2 Encoding rules (Agent B)

Strict TDD with golden vectors:

1. Pick a type. Compute expected bytes from the spec by hand. Add the vector to `tests/conformance/encoding_vectors.json`.
2. `TestConformance_Encoding` is now red for that vector.
3. Implement encode + decode until vector passes.
4. Add a property test: for generated `v`, `decode(encode(v)) == v`.
5. Add explicit padding tests at type boundaries — a vector where alignment differs from naïve concatenation. Round-trip alone misses padding bugs; byte-equality catches them.

### 7.3 Time management (Agent A)

- **LBTS as property test**: generate random regulating sets; assert `lbts == min(currentTime[h] + lookahead[h])` over the regulating set, `+Inf` when empty.
- **NER grant as sequence test**: a list of `(action, expected)` tuples driven through `FakeClock`. Each tuple is one assertion. Failures localize.
- **Stall detection**: inject `FakeClock`; advance past timeout; assert `FederationHalted{cause: stall, federate: H}` event appears.
- **Determinism**: same seed + same scenario, 10 runs, byte-identical event log.

### 7.4 Federation lifecycle (Agent A)

Sequence tests: scripted command sequences with assertions per step.

```go
ops := []op{
    {Create, "demo", goodFOM, expectOK},
    {Join,   "demo", "alice", expectHandle(1)},
    {Join,   "demo", "bob",   expectHandle(2)},
    {Resign, "demo", 1,       expectOK},
    {Destroy,"demo",          expectErr(ErrFederationHasFederatesJoined)},
    {Resign, "demo", 2,       expectOK},
    {Destroy,"demo",          expectOK},
}
runOps(t, fedmgr, ops)
```

Concurrent-join determinism: post 50 join commands through 50 goroutines but with a deterministic input-order channel. Assert handles 1..50 are assigned to a stable name order across 10 runs.

### 7.5 gRPC handlers (Agent A)

Tests use **inline fakes** of `core.FederationStore`, `core.TimeManager`, etc. — small structs implementing the interface, NOT mocking frameworks. Each handler test covers:

1. Happy path produces expected response.
2. Each documented error code (`ERR_FED_*`, etc.) is reachable from a defined input.
3. Idempotency where defined (e.g. resign of already-resigned federate).

Integration tests (`*_integration_test.go`) replace the fakes with real implementations end-to-end.

### 7.6 Event log (Agent A)

- **Round-trip property test**: any sequence of events written then read produces identical events.
- **Crash-mid-write**: truncate the file at a record boundary — reader stops cleanly; truncate mid-record — reader returns `io.ErrUnexpectedEOF` (no panic).
- **Replay determinism**: write log with one RTI, replay through a fresh RTI, assert second log byte-identical.

### 7.7 Python SDK against fake RTI (Agent C)

Build `FakeRtiServer`: small in-process double (pure Python or in-proc gRPC) that records calls and emits canned `FederateEvent`s. SDK tests drive scenarios through it:

- Join → Publish → Register → Update → assert recorded `UpdateRequest` matches attributes/timestamp.
- Subscribe → fake pushes `ReflectAttributeValues` → assert consumer callback fires with expected values.
- Error responses → assert correct typed exception raised.

### 7.8 pyjevsim bridge (Agent C)

Build `StubCoupledModel`: controllable `ta()`, `output_handler()`, recordable `external_transition()`. Drive `HLAFederate` against it + `FakeRtiServer`:

- `ta=2.0`, no incoming events → assert `next_message_request(now + 2.0)` issued; on grant, internal_transition called.
- `ta=5.0`, interaction arrives at `t=1.0` → assert grant arrives at 1.0, external_transition called, next ta evaluated.
- Two simultaneous interactions at `t=3.0`, both inputs model selects → assert delivery order matches `select()`.

### 7.9 pyjevsim API drift smoke (Agent C)

A test that imports specific pyjevsim symbols and exercises a small protocol. Pin `pyjevsim==X.Y.Z` in `pyproject.toml`. If a future bump breaks the smoke, fail loudly with a diagnostic naming the broken symbol.

## 8. Mutation sanity (orchestrator-applied)

The project does not run a mutation framework as part of CI. Instead, at PR review the orchestrator may **manually mutate one or two lines** in your code (flip `<` to `<=`, swap `min` to `max`, drop a `slices.Sort`, comment out a guard) and re-run your tests. If the suite still passes, the PR is rejected — the tests are not specifying the behavior.

Defend against this by writing **observable-behavior** tests, not internal-control-flow tests:

- Bad: `assert(internal_state.lbts == 5.0)` — depends on internal field name.
- Good: `assert next_grant(fed=1, request_time=10.0) == 5.0` — observable behavior.

## 9. Anti-patterns (rejected at review)

| Anti-pattern | Why it's wrong |
|---|---|
| Test-after disguised as test-first by re-ordering commits | Reviewer spots from PR conversation, CI history, naming style |
| Mock-heavy tests mirroring impl structure | Pass even when impl is wrong; refactoring breaks them |
| One giant test exercising ten behaviors | Failure messages don't localize |
| Tests asserting log lines or metric counts | Coupling to unstable surfaces |
| `time.Sleep` to wait for async behavior | Flake; use `FakeClock` or explicit sync |
| Hardcoded absolute paths | Won't run in CI or other agent's sandbox |
| Tests sharing state via package-level vars | Order-dependent; rejected by `t.Parallel` |
| Snapshot tests as primary spec | Easy to "approve" wrong output; only OK for pinning known-good output |
| `TestEverything` / `test_main` mega-tests | One name per behavior, not per file |
| Asserting on randomness without a seed | Flake |
| Coverage chasing — adding tests just to bump % | Doesn't add value; tests must specify behavior |

## 10. PR self-check (TDD checklist)

Before opening a PR, confirm:

- [ ] Each new behavior in this PR has at least one test.
- [ ] Each test was written before its implementation (Pattern A / B / C from §3 — note which in PR description).
- [ ] Test names describe behavior, not implementation: `TestSpec_DuplicateAttributeName_RejectedFOM004`, not `TestParserCheck`.
- [ ] No test sleeps or depends on wall-clock.
- [ ] Removing any single new implementation line causes at least one new test to fail.
- [ ] Spec tests in `tests/spec/M<x>/` (if applicable) are not weakened.
- [ ] `make test` and `make determinism` pass locally.

If any check fails: fix before opening. CI will catch most; the orchestrator catches the rest at review.

## 11. References

- `docs/srs.md` §10 — verification approach (TDD is the methodology layered on top of the levels there).
- `docs/CODING_CONVENTIONS.md` §2.5 / §3.7 — coverage targets and test naming.
- `docs/WORKFLOW.md` §3.3 — PR template includes the TDD checklist.
- Each per-agent brief — domain-specific TDD examples.
