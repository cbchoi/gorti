package sync

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeOutbox for snapshot tests — discards events.
type snapFakeOutbox struct{}

func (snapFakeOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

func TestManager_Snapshot_AnnouncedAndAchieved(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.Register(ctx, "demo", "start", []byte("tag"), []core.FederateHandle{1, 2, 3}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := mgr.Achieve(ctx, "demo", 1, "start"); err != nil {
		t.Fatalf("Achieve: %v", err)
	}
	snap := mgr.Snapshot("demo")
	if got := len(snap); got != 1 {
		t.Fatalf("Snapshot len = %d, want 1", got)
	}
	sp := snap[0]
	if sp.Label != "start" {
		t.Errorf("Label = %q, want start", sp.Label)
	}
	if sp.State != core.SyncPointStateAnnounced {
		t.Errorf("State = %v, want Announced", sp.State)
	}
	if !reflect.DeepEqual(sp.RequiredHandles, []core.FederateHandle{1, 2, 3}) {
		t.Errorf("RequiredHandles = %v, want [1 2 3]", sp.RequiredHandles)
	}
	if !reflect.DeepEqual(sp.AchievedHandles, []core.FederateHandle{1}) {
		t.Errorf("AchievedHandles = %v, want [1]", sp.AchievedHandles)
	}
}

func TestManager_Snapshot_UnknownFederation_ReturnsNil(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := mgr.Snapshot("nope"); got != nil {
		t.Errorf("Snapshot unknown = %v, want nil", got)
	}
}
