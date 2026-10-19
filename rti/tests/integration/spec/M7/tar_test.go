package m7spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// TAR grants unconstrained requesters without waiting for a regulating peer.
//
// Implements: FR-TM-2 (M7 scope).
func TestSpec_M7_TAR_Unconstrained_GrantsDespitePeer(t *testing.T) {
	assertUnconstrainedAdvance(t, (*timepkg.Manager).TimeAdvanceRequest)
}

func assertUnconstrainedAdvance(t *testing.T, advance func(*timepkg.Manager, context.Context, core.FederationName, core.FederateHandle, core.LogicalTime) error) {
	t.Helper()
	for _, tc := range []struct {
		name       string
		regulating bool
	}{{"unengaged", false}, {"regulating-only", true}} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, outbox, _ := newTestTimeManager(t)
			if mgr == nil {
				t.Fatal("time.Manager unavailable")
			}
			ctx := context.Background()
			if err := mgr.EnableRegulation(ctx, "fed", 2, 1); err != nil {
				t.Fatal(err)
			}
			if tc.regulating {
				if err := mgr.EnableRegulation(ctx, "fed", 1, 1); err != nil {
					t.Fatal(err)
				}
			}
			for i, target := range []core.LogicalTime{5, 6} {
				if err := advance(mgr, ctx, "fed", 1, target); err != nil {
					t.Fatalf("advance(%v): %v", target, err)
				}
				sent := outbox.SentTo("fed", 1)
				if len(sent) != i+1 {
					t.Fatalf("advance(%v): got %d callbacks, want %d", target, len(sent), i+1)
				}
				grant, ok := sent[i].Event.(*timepkg.TimeAdvanceGrant)
				if !ok || grant.Time != target {
					t.Fatalf("callback = %+v, want TAG(%v)", sent[i].Event, target)
				}
				state := mgr.Metrics("fed", 1)
				if state.CurrentTime != target || state.Advancing || state.Constrained || state.Regulating != tc.regulating {
					t.Fatalf("state after advance(%v) = %+v", target, state)
				}
			}
			if err := advance(mgr, ctx, "fed", 1, 4); !errors.Is(err, core.ErrTimeRequestInPast) {
				t.Fatalf("past advance = %v, want ErrTimeRequestInPast", err)
			}
			if state := mgr.Metrics("fed", 1); state.CurrentTime != 6 || state.Advancing || len(outbox.Sent()) != 2 {
				t.Fatalf("rejected advance changed time or callbacks: %+v, %+v", state, outbox.Sent())
			}
			if peer := mgr.Metrics("fed", 2); peer.CurrentTime != 0 || peer.Advancing {
				t.Fatalf("idle peer advanced: %+v", peer)
			}
		})
	}
}

// TestSpec_M7_TAR_SoleRegulator_GrantsImmediately: with only one
// regulating federate, TAR for time t is granted immediately at
// min(t, LBTS) where LBTS = t + lookahead ≥ t, so grant fires at t.
//
// SCAFFOLD — turns from a skip into a real assertion when TAR lands.
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
// early grants are §8.12 FQR's contract). Here constrained federate 1's TAR(5)
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
	if err := mgr.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}

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
	if state := mgr.Metrics("fed", 1); state.CurrentTime != 0 || !state.Advancing {
		t.Fatalf("held TAR state = %+v", state)
	}

	// Peer advances: its pending floor becomes 5+2=7 → LBTS covers 5 →
	// federate 1's grant fires at exactly the requested time.
	if err := mgr.TimeAdvanceRequest(ctx, "fed", 2, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("peer TAR: %v", err)
	}
	sent := outbox.SentTo("fed", 1)
	if len(sent) != 1 {
		t.Fatalf("TAR after peer advance: got %d callbacks, want one", len(sent))
	}
	if grant, ok := sent[0].Event.(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 5 {
		t.Fatalf("TAR callback = %+v, want TAG(5)", sent[0].Event)
	}
	if state := mgr.Metrics("fed", 1); state.CurrentTime != 5 || state.Advancing {
		t.Fatalf("TAR state after grant = %+v", state)
	}
}
