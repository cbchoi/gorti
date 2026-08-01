package m8spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/sync"
)

// newTestSyncManager builds a sync.Manager with fake outbox + permissive
// event log. Returns nil on stub state (sync.New currently returns
// ErrNotImplemented) so tests skip cleanly until M8 W1 lands.
func newTestSyncManager(t *testing.T) (*syncpkg.Manager, *fakeOutbox, *permissiveEventLog) {
	t.Helper()
	outbox := newFakeOutbox()
	log := newPermissiveEventLog()
	mgr, err := syncpkg.New(syncpkg.Options{
		Outbox:   outbox,
		EventLog: log,
	})
	if err != nil {
		t.Logf("sync.New returned: %v (expected during pre-dispatch)", err)
	}
	return mgr, outbox, log
}

// TestSpec_M8_Sync_Register_Happy: a fresh sync point can be registered.
//
// Implements: FR-SYN-1.
func TestSpec_M8_Sync_Register_Happy(t *testing.T) {
	mgr, _, _ := newTestSyncManager(t)
	if mgr == nil {
		t.Skip("sync.Manager not yet wired (M8 RED state)")
	}
	err := mgr.Register(context.Background(), "fed", "phase1", []byte("tag"), nil)
	if errors.Is(err, syncpkg.ErrNotImplemented) {
		t.Skip("Register not yet implemented")
	}
	if err != nil {
		t.Errorf("Register: %v", err)
	}
}

// TestSpec_M8_Sync_Register_Twice: re-registering the same label
// returns ErrSyncPointAlreadyRegistered.
//
// Implements: FR-SYN-1.
func TestSpec_M8_Sync_Register_Twice(t *testing.T) {
	mgr, _, _ := newTestSyncManager(t)
	if mgr == nil {
		t.Skip("sync.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.Register(ctx, "fed", "phase1", nil, nil); err != nil {
		if errors.Is(err, syncpkg.ErrNotImplemented) {
			t.Skip("Register not yet implemented")
		}
		t.Fatalf("first Register: %v", err)
	}
	err := mgr.Register(ctx, "fed", "phase1", nil, nil)
	if !errors.Is(err, core.ErrSyncPointAlreadyRegistered) {
		t.Errorf("re-register: err = %v, want ErrSyncPointAlreadyRegistered", err)
	}
}

// TestSpec_M8_Sync_AchieveAll_FiresFederationSynchronized: when all
// required federates have called Achieve, the RTI emits
// federationSynchronized to all of them. With nil requiredFederates
// (= all joined) and 2 joined federates, both must achieve before
// the synchronized event fires.
//
// Implements: FR-SYN-2, FR-SYN-3.
func TestSpec_M8_Sync_AchieveAll_FiresFederationSynchronized(t *testing.T) {
	mgr, outbox, _ := newTestSyncManager(t)
	if mgr == nil {
		t.Skip("sync.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.Register(ctx, "fed", "phase1", nil, []core.FederateHandle{1, 2}); err != nil {
		if errors.Is(err, syncpkg.ErrNotImplemented) {
			t.Skip("Register not yet implemented")
		}
		t.Fatalf("Register: %v", err)
	}

	preCount := len(outbox.Sent())

	// Federate 1 achieves; not yet synchronized.
	if err := mgr.Achieve(ctx, "fed", 1, "phase1"); err != nil {
		t.Fatalf("Achieve 1: %v", err)
	}
	// Federate 2 achieves; now ALL have achieved → federationSynchronized.
	if err := mgr.Achieve(ctx, "fed", 2, "phase1"); err != nil {
		t.Fatalf("Achieve 2: %v", err)
	}

	if mgr.QueryState("fed", "phase1") != syncpkg.StateAchieved {
		t.Errorf("QueryState after both achieved = %v, want StateAchieved", mgr.QueryState("fed", "phase1"))
	}

	// Outbox should have grown with at least 2 federationSynchronized
	// events (one per federate).
	if len(outbox.Sent()) <= preCount {
		t.Errorf("expected outbox emissions after all achieved; got %d (was %d)",
			len(outbox.Sent()), preCount)
	}
}

// TestSpec_M8_Sync_Achieve_TwiceRejected: a federate that's already
// achieved cannot achieve again.
//
// Implements: FR-SYN-2.
func TestSpec_M8_Sync_Achieve_TwiceRejected(t *testing.T) {
	mgr, _, _ := newTestSyncManager(t)
	if mgr == nil {
		t.Skip("sync.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.Register(ctx, "fed", "phase1", nil, []core.FederateHandle{1, 2}); err != nil {
		if errors.Is(err, syncpkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("Register: %v", err)
	}
	if err := mgr.Achieve(ctx, "fed", 1, "phase1"); err != nil {
		t.Fatalf("first Achieve: %v", err)
	}
	err := mgr.Achieve(ctx, "fed", 1, "phase1")
	if !errors.Is(err, core.ErrSyncPointAlreadyAchieved) {
		t.Errorf("second Achieve: err = %v, want ErrSyncPointAlreadyAchieved", err)
	}
}

// TestSpec_M8_Sync_Achieve_UnregisteredRejected: achieving an
// unregistered label returns ErrSyncPointNotRegistered.
//
// Implements: FR-SYN-2.
func TestSpec_M8_Sync_Achieve_UnregisteredRejected(t *testing.T) {
	mgr, _, _ := newTestSyncManager(t)
	if mgr == nil {
		t.Skip("sync.Manager not yet wired")
	}
	err := mgr.Achieve(context.Background(), "fed", 1, "no-such-label")
	if errors.Is(err, syncpkg.ErrNotImplemented) {
		t.Skip("Achieve not yet implemented (M8 RED state)")
	}
	if !errors.Is(err, core.ErrSyncPointNotRegistered) {
		t.Errorf("Achieve unregistered: err = %v, want ErrSyncPointNotRegistered", err)
	}
}
