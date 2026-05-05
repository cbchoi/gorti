package savepoint

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestManager_Snapshot_IdleByDefault(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := mgr.Snapshot("demo")
	if snap.SaveState != core.SaveStateIdle {
		t.Errorf("SaveState idle = %v, want SaveStateIdle", snap.SaveState)
	}
	if snap.RestoreState != core.SaveRestoreIdle {
		t.Errorf("RestoreState idle = %v, want SaveRestoreIdle", snap.RestoreState)
	}
	if snap.SaveLabel != "" || snap.RestoreLabel != "" {
		t.Errorf("Labels = (%q,%q), want empty", snap.SaveLabel, snap.RestoreLabel)
	}
}

func TestManager_Snapshot_InitiatedSaveReports(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.RequestFederationSave(context.Background(), "demo", "checkpoint", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	snap := mgr.Snapshot("demo")
	if snap.SaveLabel != "checkpoint" {
		t.Errorf("SaveLabel = %q, want checkpoint", snap.SaveLabel)
	}
	if snap.SaveState != core.SaveStateInitiated {
		t.Errorf("SaveState = %v, want SaveStateInitiated", snap.SaveState)
	}
}
