package savepoint

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSaveRecordsTimeImplementation(t *testing.T) {
	store := newMemStore()
	generation := uint64(7)
	opts := strictOptions(store, &generation, nil)
	opts.LogicalTimeImplementation = func(core.FederationName) (string, bool) { return testHLAfloat64Time, true }
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	completeSave(t, m, "time-selected")
	manifest, err := m.LoadManifest("fed", "time-selected")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.LogicalTimeImplementationName != testHLAfloat64Time {
		t.Fatalf("manifest time=%q", manifest.LogicalTimeImplementationName)
	}
}

func TestRestoreTimeImplementationCheckedBeforeApplyingSnapshots(t *testing.T) {
	for _, saved := range []string{"", testHLAfloat64Time, "HLAinteger64Time", "custom", " HLAfloat64Time"} {
		t.Run("saved-"+saved, func(t *testing.T) {
			store := newMemStore()
			snapshotter := &fakeSnapshotter{marshalled: []byte("saved")}
			m, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: store, ManagerSnapshots: map[string]ManagerSnapshotter{"sync": snapshotter}})
			if err != nil {
				t.Fatal(err)
			}
			writer, err := store.Writer("fed", "time-selected")
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteBundle(writer, Manifest{Version: 1, Federation: "fed", Label: "time-selected", LogicalTimeImplementationName: saved, Federates: []core.FederateHandle{1}, ManagerSnapshots: map[string][]byte{"sync": []byte("saved")}}, nil); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			err = m.RequestFederationRestore(context.Background(), "fed", "time-selected")
			valid := saved == "" || saved == testHLAfloat64Time
			if valid {
				if err != nil || len(snapshotter.unmarshaled) != 1 {
					t.Fatalf("compatible Float64 snapshot rejected: %v, calls=%d", err, len(snapshotter.unmarshaled))
				}
			} else if !errors.Is(err, ErrSaveBundleIncompatible) || len(snapshotter.unmarshaled) != 0 {
				t.Fatalf("incompatible representation applied: %v, calls=%d", err, len(snapshotter.unmarshaled))
			}
		})
	}
}

func TestTimeMetadataResolverFailureCannotPublishSave(t *testing.T) {
	store := newMemStore()
	m, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: store, LogicalTimeImplementation: func(core.FederationName) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	completeSave(t, m, "missing")
	if store.Exists("fed", "missing") {
		t.Fatal("save succeeded with unresolved time representation")
	}
}
