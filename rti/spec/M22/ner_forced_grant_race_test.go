// TASK-242 (M22 W3) — NER+forced-grant race reproduction.
//
// Confirms H1 from the dispatch plan §2.5: the symptom is SDK-level
// semantics, not a server-side race. A "forced grant" (clearPending=
// false in advance.go::decideGrant) tells the federate "you may
// consume messages at LBTS, but you remain in time-advancing state."
// A federate that re-issues an advance primitive after a forced
// grant correctly hits ErrTimeAdvancingState.
//
// The fix lands in the example regulators (TASK-244): waitForGrant
// must track the originally-requested time and only return when
// grant.time >= requestedTime (a "full" grant), accumulating any
// intermediate forced grants.

package m22spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M22_ForcedGrantKeepsPending — when the manager emits a
// NER forced grant (sole-pending federate, LBTS < requested), the
// federate's pendingNER state must remain true so subsequent NER
// requests return ErrTimeAdvancingState. This is the by-design
// behavior; the test pins it.
func TestSpec_M22_ForcedGrantKeepsPending(t *testing.T) {
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
	// = 0+1.0 = 1.0. LBTS = 1.0 < requested(10) → forced grant fires
	// at LBTS=1.0; pendingNER stays true.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatalf("NER(1, 10): %v", err)
	}

	// Snapshot: federate 1 has currentTime advanced to 1.0 (forced
	// grant) but is still pending.
	snap := mgr.Snapshot("fed")
	var fs1 core.TimeFederateState
	for _, fs := range snap.Federates {
		if fs.Handle == 1 {
			fs1 = fs
			break
		}
	}
	if fs1.CurrentTime != 1.0 {
		t.Errorf("after forced grant: currentTime = %v, want 1.0", fs1.CurrentTime)
	}
	if !fs1.HasPendingRequest {
		t.Error("after forced grant: HasPendingRequest = false; want true (pending stays per spec)")
	}

	// Issuing another NER on federate 1 hits ErrTimeAdvancingState.
	// This is the symptom the M21 examples worked around.
	err := mgr.NextMessageRequest(ctx, "fed", 1, 20)
	if !errors.Is(err, core.ErrTimeAdvancingState) {
		t.Errorf("second NER err = %v, want ErrTimeAdvancingState (forced grant must keep pending)", err)
	}
}

// TestSpec_M22_FullGrantClearsPending — for symmetry with the above,
// pin that a *full* grant (LBTS >= requested) clears pending and
// permits the next NER cleanly. This is the path SDK-side waitForGrant
// must hold out for after a forced grant arrives.
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
