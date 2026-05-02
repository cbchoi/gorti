// Package declaration manages the per-federation publish/subscribe
// matrices: which federate publishes/subscribes to which (object class,
// attribute) and which (interaction class). The matching engine that
// fans out updates to subscribers reads from this package.
//
// Owner: Agent A. Stubs in this package are part of the M2 contract.
//
// # Design
//
// The Manager exposes a small set of methods that mirror the gRPC
// DeclarationService RPCs. State is stored as sorted slices keyed by
// (object class handle, attribute handle) and (interaction class handle).
// Iteration is by sort order — never Go's map iteration order — so
// observable fanout sequences are deterministic across runs (NFR-DET-1).
//
// # Test seams
//
// Manager has no external dependencies beyond the core types. Tests
// construct a Manager, call publish/subscribe methods, and assert that
// SubscribersFor / PublishersFor return slices in deterministic order.
// No fakes are needed; the package is pure.
//
// # Concurrency
//
// Manager serializes all writes behind a mutex. Reads (SubscribersFor,
// PublishersFor) take a read-lock. The downstream object registry calls
// these on every Update/Send, so read paths are perf-critical; the M5
// perf task will benchmark and possibly switch to a copy-on-write
// snapshot strategy.
package declaration
