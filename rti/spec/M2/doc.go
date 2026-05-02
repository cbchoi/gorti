// Package m2spec contains the orchestrator-frozen specification tests for
// milestone M2 — Federation + Declaration + Object + EventLog + gRPC
// handlers. See docs/srs.md §10.2 for the milestone gate.
//
// These tests encode the M2 contract Agent A must satisfy. They:
//
//   - import the concrete packages under rti/internal/{federation,
//     eventlog,declaration,object,transport/grpc}/
//   - drive each component through its public API
//   - assert observable behavior, never internal state
//   - use FakeClock + in-memory EventLog + small inline fakes
//
// Per docs/TDD.md §5, these tests are committed RED before the milestone
// is dispatched. Agent A turns them green incrementally per the
// docs/M2_DISPATCH_PLAN.md wave model.
//
// Agents may ADD tests here (file new spec tests of their own) but must
// NEVER weaken or delete existing assertions. Doing so trips a verification
// finding at the M2 gate.
package m2spec
