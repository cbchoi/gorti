// Package m12spec contains the specification tests
// for milestone M12 — gRPC handler + Python SDK exposure for cut-2
// service groups (sync, ownership, MOM, DDM, savepoint).
//
// See docs/srs.md §10.4 (cut 3) for the milestone gate.
//
// Cut-2 implemented these service groups as internal Go packages
// (rti/internal/{sync,ownership,mom,ddm,savepoint}). Federates can't
// reach them over the network — that's M12's job: extend the proto
// definitions, wire gRPC handlers in
// rti/internal/transport/grpc/, expose them in the Python SDK.
//
// Spec test scope (Go side):
//   - Each new service has at least one round-trip test that constructs
//     a real grpc.Server with the handler wired, dials a client, invokes
//     the RPC, asserts the underlying Manager state changed.
//
// The Python SDK side is covered under pysdk/tests/spec/m12/.
package m12spec
