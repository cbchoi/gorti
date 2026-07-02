package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M37_AcquireIfAvailable_GrantsUnownedReportsRest: IEEE
// 1516.1-2010 §7.9/§7.10 — acquireIfAvailable atomically grants only
// the currently-unowned attributes (§7.7 acquisition notification) and
// reports the owned remainder via attributeOwnershipUnavailable; NO
// pending acquire entry is queued and the owner hears nothing.
func TestSpec_M37_AcquireIfAvailable_GrantsUnownedReportsRest(t *testing.T) {
	mgr, outbox := newOwnershipStack(t)
	ctx := context.Background()
	obj := core.ObjectHandle(7)

	// Attr 1 is owned by federate 1; attr 2 is unowned.
	mgr.RegisterInitialOwnership(ownFed, 1, obj, []core.AttributeHandle{1})

	if err := mgr.AcquireIfAvailable(ctx, ownFed, 2, obj,
		[]core.AttributeHandle{1, 2}, []byte("t")); err != nil {
		t.Fatalf("AcquireIfAvailable: %v", err)
	}

	// Acquirer: acquired{2} + unavailable{1}.
	evts2 := outbox.SentTo(2)
	if len(evts2) != 2 {
		t.Fatalf("acquirer events = %d, want 2 (acquired, unavailable); got %+v", len(evts2), evts2)
	}
	acq := evts2[0].GetOwnershipAcquired()
	if acq == nil || len(acq.GetAttributeHandles()) != 1 || acq.GetAttributeHandles()[0] != 2 {
		t.Errorf("event[0] = %+v, want OwnershipAcquired{attrs=[2]}", evts2[0])
	}
	unav := evts2[1].GetOwnershipUnavailable()
	if unav == nil {
		t.Fatalf("event[1] = %+v, want AttributeOwnershipUnavailable", evts2[1])
	}
	if unav.GetObjectHandle() != uint64(obj) {
		t.Errorf("unavailable.object = %d, want %d", unav.GetObjectHandle(), obj)
	}
	if got := unav.GetAttributeHandles(); len(got) != 1 || got[0] != 1 {
		t.Errorf("unavailable.attrs = %v, want [1]", got)
	}

	// Owner: silence — no §7.11 release request, nothing queued.
	if n := len(outbox.SentTo(1)); n != 0 {
		t.Errorf("owner received %d events, want 0", n)
	}

	// Ownership: attr 2 transferred, attr 1 untouched.
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 2); !owned || owner != 2 {
		t.Errorf("attr 2 owner = (%v, %v), want (2, true)", owner, owned)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 1 {
		t.Errorf("attr 1 owner = (%v, %v), want (1, true)", owner, owned)
	}

	// NO pending entry: when the owner later divests-if-wanted, nothing
	// transfers to federate 2 (contrast with plain Acquire's queue).
	if err := mgr.DivestIfWanted(ctx, ownFed, 1, obj, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("DivestIfWanted: %v", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); owned && owner == 2 {
		t.Errorf("attr 1 transferred to the ifAvailable caller; §7.9 must not queue a pending acquire")
	}
}

// TestSpec_M37_AcquireIfAvailable_DivestingOwnerCountsAsAvailable: an
// attribute whose owner already negotiated-divested is "available" —
// the ifAvailable acquire completes the transfer (both §7.5/§7.7
// notifications) with no unavailable report.
func TestSpec_M37_AcquireIfAvailable_DivestingOwnerCountsAsAvailable(t *testing.T) {
	mgr, outbox := newOwnershipStack(t)
	ctx := context.Background()
	obj := core.ObjectHandle(7)
	attrs := []core.AttributeHandle{1}

	mgr.RegisterInitialOwnership(ownFed, 1, obj, attrs)
	if err := mgr.NegotiatedDivest(ctx, ownFed, 1, obj, attrs, nil); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	outbox.Reset()

	if err := mgr.AcquireIfAvailable(ctx, ownFed, 2, obj, attrs, nil); err != nil {
		t.Fatalf("AcquireIfAvailable: %v", err)
	}

	if owner, owned := mgr.QueryOwnership(ownFed, obj, 1); !owned || owner != 2 {
		t.Errorf("attr 1 owner = (%v, %v), want (2, true)", owner, owned)
	}
	evts2 := outbox.SentTo(2)
	if len(evts2) != 1 || evts2[0].GetOwnershipAcquired() == nil {
		t.Errorf("acquirer events = %+v, want [OwnershipAcquired]", evts2)
	}
	for _, ev := range evts2 {
		if ev.GetOwnershipUnavailable() != nil {
			t.Errorf("unexpected AttributeOwnershipUnavailable: %+v", ev)
		}
	}
}
