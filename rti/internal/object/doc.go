// Package object implements the object/interaction registry: handle
// assignment, publish/discover routing, attribute updates, interactions.
//
// Owner: Agent A. Stubs in this package are part of the M2 contract.
//
// # Position in the dependency graph
//
//	federation.Manager  --(reads)-->  declaration.Manager
//	         |                                |
//	         v                                v
//	         +------> object.Registry <-------+
//	                          |
//	                          v
//	                  eventlog (write-ahead)
//	                          |
//	                          v
//	                       Outbox (fanout)
//
// Registry depends on Federation (federate roster), Declaration
// (subscriber lookup), EventLog (write-ahead persistence), Outbox
// (deliver Discover/Reflect/Receive to subscribers), Codec
// (attribute serialization).
//
// # Determinism
//
// Three invariants drive replay determinism:
//
//  1. Object handle assignment is monotonic per federation, derived from
//     a single counter incremented at Register time. Replay re-reads the
//     counter from the event log so handles match.
//  2. Discover/Reflect/Receive fanout iterates subscribers in
//     declaration.Manager's deterministic order.
//  3. EventLog.Append for the operation MUST complete before any
//     observable state mutation or fanout — this is the write-ahead
//     contract that makes replay possible.
//
// # Test seams
//
// Registry takes its dependencies through Options. Tests pass:
//   - in-memory FederationStore (or a stub returning canned federates)
//   - in-memory EventLog (bytes.Buffer-backed Writer)
//   - declaration.Manager (real, no fakes needed — pure)
//   - fakeOutbox that records every Send for assertion
//
// See tests/spec/M2/object_test.go for the contract tests.
package object
