package m7spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// TestSpec_M7_NMRA_NotImplementedYet: pre-dispatch sentinel, flipped on
// M7 W1 landing per the same pattern as TestSpec_M7_TAR_NotImplementedYet.
// NMRA on a non-regulating, non-constrained federate must now return
// ErrTimeNotRegulating, not ErrNotImplemented.
//
// Implements: FR-TM-2 (M7 scope).
func TestSpec_M7_NMRA_NotImplementedYet(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	err := mgr.NextMessageRequestAvailable(context.Background(), "fed", 1, core.LogicalTime(5.0))
	if errors.Is(err, timepkg.ErrNotImplemented) {
		t.Errorf("NMRA after M7: still ErrNotImplemented; expected real implementation")
	}
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("NMRA on non-regulating: err = %v, want ErrTimeNotRegulating", err)
	}
}

// TestSpec_M7_NMRA_GrantAtLBTSEqualsT: per IEEE 1516.1-2010 §8.12, the
// NER "Available" variant permits grants AT LBTS. Mirrors TARA shape
// but with NMRA semantics (federate may receive messages exactly at t).
//
// SCAFFOLD.
//
// Implements: FR-TM-2.
func TestSpec_M7_NMRA_GrantAtLBTSEqualsT(t *testing.T) {
	mgr, outbox, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(0))

	err := mgr.NextMessageRequestAvailable(ctx, "fed", 1, core.LogicalTime(0))
	if errors.Is(err, timepkg.ErrNotImplemented) {
		t.Skip("NMRA not yet implemented (M7 RED state)")
	}
	if err != nil {
		t.Fatalf("NMRA: %v", err)
	}
	sent := outbox.SentTo("fed", 1)
	if len(sent) == 0 {
		t.Fatal("NMRA at LBTS: expected grant at t=0; got 0 sends")
	}
}
