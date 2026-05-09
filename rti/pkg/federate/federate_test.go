// Scaffold owned by TASK-205½ (M21) — see docs/M21_DISPATCH_PLAN.md §6.
//
// All tests t.Skip(...) so the package compiles green throughout the
// scaffold phase. The implementer (W2½) replaces each Skip with a
// real assertion against bufconn rtid.

package federate

import "testing"

// 205½.1 — Connect + Close cycle; no goroutine leak across -race -count=10.
func TestConnectCloseLeakFree(t *testing.T) {
	t.Skip("TODO: TASK-205½ — implement Connect/Close round-trip with goroutine-leak check")
}

// 205½.2 — JoinFederation on a fresh rtid. Federation auto-created;
// federate gets handle; Events() channel returns a non-nil read-only chan.
func TestJoinFederationFresh(t *testing.T) {
	t.Skip("TODO: TASK-205½")
}

// 205½.3 — JoinFederation when the federation already exists (ALREADY_EXISTS swallowed).
func TestJoinFederationIdempotent(t *testing.T) {
	t.Skip("TODO: TASK-205½")
}

// 205½.4 — Resign closes the events channel within 1s; subsequent reads
// return zero-value (closed-channel semantics).
func TestResignClosesEvents(t *testing.T) {
	t.Skip("TODO: TASK-205½")
}

// 205½.5 — events-drain goroutine exits cleanly on Resign — no leaks.
func TestEventsGoroutineLeakFree(t *testing.T) {
	t.Skip("TODO: TASK-205½ — run under -race -count=10")
}

// 205½.6 — Typed-error round-trip: server returns FailedPrecondition + detail
// time_regulation_already_enabled → SDK surfaces ErrTimeRegulationAlreadyEnabled.
func TestTypedErrorRoundTrip(t *testing.T) {
	t.Skip("TODO: TASK-205½")
}
