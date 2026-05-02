// Package federation implements the federation lifecycle: Create, Join,
// Resign, Destroy, List. It is one of the four core service implementations
// composed by rti/cmd/rtid (the others being declaration, object, and
// time).
//
// Owner: Agent A. Stubs in this package are part of the M2 contract; the
// public API surface (Manager, Options, New, plus the methods of
// core.FederationStore) is FROZEN-SHAPE — bodies are stubbed and must be
// filled in test-first per docs/TDD.md, but signatures should not change
// without a contract-change-request.
//
// # Design for testability
//
// Manager takes its dependencies through an Options struct, never via
// package-level globals. This lets unit tests substitute a FakeClock,
// in-memory EventLog, and stub FOMRepository:
//
//	mgr := federation.New(federation.Options{
//	    Clock:    core.NewFakeClock(time.Unix(0, 0)),
//	    EventLog: testlog.NewInMemory(),
//	    FOMs:     testfoms.AlwaysOK(),
//	})
//
// Determinism is enforced by:
//   - federate handles assigned by sort order of FederateName (not arrival)
//   - all map iteration sorted by key before any observable side effect
//   - no time.Now() calls (forbidigo enforces); time flows through Clock
//
// # Concurrency
//
// Manager serializes per-federation state mutations behind a single mutex.
// This is intentional for cut 1 — performance optimization (sharded locks,
// goroutine-per-federation) is a M5 conditional task gated by perf
// baselines. Read paths (List) take a read-lock for fan-out under load.
package federation
