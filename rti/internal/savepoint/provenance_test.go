package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

const testFOMDigest = "4edc1b9a3f3f63062d7f63b29e50b9faca4d15fcd266bcb854a53e0c31f183de"

func strictOptions(store *memStore, generation *uint64, snapshotter ManagerSnapshotter) Options {
	opts := Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		Generation:  func(core.FederationName) (uint64, bool) { return *generation, true },
		FOMSHA256:   func(core.FederationName) (string, bool) { return testFOMDigest, true },
	}
	if snapshotter != nil {
		opts.ManagerSnapshots = map[string]ManagerSnapshotter{ManagerSnapshotKeySync: snapshotter}
	}
	return opts
}

func completeSave(t *testing.T, mgr *Manager, label string) {
	t.Helper()
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", label, nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}
}

func TestSaveBundleProvenanceSameGenerationRestore(t *testing.T) {
	store := newMemStore()
	generation := uint64(7)
	snapshotter := &fakeSnapshotter{marshalled: []byte("generation-seven")}
	mgr, err := New(strictOptions(store, &generation, snapshotter))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	completeSave(t, mgr, "checkpoint")

	manifest, err := mgr.LoadManifest("fed", "checkpoint")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.Version != bundleFormatVersionV2 {
		t.Fatalf("Version = %d, want %d", manifest.Version, bundleFormatVersionV2)
	}
	if manifest.FederationGeneration == nil || *manifest.FederationGeneration != 7 {
		t.Fatalf("FederationGeneration = %v, want 7", manifest.FederationGeneration)
	}
	if manifest.FOMSHA256 != testFOMDigest {
		t.Fatalf("FOMSHA256 = %q, want %q", manifest.FOMSHA256, testFOMDigest)
	}

	snapshotter.unmarshaled = nil
	if err := mgr.RequestFederationRestore(context.Background(), "fed", "checkpoint"); err != nil {
		t.Fatalf("same-generation restore: %v", err)
	}
	if len(snapshotter.unmarshaled) != 1 {
		t.Fatalf("Unmarshal calls = %d, want 1", len(snapshotter.unmarshaled))
	}
}

func TestSaveBundleProvenanceRejectsCrossGenerationBeforeUnmarshal(t *testing.T) {
	store := newMemStore()
	generation := uint64(7)
	snapshotter := &fakeSnapshotter{marshalled: []byte("generation-seven")}
	saver, err := New(strictOptions(store, &generation, snapshotter))
	if err != nil {
		t.Fatalf("New saver: %v", err)
	}
	completeSave(t, saver, "checkpoint")

	generation = 8
	snapshotter.unmarshaled = nil
	restorer, err := New(strictOptions(store, &generation, snapshotter))
	if err != nil {
		t.Fatalf("New restorer: %v", err)
	}
	err = restorer.RequestFederationRestore(context.Background(), "fed", "checkpoint")
	if !errors.Is(err, ErrSaveBundleNotFound) {
		t.Fatalf("cross-generation restore error = %v, want ErrSaveBundleNotFound", err)
	}
	if len(snapshotter.unmarshaled) != 0 {
		t.Fatalf("Unmarshal calls = %d, want 0", len(snapshotter.unmarshaled))
	}
	if got := restorer.QueryRestoreState("fed", "checkpoint"); got != RestoreIdle {
		t.Fatalf("restore state = %v, want idle after generation rejection", got)
	}
}

func TestSaveCapturesGenerationAtRequest(t *testing.T) {
	store := newMemStore()
	generation := uint64(12)
	mgr, err := New(strictOptions(store, &generation, nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, "fed", "checkpoint", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	generation = 13
	if err := mgr.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatalf("FederateSaveComplete: %v", err)
	}

	key12 := StorageKey{Federation: "fed", Generation: 12, Label: "checkpoint"}
	key13 := StorageKey{Federation: "fed", Generation: 13, Label: "checkpoint"}
	if !store.ExistsFor(key12) || store.ExistsFor(key13) {
		t.Fatalf("published keys: generation 12=%v generation 13=%v, want true/false",
			store.ExistsFor(key12), store.ExistsFor(key13))
	}
	rdr, err := store.ReaderFor(key12)
	if err != nil {
		t.Fatalf("ReaderFor generation 12: %v", err)
	}
	manifest, _, err := ReadBundle(rdr)
	_ = rdr.Close()
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if manifest.FederationGeneration == nil || *manifest.FederationGeneration != 12 {
		t.Fatalf("manifest generation = %v, want 12", manifest.FederationGeneration)
	}
}

func TestSaveBundleSameLabelCoexistsAcrossGenerations(t *testing.T) {
	store := newMemStore()
	generation := uint64(21)
	snapshotter := &fakeSnapshotter{marshalled: []byte("generation-21")}
	mgr, err := New(strictOptions(store, &generation, snapshotter))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	completeSave(t, mgr, "same-label")

	generation = 22
	snapshotter.marshalled = []byte("generation-22")
	completeSave(t, mgr, "same-label")

	key21 := StorageKey{Federation: "fed", Generation: 21, Label: "same-label"}
	key22 := StorageKey{Federation: "fed", Generation: 22, Label: "same-label"}
	if !store.ExistsFor(key21) || !store.ExistsFor(key22) {
		t.Fatalf("same-label bundles did not coexist across generations")
	}

	snapshotter.unmarshaled = nil
	if err := mgr.RequestFederationRestore(context.Background(), "fed", "same-label"); err != nil {
		t.Fatalf("RequestFederationRestore generation 22: %v", err)
	}
	if len(snapshotter.unmarshaled) != 1 || string(snapshotter.unmarshaled[0]) != "generation-22" {
		t.Fatalf("restored snapshots = %q, want generation-22", snapshotter.unmarshaled)
	}
}

func TestSaveBundleProvenanceRejectsFOMMismatchBeforeUnmarshal(t *testing.T) {
	store := newMemStore()
	generation := uint64(3)
	snapshotter := &fakeSnapshotter{marshalled: []byte("snapshot")}
	opts := strictOptions(store, &generation, snapshotter)
	saver, err := New(opts)
	if err != nil {
		t.Fatalf("New saver: %v", err)
	}
	completeSave(t, saver, "checkpoint")

	opts.FOMSHA256 = func(core.FederationName) (string, bool) {
		return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", true
	}
	snapshotter.unmarshaled = nil
	restorer, err := New(opts)
	if err != nil {
		t.Fatalf("New restorer: %v", err)
	}
	err = restorer.RequestFederationRestore(context.Background(), "fed", "checkpoint")
	if !errors.Is(err, ErrSaveBundleIncompatible) {
		t.Fatalf("restore error = %v, want ErrSaveBundleIncompatible", err)
	}
	if len(snapshotter.unmarshaled) != 0 {
		t.Fatalf("Unmarshal calls = %d, want 0", len(snapshotter.unmarshaled))
	}
}

func TestSaveBundleProvenanceRejectsLegacyBundleWhenStrict(t *testing.T) {
	store := newMemStore()
	key := StorageKey{Federation: "fed", Generation: 9, Label: "legacy"}
	w, err := store.WriterFor(key)
	if err != nil {
		t.Fatalf("WriterFor: %v", err)
	}
	if err := WriteBundle(w, Manifest{
		Federation: "fed",
		Label:      "legacy",
		Federates:  []core.FederateHandle{1},
	}, nil); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	generation := uint64(9)
	mgr, err := New(strictOptions(store, &generation, nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = mgr.RequestFederationRestore(context.Background(), "fed", "legacy")
	if !errors.Is(err, ErrSaveBundleIncompatible) {
		t.Fatalf("restore error = %v, want ErrSaveBundleIncompatible", err)
	}
}
