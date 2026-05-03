package m9spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	savepkg "github.com/cbchoi/gorti/rti/internal/savepoint"
)

func newTestSaveManager(t *testing.T) (*savepkg.Manager, *fakeOutbox) {
	t.Helper()
	outbox := newFakeOutbox()
	mgr, err := savepkg.New(savepkg.Options{
		Outbox:      outbox,
		EventLog:    newPermissiveEventLog(),
		BundleStore: newInMemStorage(),
	})
	if err != nil && !errors.Is(err, savepkg.ErrNotImplemented) {
		t.Logf("savepoint.New returned: %v (expected during pre-dispatch)", err)
	}
	return mgr, outbox
}

// TestSpec_M9_RequestSave_TransitionsToInitiated: a fresh save request
// transitions the federation to StateInitiated and broadcasts
// initiateFederateSave to the outbox.
//
// Implements: FR-SR-1.
func TestSpec_M9_RequestSave_TransitionsToInitiated(t *testing.T) {
	mgr, outbox := newTestSaveManager(t)
	if mgr == nil {
		t.Skip("savepoint.Manager not yet wired (M9 RED state)")
	}
	preCount := len(outbox.Sent())
	err := mgr.RequestFederationSave(context.Background(), "fed", "save-1", nil)
	if errors.Is(err, savepkg.ErrNotImplemented) {
		t.Skip("RequestFederationSave not yet implemented")
	}
	if err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if mgr.QuerySaveState("fed", "save-1") != savepkg.StateInitiated {
		t.Errorf("QuerySaveState = %v, want StateInitiated", mgr.QuerySaveState("fed", "save-1"))
	}
	if len(outbox.Sent()) <= preCount {
		t.Errorf("expected initiateFederateSave broadcasts; outbox grew by %d", len(outbox.Sent())-preCount)
	}
}

// TestSpec_M9_AllFederatesComplete_EmitsFederationSaved: when all joined
// federates call FederateSaveComplete, the manager transitions to
// StateSaved and emits federationSaved.
//
// SCAFFOLD — depends on a federation-membership accessor being wired
// (similar to M8 sync's Members callback). Agent A wires both in M9 W1.
//
// Implements: FR-SR-2.
func TestSpec_M9_AllFederatesComplete_EmitsFederationSaved(t *testing.T) {
	mgr, _ := newTestSaveManager(t)
	if mgr == nil {
		t.Skip("savepoint.Manager not yet wired")
	}
	t.Skip("Agent A wires the membership-aware aggregation in M9 W1")
}

// TestSpec_M9_AnyFederateFails_EmitsFederationNotSaved: if any joined
// federate calls FederateSaveNotComplete, the save closes out as
// federationNotSaved (FR-SR-2). The save bundle is NOT written.
//
// SCAFFOLD.
//
// Implements: FR-SR-2.
func TestSpec_M9_AnyFederateFails_EmitsFederationNotSaved(t *testing.T) {
	mgr, _ := newTestSaveManager(t)
	if mgr == nil {
		t.Skip("savepoint.Manager not yet wired")
	}
	t.Skip("Agent A wires the failure-aggregation path in M9 W1")
}

// TestSpec_M9_RequestSave_TwiceRejected: a second save request while
// the first is in progress returns ErrSaveAlreadyInProgress.
//
// Implements: FR-SR-1.
func TestSpec_M9_RequestSave_TwiceRejected(t *testing.T) {
	mgr, _ := newTestSaveManager(t)
	if mgr == nil {
		t.Skip("savepoint.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "save-1", nil); err != nil {
		if errors.Is(err, savepkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("first RequestFederationSave: %v", err)
	}
	err := mgr.RequestFederationSave(ctx, "fed", "save-2", nil)
	if !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		t.Errorf("second RequestFederationSave: err = %v, want ErrSaveAlreadyInProgress", err)
	}
}

// TestSpec_M9_RequestRestore_BundleNotFound: restoring an unsaved
// label returns savepoint.ErrSaveBundleNotFound.
//
// Implements: FR-SR-3.
func TestSpec_M9_RequestRestore_BundleNotFound(t *testing.T) {
	mgr, _ := newTestSaveManager(t)
	if mgr == nil {
		t.Skip("savepoint.Manager not yet wired")
	}
	err := mgr.RequestFederationRestore(context.Background(), "fed", "no-such-label")
	if errors.Is(err, savepkg.ErrNotImplemented) {
		t.Skip()
	}
	if !errors.Is(err, savepkg.ErrSaveBundleNotFound) {
		t.Errorf("Restore unsaved: err = %v, want ErrSaveBundleNotFound", err)
	}
}

// TestSpec_M9_RoundTrip_SaveThenRestore: save a federation, restore it,
// assert the restored state is byte-identical to the saved state.
//
// SCAFFOLD — the full round-trip needs a federation manager + event log
// to be wired into the save manager. Agent A wires this in M9 W1.
//
// Implements: FR-SR-3, FR-SR-5.
func TestSpec_M9_RoundTrip_SaveThenRestore(t *testing.T) {
	mgr, _ := newTestSaveManager(t)
	if mgr == nil {
		t.Skip("savepoint.Manager not yet wired")
	}
	t.Skip("Agent A wires the round-trip test in M9 W1 (depends on full save bundle format + event-log replay integration)")
}
