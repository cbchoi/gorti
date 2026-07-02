package m7spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// TestSpec_M7_TAR_NotImplementedYet: pre-dispatch sentinel that the
// orchestrator's intent comment marks as "Agent A's M7 work flips this
// to a real test". Now that TAR is implemented (M7 W1), the assertion
// inverts: TAR on a non-regulating, non-constrained federate must
// return ErrTimeNotRegulating (the dispatcher's eligibility check),
// NOT ErrNotImplemented. The test name is preserved for git-history
// hygiene; the function still serves as the per-method-table sentinel
// (TAR is wired all the way through dispatchAdvance).
//
// Implements: FR-TM-2 (M7 scope).
func TestSpec_M7_TAR_NotImplementedYet(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	err := mgr.TimeAdvanceRequest(context.Background(), "fed", 1, core.LogicalTime(5.0))
	if errors.Is(err, timepkg.ErrNotImplemented) {
		t.Errorf("TAR after M7: still ErrNotImplemented; expected real implementation")
	}
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("TAR on non-regulating: err = %v, want ErrTimeNotRegulating", err)
	}
}

// TestSpec_M7_TAR_SoleRegulator_GrantsImmediately: with only one
// regulating federate, TAR for time t is granted immediately at
// min(t, LBTS) where LBTS = t + lookahead ≥ t, so grant fires at t.
//
// SCAFFOLD — turns from skip to real assertion when Agent A lands TAR.
//
// Implements: FR-TM-2; IEEE 1516.1-2010 §8.10.
func TestSpec_M7_TAR_SoleRegulator_GrantsImmediately(t *testing.T) {
	mgr, outbox, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	err := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(5.0))
	if errors.Is(err, timepkg.ErrNotImplemented) {
		t.Skip("TAR not yet implemented (M7 RED state)")
	}
	if err != nil {
		t.Fatalf("TimeAdvanceRequest: %v", err)
	}
	sent := outbox.SentTo("fed", 1)
	if len(sent) == 0 {
		t.Fatal("TAR sole regulator: expected a TimeAdvanceGrant; got 0 outbox sends")
	}
}

// TestSpec_M7_TAR_TwoRegulators_HeldUntilLBTSCoversRequest: M37 EB-5
// re-pin. IEEE 1516.1-2010 §8.10: a timeAdvanceRequest(t) is granted
// at EXACTLY t once LBTS covers it; while LBTS < t the request HOLDS
// (the pre-M37 behavior granted incrementally at LBTS = min(5+1,
// 0+2) = 2, silently parking the federate below its requested time —
// early grants are §8.12 FQR's contract). Here federate 1's TAR(5)
// holds against the idle peer's LBTS contribution 0+2=2, then fires
// at 5 when the peer's own TAR(5) promotes its floor to 5+2=7.
//
// Implements: FR-TM-2, FR-TM-3.
func TestSpec_M7_TAR_TwoRegulators_HeldUntilLBTSCoversRequest(t *testing.T) {
	mgr, outbox, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(2.0))

	err := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(5.0))
	if errors.Is(err, timepkg.ErrNotImplemented) {
		t.Skip("TAR not yet implemented (M7 RED state)")
	}
	if err != nil {
		t.Fatalf("TAR: %v", err)
	}
	if sent := outbox.SentTo("fed", 1); len(sent) != 0 {
		t.Fatalf("TAR with LBTS(2) below requested(5): got %d grants, want the request HELD (§8.10)", len(sent))
	}

	// Peer advances: its pending floor becomes 5+2=7 → LBTS covers 5 →
	// federate 1's grant fires at exactly the requested time.
	if err := mgr.TimeAdvanceRequest(ctx, "fed", 2, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("peer TAR: %v", err)
	}
	sent := outbox.SentTo("fed", 1)
	if len(sent) == 0 {
		t.Fatal("TAR after peer advance: expected the held grant to fire at the requested time; got 0")
	}
}
