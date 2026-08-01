// TASK-242 (M22 W3) — NER blocked-request semantics.
//
// M38 GA update: the "forced grant" this file originally reproduced
// (sole-pending NER granted at LBTS with clearPending=false,
// advance.go::decideGrant) is retired — IEEE 1516.1-2010 §8.8 defines
// no interim grant for a pending nextMessageRequest. The invariant the
// original race relied on survives in a stronger form: a blocked NER
// keeps the federate in time-advancing state (ErrTimeAdvancingState on
// re-issue) — now with NO grant callback at all until the request
// completes at min(requested, next TSO message time).

package m22spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M22_BlockedNERKeepsPending — a blocked NER (LBTS <
// requested, no queued TSO) stays pending with NO emission (M38 GA —
// §8.8 no interim grant): currentTime does not move and subsequent NER
// requests return ErrTimeAdvancingState.
func TestSpec_M22_BlockedNERKeepsPending(t *testing.T) {
	mgr, _ := newM22Manager(t)
	ctx := context.Background()

	// Two regulators with lookahead=1.0.
	if err := mgr.EnableRegulation(ctx, "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, 1.0); err != nil {
		t.Fatalf("EnableRegulation(2): %v", err)
	}

	// Federate 1 issues NER(t=10). Federate 2 has not issued any
	// advance, so its contribution to LBTS is currentTime+lookahead
	// = 0+1.0 = 1.0. LBTS = 1.0 < requested(10), no queued TSO → the
	// request HOLDS: no grant, currentTime unchanged (§8.8).
	if err := mgr.NextMessageRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatalf("NER(1, 10): %v", err)
	}

	// Snapshot: federate 1 is still at 0 and still pending.
	snap := mgr.Snapshot("fed")
	var fs1 core.TimeFederateState
	for _, fs := range snap.Federates {
		if fs.Handle == 1 {
			fs1 = fs
			break
		}
	}
	if fs1.CurrentTime != 0 {
		t.Errorf("blocked NER: currentTime = %v, want 0 (§8.8 — no interim grant)", fs1.CurrentTime)
	}
	if !fs1.HasPendingRequest {
		t.Error("blocked NER: HasPendingRequest = false; want true (request held)")
	}

	// Issuing another NER on federate 1 hits ErrTimeAdvancingState.
	err := mgr.NextMessageRequest(ctx, "fed", 1, 20)
	if !errors.Is(err, core.ErrTimeAdvancingState) {
		t.Errorf("second NER err = %v, want ErrTimeAdvancingState (blocked request stays pending)", err)
	}
}

// TestSpec_M22_FullGrantClearsPending — for symmetry with the above,
// pin that a *full* grant (LBTS covers the request) clears pending and
// permits the next NER cleanly.
func TestSpec_M22_FullGrantClearsPending(t *testing.T) {
	mgr, _ := newM22Manager(t)
	ctx := context.Background()

	if err := mgr.EnableRegulation(ctx, "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, 1.0); err != nil {
		t.Fatalf("EnableRegulation(2): %v", err)
	}

	// Federate 1 issues NER(t=5). Federate 2 also issues NER far
	// ahead (NER(100)) so its LBTS contribution = currentTime+lookahead
	// = 0+1.0 = 1.0... wait, NER promotes its floor to requestedTime,
	// so federate 2's contribution becomes max(currentTime+lookahead,
	// requestedTime) — i.e., min over peers in LBTS computation rises.
	// Practically: we need fed/2's contribution >= fed/1's requested
	// for fed/1 to get a full grant.
	if err := mgr.NextMessageRequest(ctx, "fed", 2, 100); err != nil {
		t.Fatalf("NER(2, 100): %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, 5); err != nil {
		t.Fatalf("NER(1, 5): %v", err)
	}

	snap := mgr.Snapshot("fed")
	var fs1 core.TimeFederateState
	for _, fs := range snap.Federates {
		if fs.Handle == 1 {
			fs1 = fs
			break
		}
	}
	if fs1.CurrentTime < 5 {
		t.Errorf("after full grant: currentTime = %v, want >= 5", fs1.CurrentTime)
	}
	// After a full grant, pendingNER must be cleared so next NER works.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, 6); err != nil {
		t.Errorf("second NER after full grant: %v (should succeed; pending was cleared)", err)
	}
}
