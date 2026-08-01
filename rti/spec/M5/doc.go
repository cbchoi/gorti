// Package m5spec contains the specification tests for milestone M5:
// hardening, modes, performance, and cross-language end-to-end behavior.
//
// These tests encode the M5 contract. They:
//
//   - import the concrete rti/internal/* packages (mode wiring, perf harness)
//   - drive their public APIs via the gRPC handler / direct calls
//   - use FakeClock + in-memory Outbox + permissive EventLog where applicable
//   - assert observable behavior, never internal state
//
// New tests may extend this package, but existing assertions must not be
// weakened or deleted.
package m5spec
