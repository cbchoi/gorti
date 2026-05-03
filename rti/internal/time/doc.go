// Package time implements HLA time management — regulation, constrained
// federate tracking, LBTS computation, NER (Next Message Request) request
// handling, lookahead enforcement, and stall-timeout detection.
//
// Owner: Agent A. Stubs in this package are part of the M3 contract;
// the public API surface (Manager, Options, New, plus the methods of
// core.TimeManager) is FROZEN-SHAPE — bodies are stubbed and must be
// filled in test-first per docs/TDD.md, but signatures should not change
// without a contract-change-request.
//
// # Cut-1 scope
//
// Per docs/srs.md §10.2 M3 and docs/agent-a-rti-core.md §4 M3:
//
//   - NER only (TAR is cut-2)
//   - Regulating + constrained federates per federation
//   - LBTS = min(currentTime + lookahead) over regulating federates
//   - Stall timeout: configurable per-federation, default 60s, halt-with-
//     diagnostic on fire
//   - Wall-clock-free: all time operations flow through core.Clock per D-1
//
// # Design for testability
//
// Manager takes its dependencies through Options. Tests substitute
// FakeClock + in-memory Outbox + permissiveEventLog (the same fakes
// used by federation/object spec tests):
//
//	mgr := time.New(time.Options{
//	    Clock:        core.NewFakeClock(time.Unix(0, 0)),
//	    Outbox:       newFakeOutbox(),
//	    EventLog:     newPermissiveEventLog(),
//	    StallTimeout: 60 * stdtime.Second,
//	})
//
// LBTS is exported as a pure function (no Manager state) so it can be
// property-tested directly — see lbts.go and rti/spec/M3/lbts_test.go.
//
// # Stall detection (cut-1 design)
//
// Stall detection is poll-based, not goroutine-driven, so tests can
// drive it deterministically with a FakeClock. The Manager exposes a
// CheckStalls(ctx) method that:
//
//   - reads the wall time from opts.Clock
//   - for each federation, finds federates whose last NER timestamp is
//     older than the federation's StallTimeout
//   - emits FederationHalted to EventLog + Outbox naming the stalled
//     federate
//
// Production rtid wires a goroutine that calls CheckStalls every second
// (or per a configurable interval). Tests call CheckStalls explicitly
// after Clock.Advance.
//
// # Determinism (NFR-DET-1)
//
// LBTS computation iterates a sorted snapshot of regulating federates
// (handle order). NER grants emit through the Outbox in handle-sorted
// order when multiple federates are simultaneously eligible. Tie-break
// rules per docs/AGENTS.md §10 (federate handle → object handle →
// attribute handle).
package time
