package time

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// M36 DB-2 — membership / LBTS-raising events must re-evaluate pending
// time-advance requests. Traced from parity-CE on tm_tso_ordering:
// OnFederateResign never re-ran tryGrantPending — a pending advance
// that becomes grantable when the last blocking regulator resigns hung
// forever.
//
// Shared scenario for the three tests below (M38 GA update: per §8.8
// the blocked NER now HOLDS with no interim grant — the pre-M38
// sole-pending forced grant at LBTS is retired, so the LBTS-raising
// event releases the ONLY grant):
//   - fed1 regulating (lookahead 1) NERs to 5.
//   - fed2 regulating (lookahead 1) at time 0 never requests.
//   - LBTS = min(5+1, 0+1) = 1 → fed1's NER(5) stays pending, nothing
//     is emitted (§8.8 — no interim grant).
//   - Then fed2's blocking contribution is removed (resign / disable
//     regulation / lookahead raise) → LBTS rises above 5 → fed1's full
//     grant at 5 MUST fire without any further advance call.
func setupBlockedNER(t *testing.T) (*Manager, *recordingOutbox, context.Context) {
	t.Helper()
	out := &recordingOutbox{}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable fed1: %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable fed2: %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5)); err != nil {
		t.Fatalf("NER fed1: %v", err)
	}
	// Precondition (M38 GA — §8.8): the blocked NER emits NOTHING; the
	// request is simply held.
	if sends := out.snapshot(); len(sends) != 0 {
		t.Fatalf("precondition: sends = %+v, want none (§8.8 — pending NER emits no interim grant)", sends)
	}
	return mgr, out, ctx
}

// expectFullGrantAt5 asserts the sole emission is fed1's full grant at
// the originally requested time 5.
func expectFullGrantAt5(t *testing.T, out *recordingOutbox, when string) {
	t.Helper()
	sends := out.snapshot()
	if len(sends) != 1 {
		t.Fatalf("after %s: sends = %+v, want exactly the full grant at 5", when, sends)
	}
	if sends[0].h != 1 || float64(sends[0].t) != 5 {
		t.Errorf("after %s: grant = (h=%d, t=%v), want (h=1, t=5)", when, sends[0].h, sends[0].t)
	}
}

// TestOnFederateResign_ReleasesPendingGrant: the last blocking regulator
// resigning must unblock the surviving federate's pending NER.
func TestOnFederateResign_ReleasesPendingGrant(t *testing.T) {
	mgr, out, ctx := setupBlockedNER(t)
	mgr.OnFederateResign(ctx, "fed", 2)
	expectFullGrantAt5(t, out, "fed2 resign")
}

// TestDisableRegulation_ReleasesPendingGrant: a regulator leaving the
// regulating set (without resigning) raises LBTS the same way and must
// also re-run the grant loop.
func TestDisableRegulation_ReleasesPendingGrant(t *testing.T) {
	mgr, out, ctx := setupBlockedNER(t)
	if err := mgr.DisableRegulation(ctx, "fed", 2); err != nil {
		t.Fatalf("DisableRegulation fed2: %v", err)
	}
	expectFullGrantAt5(t, out, "fed2 disable-regulation")
}

// TestModifyLookahead_RaisesLBTS_ReleasesPendingGrant: raising a
// regulator's lookahead raises its LBTS contribution; pending peers
// whose predicate becomes satisfied must be granted.
func TestModifyLookahead_RaisesLBTS_ReleasesPendingGrant(t *testing.T) {
	mgr, out, ctx := setupBlockedNER(t)
	// fed2 contribution becomes 0+10=10; LBTS = min(5+1, 10) = 6 > 5.
	if err := mgr.ModifyLookahead(ctx, "fed", 2, core.LogicalTime(10)); err != nil {
		t.Fatalf("ModifyLookahead fed2: %v", err)
	}
	expectFullGrantAt5(t, out, "fed2 lookahead raise")
}
