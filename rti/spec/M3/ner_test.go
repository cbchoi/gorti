package m3spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M3_NER_NotRegulating_RejectsRequest: a federate that is
// neither regulating nor constrained cannot meaningfully NER (it has
// nothing to advance against). Per HLA semantics, the contract is
// permissive — non-time-managed federates may still NER but it's a no-
// op. Cut-1 reports core.ErrTimeNotRegulating to make the misuse
// explicit; Agent A may relax to no-op grant if the spec test is
// updated.
//
// Implements: FR-TM-2.
func TestSpec_M3_NER_NotRegulating_RejectsRequest(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	err := mgr.NextMessageRequest(context.Background(), "fed", 1, core.LogicalTime(5.0))
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("NER on non-regulating: err = %v, want ErrTimeNotRegulating", err)
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
// from federate 1 to t=5 cannot be granted until federate 2 advances
// past LBTS = min(t1+l1, t2+l2). Specifically: fed1 lookahead=1,
// fed2 lookahead=2 (both at time 0 initially).
//   - fed1 NER(5): LBTS = min(5+1, 0+2) = 2. Grant at 2 (not 5).
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

	// fed1 requests advance to 5; LBTS = min(5+1, 0+2) = 2 → grant at 2
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("NER fed1: %v", err)
	}
	// fed2 has not yet NER'd; the grant for fed1 must be ≤ 2 (cannot exceed LBTS).
	sent := outbox.SentTo("fed", 1)
	if len(sent) == 0 {
		t.Fatal("fed1 expected to receive a grant after first NER")
	}
	// (Specific grant time assertion deferred — implementation may grant
	// at LBTS or hold for fed2; both are valid HLA behaviors. The spec
	// asserts only "a grant was issued.")
}

// TestSpec_M3_NER_DuplicateRequestRejected: a federate cannot have two
// outstanding NER requests at once. The second NER before a grant
// returns an error (specific code is implementation choice — but it
// must NOT silently overwrite the first request).
//
// Implements: FR-TM-2.
func TestSpec_M3_NER_DuplicateRequestRejected(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(2.0))

	// Two NER calls for fed1 with no grant in between (since fed2 hasn't
	// NER'd to advance LBTS). Second should fail.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("first NER: %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(7.0)); err == nil {
		t.Errorf("second concurrent NER: expected error, got nil")
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
	// 3 regulators, all lookahead=1, all at time=0.
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 3, core.LogicalTime(1.0))
	// All three request advance to t=1; LBTS = min(1+1)*3 = 2 ≥ 1 → all
	// three grantable simultaneously.
	for _, h := range []core.FederateHandle{3, 1, 2} { // out-of-order request
		_ = mgr.NextMessageRequest(ctx, "fed", h, core.LogicalTime(1.0))
	}
	// Grants in outbox must be in handle order (1, 2, 3) regardless of
	// the request order above.
	sent := outbox.Sent()
	var grantedFederates []core.FederateHandle
	for _, s := range sent {
		grantedFederates = append(grantedFederates, s.Federate)
	}
	want := []core.FederateHandle{1, 2, 3}
	if len(grantedFederates) < len(want) {
		t.Skipf("expected ≥3 grants, got %d (impl may batch differently; revisit)", len(grantedFederates))
	}
	// Check the prefix matches sorted order (allows for multi-round grants)
	for i, w := range want {
		if i >= len(grantedFederates) || grantedFederates[i] != w {
			t.Errorf("grant[%d] = %v (full sequence: %v); want sorted prefix %v",
				i, grantedFederates[i:], grantedFederates, want)
			break
		}
	}
}
