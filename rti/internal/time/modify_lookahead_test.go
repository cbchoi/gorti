// TASK-202b (M21) — see docs/M21_DISPATCH_PLAN.md §6.

package time

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// 202b.1 — ModifyLookahead before EnableRegulation → ErrTimeNotRegulating.
func TestModifyLookaheadBeforeEnable(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = mgr.ModifyLookahead(context.Background(), "fed", 1, core.LogicalTime(2.0))
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("ModifyLookahead before enable: err = %v, want ErrTimeNotRegulating", err)
	}
}

// 202b.2 — EnableRegulation(la=1.0); ModifyLookahead(la=2.0); Snapshot lookahead = 2.0.
func TestModifyLookaheadUpdates(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if err := mgr.ModifyLookahead(ctx, "fed", 1, core.LogicalTime(2.0)); err != nil {
		t.Fatalf("ModifyLookahead: %v", err)
	}
	got := mgr.states.snapshot("fed", 1)
	if got.lookahead != core.LogicalTime(2.0) {
		t.Errorf("lookahead = %v, want 2.0", got.lookahead)
	}
	if !got.regulating {
		t.Errorf("regulating = false, want true (ModifyLookahead must NOT toggle regulating off)")
	}
}

// 202b.3 — ModifyLookahead with NaN / negative / +Inf → ErrTimeInvalidLookahead.
func TestModifyLookaheadInvalidValue(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	cases := map[string]float64{
		"NaN":      math.NaN(),
		"negative": -1.0,
		"plus_inf": math.Inf(1),
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			err := mgr.ModifyLookahead(ctx, "fed", 1, core.LogicalTime(v))
			if !errors.Is(err, core.ErrTimeInvalidLookahead) {
				t.Errorf("ModifyLookahead(%v): err = %v, want ErrTimeInvalidLookahead", v, err)
			}
		})
	}
}

// 202b.4 — ModifyLookahead while NER pending → OK; pending request's
// captured lookahead is not perturbed (the new lookahead applies to
// future requests only).
//
// Implementation note: the pending NER's lookahead is captured at
// dispatchAdvance time (advance.go) and stored in nerStore's
// requested time. The state-side `lookahead` field is what new requests
// will see; ModifyLookahead updates that field but does NOT modify
// any pending-request bookkeeping.
func TestModifyLookaheadWhileNERPending(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("NextMessageRequest: %v", err)
	}
	// Now modify lookahead while NER is pending. Should succeed; new
	// lookahead is observable via Snapshot.
	if err := mgr.ModifyLookahead(ctx, "fed", 1, core.LogicalTime(0.5)); err != nil {
		t.Fatalf("ModifyLookahead while NER pending: %v", err)
	}
	got := mgr.states.snapshot("fed", 1)
	if got.lookahead != core.LogicalTime(0.5) {
		t.Errorf("lookahead = %v, want 0.5", got.lookahead)
	}
}
