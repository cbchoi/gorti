// Package m2spec contains the specification tests for milestone M2:
// federation, declaration, object, event-log, and gRPC behavior.
//
// These tests encode the M2 contract. They:
//
//   - import the concrete packages under rti/internal/{federation,
//     eventlog,declaration,object,transport/grpc}/
//   - drive each component through its public API
//   - assert observable behavior, never internal state
//   - use FakeClock + in-memory EventLog + small inline fakes
//
// New tests may extend this package, but existing assertions must not be
// weakened or deleted.
package m2spec
