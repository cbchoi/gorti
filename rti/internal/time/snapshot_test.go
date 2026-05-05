package time

import (
	"context"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestManager_Snapshot_EmptyFederation_LBTSPositiveInfinity(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := mgr.Snapshot("demo")
	if len(snap.Federates) != 0 {
		t.Errorf("Federates = %v, want empty", snap.Federates)
	}
	if !math.IsInf(float64(snap.LBTS), 1) {
		t.Errorf("LBTS = %v, want +Inf", snap.LBTS)
	}
}

func TestManager_Snapshot_RegulatingFederate(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "demo", 1, 2.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if err := mgr.EnableConstrained(ctx, "demo", 2); err != nil {
		t.Fatalf("EnableConstrained: %v", err)
	}
	snap := mgr.Snapshot("demo")
	if len(snap.Federates) != 2 {
		t.Fatalf("Federates len = %d, want 2", len(snap.Federates))
	}
	// federate 1: regulating, lookahead 2.
	if !snap.Federates[0].Regulating || snap.Federates[0].Handle != 1 || snap.Federates[0].Lookahead != 2.0 {
		t.Errorf("Federates[0] = %+v, want handle=1 reg=true lookahead=2", snap.Federates[0])
	}
	// federate 2: constrained.
	if !snap.Federates[1].Constrained || snap.Federates[1].Handle != 2 {
		t.Errorf("Federates[1] = %+v, want handle=2 constr=true", snap.Federates[1])
	}
	// LBTS = currentTime(0) + lookahead(2) = 2.
	if float64(snap.LBTS) != 2.0 {
		t.Errorf("LBTS = %v, want 2.0", snap.LBTS)
	}
}
