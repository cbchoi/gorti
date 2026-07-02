package m37spec

import (
	"context"
	"testing"
	gotime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// M37 Agent EF — grant-targeting invariant behind the tm_ner_pair
// conformance fixture (IEEE 1516.1-2010 §8.13).
//
// A timeAdvanceGrant is addressed to the ONE federate whose outstanding
// §8.8-§8.12 advance request it answers. In particular, when a
// constrained federate's sole-pending NER is force-granted at LBTS
// (gorti's documented interim NER semantics — advance.go decideGrant),
// a regulating peer with NO pending request of its own must hear
// NOTHING: an unsolicited grant would silently advance its logical
// time and turn its next boundary-legal TSO send (ts == time +
// lookahead, §8.1.2) into an InvalidLogicalTime rejection — the exact
// failure shape of the M37 tm_ner_pair regression report (which turned
// out to be a stale fixture binary, not a server bug; this test pins
// the server invariant regardless).

const grantTargetFed = core.FederationName("m37_forced_grant_targeting")

// grantsByRecipient filters the outbox recording down to
// TimeAdvanceGrant events, keyed by recipient handle.
func grantsByRecipient(outbox *fakeOutbox) map[core.FederateHandle][]*timepkg.TimeAdvanceGrant {
	got := map[core.FederateHandle][]*timepkg.TimeAdvanceGrant{}
	for _, rec := range outbox.Sent() {
		if g, ok := rec.Event.(*timepkg.TimeAdvanceGrant); ok {
			got[rec.Federate] = append(got[rec.Federate], g)
		}
	}
	return got
}

// TestSpec_M37_ForcedGrant_TargetsRequesterOnly: federate 1 regulates
// (lookahead 1.0, no pending request); federate 2 is constrained and
// issues NER(5). LBTS = 0 + 1.0 = 1.0, federate 2 is the sole pending
// request, so the forced-grant path fires at LBTS. The grant must go
// to federate 2 alone; federate 1 must receive no TimeAdvanceGrant.
func TestSpec_M37_ForcedGrant_TargetsRequesterOnly(t *testing.T) {
	ctx := context.Background()
	outbox := newFakeOutbox()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewFakeClock(gotime.Unix(0, 0)),
		Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}

	if err := mgr.EnableRegulation(ctx, grantTargetFed, 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, grantTargetFed, 2); err != nil {
		t.Fatalf("EnableConstrained(2): %v", err)
	}

	if err := mgr.NextMessageRequest(ctx, grantTargetFed, 2, 5.0); err != nil {
		t.Fatalf("NER(2, 5.0): %v", err)
	}

	got := grantsByRecipient(outbox)
	if n := len(got[1]); n != 0 {
		t.Errorf("regulating federate 1 (no pending request) received %d TimeAdvanceGrant(s) %v, want 0", n, got[1])
	}
	if n := len(got[2]); n != 1 {
		t.Fatalf("requester federate 2 received %d TimeAdvanceGrant(s), want exactly 1 (forced grant at LBTS)", n)
	}
	if ft := float64(got[2][0].Time); ft != 1.0 {
		t.Errorf("forced grant time = %v, want 1.0 (LBTS = 0 + lookahead 1.0)", ft)
	}
}

// TestSpec_M37_NER_AtLBTSBoundary_NoGrantAtAll: the tm_ner_pair
// lockstep precondition. Federate 2's NER(1.0) with LBTS == 1.0 must
// HOLD (strict predicate LBTS > t fails; forced grant needs LBTS < t):
// nobody — neither the requester nor the idle regulator — receives a
// grant until the regulator's own NER(1.0) raises LBTS to 2.0.
func TestSpec_M37_NER_AtLBTSBoundary_NoGrantAtAll(t *testing.T) {
	ctx := context.Background()
	outbox := newFakeOutbox()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewFakeClock(gotime.Unix(0, 0)),
		Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}

	if err := mgr.EnableRegulation(ctx, grantTargetFed, 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, grantTargetFed, 2); err != nil {
		t.Fatalf("EnableConstrained(2): %v", err)
	}

	// Constrained NER(1.0) at LBTS == 1.0: no grant to anyone.
	if err := mgr.NextMessageRequest(ctx, grantTargetFed, 2, 1.0); err != nil {
		t.Fatalf("NER(2, 1.0): %v", err)
	}
	if got := grantsByRecipient(outbox); len(got) != 0 {
		t.Fatalf("grants after boundary NER = %v, want none (LBTS == requested holds for strict NER)", got)
	}

	// Regulator's own NER(1.0) promotes its LBTS floor to requested +
	// lookahead = 2.0: BOTH pending requests are now grantable, each
	// federate exactly once, at t = 1.0.
	if err := mgr.NextMessageRequest(ctx, grantTargetFed, 1, 1.0); err != nil {
		t.Fatalf("NER(1, 1.0): %v", err)
	}
	got := grantsByRecipient(outbox)
	for _, h := range []core.FederateHandle{1, 2} {
		if n := len(got[h]); n != 1 {
			t.Errorf("federate %d received %d TimeAdvanceGrant(s), want exactly 1", h, n)
			continue
		}
		if ft := float64(got[h][0].Time); ft != 1.0 {
			t.Errorf("federate %d grant time = %v, want 1.0", h, ft)
		}
	}
}
