package ownership

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// snapFakeOutbox is a no-op outbox used in snapshot tests.
type snapFakeOutbox struct{}

func (snapFakeOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

func TestManager_Snapshot_RecordsCounters(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mgr.RegisterInitialOwnership("demo", 1, 100, []core.AttributeHandle{1, 2, 3})
	mgr.RegisterInitialOwnership("demo", 2, 200, []core.AttributeHandle{1})

	snap := mgr.Snapshot("demo")
	if snap.OwnedAttributesCount != 4 {
		t.Errorf("OwnedAttributesCount = %d, want 4", snap.OwnedAttributesCount)
	}
	if snap.PendingDivestsCount != 0 {
		t.Errorf("PendingDivestsCount = %d, want 0", snap.PendingDivestsCount)
	}
	if snap.PendingAcquiresCount != 0 {
		t.Errorf("PendingAcquiresCount = %d, want 0", snap.PendingAcquiresCount)
	}

	// Trigger a NegotiatedDivest to populate pendingDivests.
	if err := mgr.NegotiatedDivest(context.Background(), "demo", 1, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	snap = mgr.Snapshot("demo")
	if snap.PendingDivestsCount != 1 {
		t.Errorf("PendingDivestsCount after divest = %d, want 1", snap.PendingDivestsCount)
	}
}

func TestManager_Snapshot_UnknownFederation_ReturnsZero(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := mgr.Snapshot("nope")
	if snap.OwnedAttributesCount != 0 || snap.PendingDivestsCount != 0 || snap.PendingAcquiresCount != 0 {
		t.Errorf("Snapshot unknown = %+v, want zero", snap)
	}
}
