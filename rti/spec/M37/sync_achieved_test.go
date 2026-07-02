package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M37_SyncAchieved_UnsuccessfulFederateReported: IEEE
// 1516.1-2010 §4.14/§4.15 — a federate achieving with
// successfully=false still counts toward completion, and every
// recipient's federationSynchronized carries it in failed_to_sync.
func TestSpec_M37_SyncAchieved_UnsuccessfulFederateReported(t *testing.T) {
	mgr, outbox := newSyncStack(t)
	ctx := context.Background()

	if err := mgr.RegisterBy(ctx, syncFed, 1, "gate", nil, nil); err != nil {
		t.Fatalf("RegisterBy: %v", err)
	}
	outbox.Reset()

	if err := mgr.AchieveWith(ctx, syncFed, 1, "gate", true); err != nil {
		t.Fatalf("AchieveWith(1, true): %v", err)
	}
	if err := mgr.AchieveWith(ctx, syncFed, 2, "gate", false); err != nil {
		t.Fatalf("AchieveWith(2, false): %v", err)
	}

	for _, h := range []core.FederateHandle{1, 2} {
		evts := outbox.SentTo(h)
		if len(evts) != 1 {
			t.Fatalf("federate %d events = %d, want 1 (synchronized); got %+v", h, len(evts), evts)
		}
		synced := evts[0].GetSyncSynchronized()
		if synced == nil {
			t.Fatalf("federate %d event = %+v, want FederationSynchronized", h, evts[0])
		}
		if synced.GetLabel() != "gate" {
			t.Errorf("federate %d synchronized.label = %q, want %q", h, synced.GetLabel(), "gate")
		}
		failed := synced.GetFailedToSync()
		if len(failed) != 1 || failed[0] != 2 {
			t.Errorf("federate %d failed_to_sync = %v, want [2]", h, failed)
		}
	}
}

// TestSpec_M37_SyncAchieved_AllSuccessful_EmptyFailedSet: the legacy
// Achieve path (and successfully=true) leaves failed_to_sync empty —
// wire-identical to pre-M37 for old clients.
func TestSpec_M37_SyncAchieved_AllSuccessful_EmptyFailedSet(t *testing.T) {
	mgr, outbox := newSyncStack(t)
	ctx := context.Background()

	if err := mgr.RegisterBy(ctx, syncFed, 1, "gate", nil, nil); err != nil {
		t.Fatalf("RegisterBy: %v", err)
	}
	outbox.Reset()

	if err := mgr.Achieve(ctx, syncFed, 1, "gate"); err != nil {
		t.Fatalf("Achieve(1): %v", err)
	}
	if err := mgr.AchieveWith(ctx, syncFed, 2, "gate", true); err != nil {
		t.Fatalf("AchieveWith(2, true): %v", err)
	}

	evts := outbox.SentTo(1)
	if len(evts) != 1 || evts[0].GetSyncSynchronized() == nil {
		t.Fatalf("events = %+v, want [synchronized]", evts)
	}
	if failed := evts[0].GetSyncSynchronized().GetFailedToSync(); len(failed) != 0 {
		t.Errorf("failed_to_sync = %v, want empty", failed)
	}
}
