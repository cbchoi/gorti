package mom

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// snapFakeOutbox is a no-op outbox.
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
	ctx := context.Background()
	if err := mgr.FederationCreated(ctx, "demo", nil); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := mgr.FederateJoined(ctx, "demo", 1, "alpha", ""); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	mgr.IncrementUpdatesSent("demo", 1)
	mgr.IncrementUpdatesSent("demo", 1)
	mgr.IncrementInteractionsSent("demo", 1)

	snap := mgr.Snapshot("demo")
	if got := len(snap.PerFederate); got != 1 {
		t.Fatalf("PerFederate len = %d, want 1", got)
	}
	c := snap.PerFederate[1]
	if c.UpdatesSent != 2 {
		t.Errorf("UpdatesSent = %d, want 2", c.UpdatesSent)
	}
	if c.InteractionsSent != 1 {
		t.Errorf("InteractionsSent = %d, want 1", c.InteractionsSent)
	}
}

func TestManager_Snapshot_UnknownFederation_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := mgr.Snapshot("nope")
	if len(snap.PerFederate) != 0 {
		t.Errorf("PerFederate = %v, want empty", snap.PerFederate)
	}
}
