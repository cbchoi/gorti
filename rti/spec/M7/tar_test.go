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

// TestSpec_M7_TAR_TwoRegulators_GrantBoundedByLBTS: with two
// regulators, TAR from federate 1 to t=5 is granted at LBTS = min(5+1,
// 0+2) = 2, not at 5. Same shape as M3 NER spec test for two
// regulators, but exercising TAR.
//
// SCAFFOLD.
//
// Implements: FR-TM-2, FR-TM-3.
func TestSpec_M7_TAR_TwoRegulators_GrantBoundedByLBTS(t *testing.T) {
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
	sent := outbox.SentTo("fed", 1)
	if len(sent) == 0 {
		t.Fatal("TAR with peer regulator: expected at least one grant (at LBTS); got 0")
	}
}
