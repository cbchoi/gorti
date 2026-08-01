package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeSnapshotter is a minimal ManagerSnapshotter implementation that
// records every Marshal / Unmarshal call. Tests inject it under
// arbitrary keys to exercise the savepoint Manager's snapshot
// orchestration without dragging in the real sync / ownership / mom
// / ddm packages.
type fakeSnapshotter struct {
	name        string
	marshalErr  error
	marshalled  []byte
	unmarshaled [][]byte
}

func (f *fakeSnapshotter) Marshal(_ core.FederationName) ([]byte, error) {
	if f.marshalErr != nil {
		return nil, f.marshalErr
	}
	return append([]byte(nil), f.marshalled...), nil
}

func (f *fakeSnapshotter) Unmarshal(_ core.FederationName, data []byte) error {
	f.unmarshaled = append(f.unmarshaled, append([]byte(nil), data...))
	return nil
}

// TestSave_BundlesManagerSnapshots exercises M13 thread C
// (docs/srs.md §10.4): each registered ManagerSnapshotter's Marshal
// output lands in the manifest under its registered key.
func TestSave_BundlesManagerSnapshots(t *testing.T) {
	t.Parallel()
	syncSnap := &fakeSnapshotter{name: "sync", marshalled: []byte("sync-state-bytes")}
	ownSnap := &fakeSnapshotter{name: "ownership", marshalled: []byte("ownership-state-bytes")}
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		ManagerSnapshots: map[string]ManagerSnapshotter{
			ManagerSnapshotKeySync:      syncSnap,
			ManagerSnapshotKeyOwnership: ownSnap,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}
	manifest, err := mgr.LoadManifest("fed", "lbl")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if got := manifest.ManagerSnapshots[ManagerSnapshotKeySync]; string(got) != "sync-state-bytes" {
		t.Errorf("sync snapshot = %q, want %q", string(got), "sync-state-bytes")
	}
	if got := manifest.ManagerSnapshots[ManagerSnapshotKeyOwnership]; string(got) != "ownership-state-bytes" {
		t.Errorf("ownership snapshot = %q, want %q", string(got), "ownership-state-bytes")
	}
}

// TestRestore_AppliesManagerSnapshots verifies the symmetric path:
// each registered ManagerSnapshotter sees Unmarshal(fed, bytes) for
// the matching manifest key on RequestFederationRestore.
func TestRestore_AppliesManagerSnapshots(t *testing.T) {
	t.Parallel()
	syncSnap := &fakeSnapshotter{name: "sync", marshalled: []byte("sync-state-bytes")}
	momSnap := &fakeSnapshotter{name: "mom", marshalled: []byte("mom-state-bytes")}
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		ManagerSnapshots: map[string]ManagerSnapshotter{
			ManagerSnapshotKeySync: syncSnap,
			ManagerSnapshotKeyMOM:  momSnap,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}

	// Reset the per-snapshotter recorders so we observe only the
	// restore-path calls.
	syncSnap.unmarshaled = nil
	momSnap.unmarshaled = nil

	if err := mgr.RequestFederationRestore(ctx, "fed", "lbl"); err != nil {
		t.Fatalf("RequestFederationRestore: %v", err)
	}
	if len(syncSnap.unmarshaled) != 1 ||
		string(syncSnap.unmarshaled[0]) != "sync-state-bytes" {
		t.Errorf("sync Unmarshal = %v, want one call with sync-state-bytes", syncSnap.unmarshaled)
	}
	if len(momSnap.unmarshaled) != 1 ||
		string(momSnap.unmarshaled[0]) != "mom-state-bytes" {
		t.Errorf("mom Unmarshal = %v, want one call with mom-state-bytes", momSnap.unmarshaled)
	}
}

// TestSave_MarshalErrorFlipsToNotSaved asserts that any single
// snapshotter's Marshal error propagates as federationNotSaved —
// keeps a partially-snapshotted bundle from claiming saved-state.
func TestSave_MarshalErrorFlipsToNotSaved(t *testing.T) {
	t.Parallel()
	bad := &fakeSnapshotter{name: "sync", marshalErr: errors.New("boom")}
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		ManagerSnapshots: map[string]ManagerSnapshotter{
			ManagerSnapshotKeySync: bad,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}
	if got := mgr.QuerySaveState("fed", "lbl"); got != StateNotSaved {
		t.Errorf("QuerySaveState = %v, want StateNotSaved", got)
	}
}

// TestRestore_LegacyBundle_NoManagerSnapshots verifies that a bundle
// written without manager_snapshots (e.g. an old M9-era bundle that
// pre-dates M13) restores cleanly — applyManagerSnapshots is a
// silent no-op when manifest.ManagerSnapshots is nil.
func TestRestore_LegacyBundle_NoManagerSnapshots(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		// No ManagerSnapshots configured — pre-M13 production
		// configuration. The save bundle should still write +
		// restore cleanly.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "lbl", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}
	manifest, err := mgr.LoadManifest("fed", "lbl")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.ManagerSnapshots != nil {
		t.Errorf("ManagerSnapshots = %v, want nil for a no-snapshotter bundle",
			manifest.ManagerSnapshots)
	}
	if err := mgr.RequestFederationRestore(ctx, "fed", "lbl"); err != nil {
		t.Errorf("RequestFederationRestore on legacy bundle: %v", err)
	}
}
