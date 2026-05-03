// Package m3spec contains the orchestrator-frozen specification tests for
// milestone M3 — Time Management (NER + LBTS + stall timeout).
// See docs/srs.md §10.2 for the milestone gate.
//
// These tests encode the M3 contract Agent A must satisfy. They:
//
//   - import the concrete `rti/internal/time` package
//   - drive the Manager through its public API
//   - use FakeClock + in-memory Outbox + permissive EventLog from fixtures
//   - assert observable behavior, never internal state
//
// Per docs/TDD.md §5, these tests are committed RED before the milestone
// is dispatched. Agent A turns them green incrementally per the M3 wave
// model (TBD; see CHANGELOG-MASTERPLAN entry once dispatched).
//
// Agents may ADD tests here (file new spec tests of their own) but must
// NEVER weaken or delete existing assertions. Doing so trips a
// verification finding at the M3 gate.
package m3spec
