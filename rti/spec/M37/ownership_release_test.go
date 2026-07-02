package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ownership"
)

const ownFed = core.FederationName("m37_ownership_release")

func newOwnershipStack(t *testing.T) (*ownership.Manager, *fakeOutbox) {
	t.Helper()
	outbox := newFakeOutbox()
	mgr, err := ownership.New(ownership.Options{Outbox: outbox})
	if err != nil {
		t.Fatalf("ownership.New: %v", err)
	}
	return mgr, outbox
}

// TestSpec_M37_Acquire_OwnedAttributes_RequestsReleaseFromOwner: IEEE
// 1516.1-2010 §7.11 — attributeOwnershipAcquisition against attributes
// owned by another federate queues the acquire (pre-M37 behavior) AND
// fires requestAttributeOwnershipRelease at the current owner, echoing
// the acquirer's tag.
func TestSpec_M37_Acquire_OwnedAttributes_RequestsReleaseFromOwner(t *testing.T) {
	mgr, outbox := newOwnershipStack(t)
	ctx := context.Background()
	obj := core.ObjectHandle(7)
	attrs := []core.AttributeHandle{1, 2}

	mgr.RegisterInitialOwnership(ownFed, 1, obj, attrs)

	if err := mgr.Acquire(ctx, ownFed, 2, obj, attrs, []byte("gimme")); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Owner (federate 1) hears the release request.
	evts1 := outbox.SentTo(1)
	if len(evts1) != 1 {
		t.Fatalf("owner events = %d, want 1 (release request); got %+v", len(evts1), evts1)
	}
	rel := evts1[0].GetOwnershipReleaseRequested()
	if rel == nil {
		t.Fatalf("event[0] = %+v, want RequestAttributeOwnershipRelease", evts1[0])
	}
	if rel.GetObjectHandle() != uint64(obj) {
		t.Errorf("release.object = %d, want %d", rel.GetObjectHandle(), obj)
	}
	if got := rel.GetAttributeHandles(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("release.attrs = %v, want [1 2]", got)
	}
	if string(rel.GetTag()) != "gimme" {
		t.Errorf("release.tag = %q, want %q", rel.GetTag(), "gimme")
	}

	// The acquire stays QUEUED — ownership unchanged, no acquirer event.
	if owner, owned := mgr.QueryOwnership(ownFed, obj, attrs[0]); !owned || owner != 1 {
		t.Errorf("QueryOwnership = (%v, %v), want (1, true) — acquire must stay pending", owner, owned)
	}
	if n := len(outbox.SentTo(2)); n != 0 {
		t.Errorf("acquirer received %d events, want 0 (request queued)", n)
	}

	// The queue still resolves: owner divests-if-wanted → transfer.
	if err := mgr.DivestIfWanted(ctx, ownFed, 1, obj, attrs); err != nil {
		t.Fatalf("DivestIfWanted: %v", err)
	}
	if owner, owned := mgr.QueryOwnership(ownFed, obj, attrs[0]); !owned || owner != 2 {
		t.Errorf("post-divest QueryOwnership = (%v, %v), want (2, true)", owner, owned)
	}
}

// TestSpec_M37_Acquire_UnownedAttributes_NoReleaseRequest: acquiring
// unowned attributes transfers immediately (M17.27) — nobody hears a
// §7.11 release request.
func TestSpec_M37_Acquire_UnownedAttributes_NoReleaseRequest(t *testing.T) {
	mgr, outbox := newOwnershipStack(t)

	if err := mgr.Acquire(context.Background(), ownFed, 2, core.ObjectHandle(7),
		[]core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if got := len(outbox.Sent()); got != 1 {
		t.Fatalf("total events = %d, want 1 (acquired notification only); got %+v", got, outbox.Sent())
	}
	// Acquirer gets exactly the acquisition notification — no §7.11
	// release request to anyone.
	evts2 := outbox.SentTo(2)
	if len(evts2) != 1 || evts2[0].GetOwnershipAcquired() == nil {
		t.Errorf("acquirer events = %+v, want [OwnershipAcquired]", evts2)
	}
}
