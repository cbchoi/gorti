// Package m5spec contains the orchestrator-frozen specification tests for
// milestone M5 — Hardening + modes + perf + cross-language end-to-end.
// See docs/srs.md §10.2 for the milestone gate.
//
// These tests encode the M5 contract Agents A, B, C must satisfy. They:
//
//   - import the concrete rti/internal/* packages (mode wiring, perf harness)
//   - drive their public APIs via the gRPC handler / direct calls
//   - use FakeClock + in-memory Outbox + permissive EventLog where applicable
//   - assert observable behavior, never internal state
//
// Per docs/TDD.md §5, these tests are committed RED before the milestone
// is dispatched. Agents turn them green incrementally per the M5 wave
// model (see docs/M5_DISPATCH_PLAN.md):
//
//   - W1A (Agent A): mode_flag_test.go, best_effort_test.go
//   - W1B (Agent B): TASK-083 audit (no source changes; spec tests not affected)
//   - W1C (Agent C): cross_lang_test.go (Python-side; this package's
//     cross_lang_test.go is the orchestration scaffold)
//   - W2A (Agent A): perf_test.go, soak_test.go
//   - W2B (Agent C): test_spec_m5_modes.py under pysdk/tests/spec/m5/
//
// Agents may ADD tests here (file new spec tests of their own) but must
// NEVER weaken or delete existing assertions. Doing so trips a
// verification finding at the M5 gate.
package m5spec
