package m37spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M37_TwoPhaseDivest_AcquireThenConfirm: IEEE 1516.1-2010
// §7.3/§7.5/§7.6 — under the REAL two-phase protocol an acquirer's
// arrival does NOT transfer ownership; the divester receives
// requestDivestitureConfirmation and the transfer completes only on
// ConfirmDivestiture (acquirer then gets the §7.7 notification; the
// divester hears nothing further).
func TestSpec_M37_TwoPhaseDivest_AcquireThenConfirm(t *testing.T) {
	mgr, outbox := newOwnershipStack(t)
	ctx := context.Background()
	obj := core.ObjectHandle(7)
	attrs := []core.AttributeHandle{1}

	mgr.RegisterInitialOwnership(ownFed, 1, obj, attrs)
	if err := mgr.NegotiatedDivestTwoPhase(ctx, ownFed, 1, obj, attrs, []byte("nd")); err != nil {
		t.Fatalf("NegotiatedDivestTwoPhase: %v", err)
	}
	// No acquirer yet → divester must NOT be asked to confirm.
	if n := len(outbox.SentTo(1)); n != 0 {
		t.Fatalf("divester events before any acquirer = %d, want 0; got %+v", n, outbox.SentTo(1))
	}

	// Phase 1 — acquirer arrives: queued, divester asked to confirm.
	if err := mgr.Acquire(ctx, ownFed, 2, obj, attrs, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 1 {
		t.Fatalf("post-acquire owner = (%v, %v), want (1, true) — §7.6: transfer must wait for confirm", owner, owned)
	}
	evts1 := outbox.SentTo(1)
	if len(evts1) != 1 {
		t.Fatalf("divester events = %d, want 1 (confirmation request); got %+v", len(evts1), evts1)
	}
	confirmReq := evts1[0].GetOwnershipDivestConfirmed()
	if confirmReq == nil {
		t.Fatalf("event = %+v, want RequestDivestitureConfirmation", evts1[0])
	}
	if got := confirmReq.GetAttributeHandles(); len(got) != 1 || got[0] != 1 {
		t.Errorf("confirmation attrs = %v, want [1]", got)
	}
	if n := len(outbox.SentTo(2)); n != 0 {
		t.Errorf("acquirer events before confirm = %d, want 0", n)
	}

	// Phase 2 — divester confirms: transfer completes.
	if err := mgr.ConfirmDivestiture(ctx, ownFed, 1, obj, attrs); err != nil {
		t.Fatalf("ConfirmDivestiture: %v", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 2 {
		t.Errorf("post-confirm owner = (%v, %v), want (2, true)", owner, owned)
	}
	evts2 := outbox.SentTo(2)
	if len(evts2) != 1 || evts2[0].GetOwnershipAcquired() == nil {
		t.Errorf("acquirer events = %+v, want [OwnershipAcquired]", evts2)
	}
	// Divester got exactly the ONE confirmation request — no
	// post-transfer duplicate.
	if n := len(outbox.SentTo(1)); n != 1 {
		t.Errorf("divester total events = %d, want 1", n)
	}
}

// TestSpec_M37_TwoPhaseDivest_QueuedAcquirerAsksConfirmationImmediately:
// when an acquire is ALREADY queued, the two-phase divest asks the
// divester to confirm right away (§7.5) instead of transferring
// opportunistically.
func TestSpec_M37_TwoPhaseDivest_QueuedAcquirerAsksConfirmationImmediately(t *testing.T) {
	mgr, outbox := newOwnershipStack(t)
	ctx := context.Background()
	obj := core.ObjectHandle(7)
	attrs := []core.AttributeHandle{1}

	mgr.RegisterInitialOwnership(ownFed, 1, obj, attrs)
	if err := mgr.Acquire(ctx, ownFed, 2, obj, attrs, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	outbox.Reset() // drop the §7.11 release request

	if err := mgr.NegotiatedDivestTwoPhase(ctx, ownFed, 1, obj, attrs, nil); err != nil {
		t.Fatalf("NegotiatedDivestTwoPhase: %v", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 1 {
		t.Fatalf("owner = (%v, %v), want (1, true) — no opportunistic transfer under two-phase", owner, owned)
	}
	evts1 := outbox.SentTo(1)
	if len(evts1) != 1 || evts1[0].GetOwnershipDivestConfirmed() == nil {
		t.Fatalf("divester events = %+v, want [RequestDivestitureConfirmation]", evts1)
	}

	if err := mgr.ConfirmDivestiture(ctx, ownFed, 1, obj, attrs); err != nil {
		t.Fatalf("ConfirmDivestiture: %v", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 2 {
		t.Errorf("post-confirm owner = (%v, %v), want (2, true)", owner, owned)
	}
}

// TestSpec_M37_ConfirmDivestiture_Preconditions: confirming without a
// pending divest, from the wrong federate, or with no queued acquirer
// fails with ErrOwnershipNotInTransfer and mutates nothing.
func TestSpec_M37_ConfirmDivestiture_Preconditions(t *testing.T) {
	mgr, _ := newOwnershipStack(t)
	ctx := context.Background()
	obj := core.ObjectHandle(7)
	attrs := []core.AttributeHandle{1}

	mgr.RegisterInitialOwnership(ownFed, 1, obj, attrs)

	// No pending divest at all.
	if err := mgr.ConfirmDivestiture(ctx, ownFed, 1, obj, attrs); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
		t.Errorf("confirm without divest err = %v, want ErrOwnershipNotInTransfer", err)
	}

	if err := mgr.NegotiatedDivestTwoPhase(ctx, ownFed, 1, obj, attrs, nil); err != nil {
		t.Fatalf("NegotiatedDivestTwoPhase: %v", err)
	}
	// No queued acquirer yet.
	if err := mgr.ConfirmDivestiture(ctx, ownFed, 1, obj, attrs); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
		t.Errorf("confirm without acquirer err = %v, want ErrOwnershipNotInTransfer", err)
	}
	// Wrong federate.
	if err := mgr.ConfirmDivestiture(ctx, ownFed, 9, obj, attrs); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
		t.Errorf("confirm by non-owner err = %v, want ErrOwnershipNotInTransfer", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 1 {
		t.Errorf("owner = (%v, %v), want (1, true) — failed confirms must not mutate", owner, owned)
	}
}

// TestSpec_M37_OnePhaseDivest_LegacyFlowUnchanged: the frozen
// NegotiatedDivest (two_phase absent/false on the wire) keeps the
// pre-M37 one-phase behavior — Acquire completes the transfer
// immediately. Old clients that never call ConfirmDivestiture keep
// working (IR-PROTO-1).
func TestSpec_M37_OnePhaseDivest_LegacyFlowUnchanged(t *testing.T) {
	mgr, outbox := newOwnershipStack(t)
	ctx := context.Background()
	obj := core.ObjectHandle(7)
	attrs := []core.AttributeHandle{1}

	mgr.RegisterInitialOwnership(ownFed, 1, obj, attrs)
	if err := mgr.NegotiatedDivest(ctx, ownFed, 1, obj, attrs, nil); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	if err := mgr.Acquire(ctx, ownFed, 2, obj, attrs, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 2 {
		t.Errorf("owner = (%v, %v), want (2, true) — one-phase must transfer immediately", owner, owned)
	}
	if evts := outbox.SentTo(1); len(evts) != 1 || evts[0].GetOwnershipDivestConfirmed() == nil {
		t.Errorf("divester events = %+v, want [RequestDivestitureConfirmation] (post-transfer, pre-M37 shape)", evts)
	}
	if evts := outbox.SentTo(2); len(evts) != 1 || evts[0].GetOwnershipAcquired() == nil {
		t.Errorf("acquirer events = %+v, want [OwnershipAcquired]", evts)
	}
}
