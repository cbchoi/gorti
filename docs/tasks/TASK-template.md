# TASK-NNN: <short imperative title>

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-a / agent-b / agent-c |
| Milestone | M0 / M1 / M2 / M3 / M4 / M5 |
| Created | YYYY-MM-DD |
| Updated | YYYY-MM-DD |
| Depends-on | TASK-NNN, ... / none |
| Blocks | TASK-NNN, ... / none |

## Goal

<One to three sentences. Describe the externally observable behavior that
must exist when this task is DONE. Avoid implementation detail; describe
what works, not how.>

## Scope (in)

<Bullet list of concrete deliverables. Each bullet should map to one or more
of: a function/method that exists, a test that passes, a file that exists.>

- ...

## Scope (out)

<Bullet list of things explicitly NOT in this task. Use this section to
prevent scope creep — if the agent is tempted to "while I'm here" something,
they should see it listed here as out-of-scope.>

- ...

## Implements

<Comma-separated SRS requirement IDs (FR-*, NFR-*, IR-*, C-*) advanced by
this task. Also list specification tests under tests/spec/M<x>/ that this
task is expected to turn green.>

- Requirements: FR-FOM-1, FR-FOM-3
- Spec tests: tests/spec/M1/parser_diagnostics_test.go::TestSpec_M1_ParseMinimalGoodFOM_NoDiagnostics

## TDD entry point

<Name the first red test the agent should target. The agent will then write
their own unit tests test-first per docs/TDD.md to fill in the path from
that first red test to green.>

- Start with: <test name + brief description of what it asserts>

## Acceptance criteria

- [ ] <Objective, testable. e.g. "tests/spec/M1/parser_diagnostics_test.go passes for case 'minimal'.">
- [ ] <e.g. "go test ./rti/pkg/fom/parser/... is green.">
- [ ] <e.g. "Coverage on rti/pkg/fom/parser ≥ 75%.">
- [ ] <e.g. "PR follows TDD commit pattern A or C per docs/TDD.md §3.">
- [ ] <e.g. "make verify is green locally before opening the PR.">

## Notes / hints

<Optional. Pointers to relevant spec sections, known gotchas, links to
prior work or design discussion. Keep brief.>

- IEEE 1516.2-2010 §<X> for the relevant rule.
- See docs/sdd.md §<Y> for the design.
- Related: TASK-MMM (sibling work that ships in parallel).
