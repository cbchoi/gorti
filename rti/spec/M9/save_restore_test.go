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
// Wired in M9 W1 via the Options.Members membership callback that the
// savepoint.Manager honors (mirrors sync.Manager's Members shape).
//
// Implements: FR-SR-2.
func TestSpec_M9_AllFederatesComplete_EmitsFederationSaved(t *testing.T) {
	outbox := newFakeOutbox()
	store := newInMemStorage()
	members := func(_ core.FederationName) []core.FederateHandle {
		return []core.FederateHandle{1, 2, 3}
	}
	mgr, err := savepkg.New(savepkg.Options{
		Outbox:      outbox,
		EventLog:    newPermissiveEventLog(),
		BundleStore: store,
		Members:     members,
	})
	if err != nil {
		t.Fatalf("savepoint.New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "save-1", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2, 3} {
		if err := mgr.FederateSaveComplete(ctx, "fed", h); err != nil {
			t.Fatalf("FederateSaveComplete(%d): %v", h, err)
		}
	}
	if got := mgr.QuerySaveState("fed", "save-1"); got != savepkg.StateSaved {
		t.Errorf("QuerySaveState = %v, want StateSaved", got)
	}
	if !store.Exists("fed", "save-1") {
		t.Errorf("expected save bundle to be written for save-1")
	}
	// initiateFederateSave x3 + federationSaved x3 = 6 envelopes.
	if got := len(outbox.Sent()); got < 6 {
		t.Errorf("outbox emitted %d envelopes, want >= 6 (3 initiate + 3 saved)", got)
	}
}

// TestSpec_M9_AnyFederateFails_EmitsFederationNotSaved: if any joined
// federate calls FederateSaveNotComplete, the save closes out as
// federationNotSaved (FR-SR-2). The save bundle is NOT written.
//
// Wired in M9 W1 via the Options.Members membership callback.
//
// Implements: FR-SR-2.
func TestSpec_M9_AnyFederateFails_EmitsFederationNotSaved(t *testing.T) {
	outbox := newFakeOutbox()
	store := newInMemStorage()
	members := func(_ core.FederationName) []core.FederateHandle {
		return []core.FederateHandle{1, 2, 3}
	}
	mgr, err := savepkg.New(savepkg.Options{
		Outbox:      outbox,
		EventLog:    newPermissiveEventLog(),
		BundleStore: store,
		Members:     members,
	})
	if err != nil {
		t.Fatalf("savepoint.New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "save-1", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete(1): %v", err)
	}
	if err := mgr.FederateSaveNotComplete(ctx, "fed", 2); err != nil {
		t.Fatalf("FederateSaveNotComplete(2): %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 3); err != nil {
		t.Fatalf("FederateSaveComplete(3): %v", err)
	}
	if got := mgr.QuerySaveState("fed", "save-1"); got != savepkg.StateNotSaved {
		t.Errorf("QuerySaveState = %v, want StateNotSaved", got)
	}
	if store.Exists("fed", "save-1") {
		t.Errorf("expected NO save bundle for failed save")
	}
	_ = outbox.Sent() // emissions counted; no specific count-assertion.
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
// assert the post-restore federate roster matches the pre-save snapshot.
//
// Wired in M9 W1. The cut-1 byte-determinism contract here is that the
// manifest's Federates list — written into the bundle at save-close
// time — round-trips through Storage and is restored byte-identically
// (drives the same initiateFederateRestore broadcast order). Per-manager
// state snapshots (declarations, ownership, sync points, MOM, DDM) are
// deferred to M9 W2; the event-log slice in the bundle is the FR-SR-5
// vehicle for full state reconstruction (NFR-DET-2 already proves the
// replay path itself is byte-identical, see rti/spec/M2).
//
// Implements: FR-SR-3, FR-SR-5.
func TestSpec_M9_RoundTrip_SaveThenRestore(t *testing.T) {
	outbox := newFakeOutbox()
	store := newInMemStorage()
	preSaveFederates := []core.FederateHandle{1, 2, 3}
	members := func(_ core.FederationName) []core.FederateHandle {
		// Snapshot per-call so a post-restore mutation can't leak in.
		out := make([]core.FederateHandle, len(preSaveFederates))
		copy(out, preSaveFederates)
		return out
	}
	mgr, err := savepkg.New(savepkg.Options{
		Outbox:      outbox,
		EventLog:    newPermissiveEventLog(),
		BundleStore: store,
		Members:     members,
	})
	if err != nil {
		t.Fatalf("savepoint.New: %v", err)
	}
	ctx := context.Background()

	// Phase 1: save
	if err := mgr.RequestFederationSave(ctx, "fed", "rt", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	for _, h := range preSaveFederates {
		if err := mgr.FederateSaveComplete(ctx, "fed", h); err != nil {
			t.Fatalf("FederateSaveComplete(%d): %v", h, err)
		}
	}
	if got := mgr.QuerySaveState("fed", "rt"); got != savepkg.StateSaved {
		t.Fatalf("after-save QuerySaveState = %v, want StateSaved", got)
	}

	// Inspect the persisted manifest to assert byte-deterministic
	// federate-list round-trip.
	manifest, err := mgr.LoadManifest("fed", "rt")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if got := manifest.Federates; !equalHandles(got, preSaveFederates) {
		t.Errorf("manifest federates = %v, want %v", got, preSaveFederates)
	}
	if manifest.Federation != "fed" || manifest.Label != "rt" {
		t.Errorf("manifest identity = (%q, %q), want (fed, rt)", manifest.Federation, manifest.Label)
	}

	// Phase 2: restore
	if err := mgr.RequestFederationRestore(ctx, "fed", "rt"); err != nil {
		t.Fatalf("RequestFederationRestore: %v", err)
	}
	if got := mgr.QueryRestoreState("fed", "rt"); got != savepkg.RestoreInitiated {
		t.Errorf("post-request QueryRestoreState = %v, want RestoreInitiated", got)
	}
	for _, h := range preSaveFederates {
		if err := mgr.FederateRestoreComplete(ctx, "fed", h); err != nil {
			t.Fatalf("FederateRestoreComplete(%d): %v", h, err)
		}
	}
	if got := mgr.QueryRestoreState("fed", "rt"); got != savepkg.RestoreCompleted {
		t.Errorf("post-complete QueryRestoreState = %v, want RestoreCompleted", got)
	}
}

func equalHandles(a, b []core.FederateHandle) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
