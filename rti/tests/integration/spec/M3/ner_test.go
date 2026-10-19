package m3spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// An unconstrained federate advances without waiting for a regulating peer,
// whether or not the requester itself is regulating.
//
// Implements: FR-TM-2.
func TestSpec_M3_NER_Unconstrained_GrantsDespitePeer(t *testing.T) {
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
				if err := mgr.NextMessageRequest(ctx, "fed", 1, target); err != nil {
					t.Fatalf("NER(%v): %v", target, err)
				}
				sent := outbox.SentTo("fed", 1)
				if len(sent) != i+1 {
					t.Fatalf("NER(%v): got %d callbacks, want %d", target, len(sent), i+1)
				}
				grant, ok := sent[i].Event.(*timepkg.TimeAdvanceGrant)
				if !ok || grant.Time != target {
					t.Fatalf("callback = %+v, want TAG(%v)", sent[i].Event, target)
				}
				state := mgr.Metrics("fed", 1)
				if state.CurrentTime != target || state.Advancing || state.Constrained || state.Regulating != tc.regulating {
					t.Fatalf("state after NER(%v) = %+v", target, state)
				}
			}
			if err := mgr.NextMessageRequest(ctx, "fed", 1, 4); !errors.Is(err, core.ErrTimeRequestInPast) {
				t.Fatalf("past NER = %v, want ErrTimeRequestInPast", err)
			}
			if state := mgr.Metrics("fed", 1); state.CurrentTime != 6 || state.Advancing || len(outbox.Sent()) != 2 {
				t.Fatalf("rejected NER changed time or callbacks: %+v, %+v", state, outbox.Sent())
			}
			if peer := mgr.Metrics("fed", 2); peer.CurrentTime != 0 || peer.Advancing {
				t.Fatalf("idle peer advanced: %+v", peer)
			}
		})
	}
}

// TestSpec_M3_NER_RequestInPast_Rejected: requesting an advance to a
// time less than the federate's current logical time returns
// core.ErrTimeRequestInPast.
//
// M36 DB-1: the original test asserted rejection of t <
// currentTime + lookahead (advance to 1.0 from time 0 with lookahead
// 2.0). That floor was a spec violation — IEEE 1516.1 §8.8 only
// requires t >= currentTime; lookahead constrains OUTGOING TSO
// timestamps. The test now advances the sole regulator to 5.0 first
// and asserts rejection of a genuinely past target, and additionally
// asserts the sub-lookahead target is ACCEPTED.
//
// Implements: FR-TM-2, FR-TM-5.
func TestSpec_M3_NER_RequestInPast_Rejected(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(2.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	// Sub-lookahead target from time 0 is legal (§8.8): sole regulator
	// full-grants immediately, advancing currentTime to 1.0.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("NER to 1.0 (below lookahead, above currentTime): %v", err)
	}
	// Advance further to 5.0 so a past target exists.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("NER to 5.0: %v", err)
	}
	// A target below currentTime (5.0) is in the past.
	err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(4.0))
	if !errors.Is(err, core.ErrTimeRequestInPast) {
		t.Errorf("NER to past: err = %v, want ErrTimeRequestInPast", err)
	}
}

// TestSpec_M3_NER_SoleRegulator_GrantsImmediately: with only one
// regulating federate, NER for time t is granted immediately because
// LBTS = t + lookahead ≥ t.
//
// Implements: FR-TM-2, FR-TM-3.
func TestSpec_M3_NER_SoleRegulator_GrantsImmediately(t *testing.T) {
	mgr, outbox, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("NextMessageRequest: %v", err)
	}

	// Outbox should have received exactly one TimeAdvanceGrant for federate 1.
	sent := outbox.SentTo("fed", 1)
	if len(sent) == 0 {
		t.Fatalf("expected TimeAdvanceGrant for federate 1, got %d sends to it (total outbox=%d)",
			len(sent), len(outbox.Sent()))
	}
}

// TestSpec_M3_NER_TwoRegulators_GrantWaits: with two regulators, a NER
// from constrained federate 1 to t=5 cannot be granted until federate 2 advances
// past LBTS = min(t1+l1, t2+l2). Specifically: fed1 lookahead=1,
// fed2 lookahead=2 (both at time 0 initially).
//   - fed1 NER(5): LBTS = min(5+1, 0+2) = 2 < 5, no queued TSO → the
//     request HOLDS with no grant (M38 GA — IEEE 1516.1-2010 §8.8
//     defines no interim grant; the pre-M38 forced grant at LBTS is
//     retired).
//   - Then fed2 NER(5): LBTS = min(5+1, 5+2) = 6. Both grant at 5.
//
// Implements: FR-TM-2, FR-TM-3.
func TestSpec_M3_NER_TwoRegulators_GrantWaits(t *testing.T) {
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

	// fed1 requests advance to 5; LBTS = min(5+1, 0+2) = 2 → hold.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("NER fed1: %v", err)
	}
	// fed2 has not yet NER'd: fed1's request must WAIT (§8.8 — no
	// grant below the requested time without a queued TSO message).
	if sent := outbox.SentTo("fed", 1); len(sent) != 0 {
		t.Fatalf("fed1 received %v before LBTS covered its request, want none", sent)
	}
	// fed2's own NER(5) promotes its floor to 5+2=7 → LBTS = 6 > 5:
	// both pending requests complete at exactly t=5.
	if err := mgr.NextMessageRequest(ctx, "fed", 2, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("NER fed2: %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		sent := outbox.SentTo("fed", h)
		if len(sent) != 1 {
			t.Fatalf("fed%d grants = %v, want exactly one at 5", h, sent)
		}
		if grant, ok := sent[0].Event.(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 5 {
			t.Fatalf("fed%d callback = %+v, want TAG(5)", h, sent[0].Event)
		}
		if state := mgr.Metrics("fed", h); state.CurrentTime != 5 || state.Advancing {
			t.Fatalf("fed%d state after grant = %+v", h, state)
		}
	}
}

// TestSpec_M3_NER_DuplicateRequestRejected: a federate cannot have two
// outstanding NER requests at once. The second NER before a grant
// returns an error (specific code is implementation choice — but it
// must NOT silently overwrite the first request).
//
// Implements: FR-TM-2.
func TestSpec_M3_NER_DuplicateRequestRejected(t *testing.T) {
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

	// Two NER calls for fed1 with no grant in between (since fed2 hasn't
	// NER'd to advance LBTS). Second should fail.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("first NER: %v", err)
	}
	before := mgr.Snapshot("fed").Federates[0]
	if !before.HasPendingRequest || before.PendingRequestTime != 5 || before.CurrentTime != 0 {
		t.Fatalf("first NER did not remain pending: %+v", before)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(7.0)); !errors.Is(err, core.ErrTimeAdvancingState) {
		t.Errorf("second concurrent NER = %v, want ErrTimeAdvancingState", err)
	}
	if after := mgr.Snapshot("fed").Federates[0]; after != before || len(outbox.Sent()) != 0 {
		t.Fatalf("duplicate NER changed pending state or emitted a callback: before=%+v after=%+v", before, after)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 2, 5); err != nil {
		t.Fatal(err)
	}
	sent := outbox.SentTo("fed", 1)
	if len(sent) != 1 {
		t.Fatalf("held NER callbacks = %v, want one", sent)
	}
	if grant, ok := sent[0].Event.(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 5 {
		t.Fatalf("duplicate NER overwrote the original target: %+v", sent[0].Event)
	}
}

// TestSpec_M3_NER_SimultaneousReady_DeterministicGrantOrder: when
// multiple federates are simultaneously eligible for a grant (LBTS
// satisfied for all), grants are emitted in handle-sorted order. This
// is the determinism contract — different goroutine scheduling must
// not produce different grant orders.
//
// Implements: FR-TM-2, NFR-DET-1.
func TestSpec_M3_NER_SimultaneousReady_DeterministicGrantOrder(t *testing.T) {
	mgr, outbox, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	// All three requesters are constrained so none grants before all are ready.
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 3, core.LogicalTime(1.0))
	for _, h := range []core.FederateHandle{1, 2, 3} {
		if err := mgr.EnableConstrained(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	// All three request advance to t=1; LBTS = min(1+1)*3 = 2 ≥ 1 → all
	// three grantable simultaneously.
	for i, h := range []core.FederateHandle{3, 1, 2} { // out-of-order request
		if err := mgr.NextMessageRequest(ctx, "fed", h, core.LogicalTime(1.0)); err != nil {
			t.Fatal(err)
		}
		if i < 2 && len(outbox.Sent()) != 0 {
			t.Fatal("grant emitted before all three requests were ready")
		}
	}
	// Grants in outbox must be in handle order (1, 2, 3) regardless of
	// the request order above.
	sent := outbox.Sent()
	var grantedFederates []core.FederateHandle
	for _, s := range sent {
		if grant, ok := s.Event.(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 1 {
			t.Fatalf("callback = %+v, want TAG(1)", s.Event)
		}
		grantedFederates = append(grantedFederates, s.Federate)
	}
	want := []core.FederateHandle{1, 2, 3}
	if len(grantedFederates) != len(want) {
		t.Fatalf("expected 3 grants, got %v", grantedFederates)
	}
	// All requests become ready in the same round, in handle order.
	for i, w := range want {
		if i >= len(grantedFederates) || grantedFederates[i] != w {
			t.Errorf("grant[%d] = %v (full sequence: %v); want sorted prefix %v",
				i, grantedFederates[i:], grantedFederates, want)
			break
		}
	}
}
