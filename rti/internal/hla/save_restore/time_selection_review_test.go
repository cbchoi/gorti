package savepoint

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestReviewTimeSelectionBundleRetainsExactMetadata(t *testing.T) {
	for _, name := range []string{"", testHLAfloat64Time, "HLAinteger64Time", "custom", " HLAfloat64Time"} {
		t.Run(name, func(t *testing.T) {
			var buffer bytes.Buffer
			original := Manifest{Federation: "fed", Label: "selection", LogicalTimeImplementationName: name}
			if err := WriteBundle(&buffer, original, nil); err != nil {
				t.Fatal(err)
			}
			decoded, _, err := ReadBundle(&buffer)
			if err != nil || decoded.LogicalTimeImplementationName != name {
				t.Fatalf("persisted selection changed: got=%q want=%q err=%v", decoded.LogicalTimeImplementationName, name, err)
			}
		})
	}
}

func TestReviewTimeSelectionRestoreRejectsBeforePreparation(t *testing.T) {
	for _, generationAware := range []bool{false, true} {
		for _, saved := range []string{"HLAinteger64Time", "custom", "HLAfloat64Time "} {
			t.Run(saved+map[bool]string{false: "/legacy", true: "/generation"}[generationAware], func(t *testing.T) {
				store := newMemStore()
				generation := uint64(4)
				order := []string{}
				snapshot := &transitionSnapshot{live: testLive, key: "state", order: &order}
				out := &transitionOutbox{}
				opts := Options{
					Outbox: out, BundleStore: store,
					Members:           func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
					RequiredSnapshots: []string{"state"},
					ManagerSnapshots:  map[string]ManagerSnapshotter{"state": snapshot},
				}
				manifest := Manifest{Version: 1, Federation: "fed", Label: "selection", LogicalTimeImplementationName: saved, Federates: []core.FederateHandle{1}, ManagerSnapshots: map[string][]byte{"state": []byte("saved")}}
				if generationAware {
					opts.Generation = func(core.FederationName) (uint64, bool) { return generation, true }
					opts.LogicalTimeImplementation = func(core.FederationName) (string, bool) { return testHLAfloat64Time, true }
					manifest.Version, manifest.FederationGeneration = 2, &generation
				}
				m, err := New(opts)
				if err != nil {
					t.Fatal(err)
				}
				writer, err := m.bundleWriter("fed", "selection", manifest.FederationGeneration)
				if err != nil {
					t.Fatal(err)
				}
				if err := WriteBundle(writer, manifest, nil); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				err = m.RequestFederationRestore(context.Background(), "fed", "selection")
				if !errors.Is(err, ErrSaveBundleIncompatible) || len(order) != 0 || snapshot.live != testLive {
					t.Fatalf("incompatible time reached preparation: err=%v order=%v live=%q", err, order, snapshot.live)
				}
				if sent := out.Sent(); len(sent) != 0 {
					t.Fatalf("incompatible time initiated restore callbacks: %+v", sent)
				}
			})
		}
	}
}

func TestReviewTimeSelectionRestoreRevalidatesAtCommit(t *testing.T) {
	for _, changed := range []string{"HLAinteger64Time", "", "missing"} {
		t.Run(changed, func(t *testing.T) {
			liveName, found := testHLAfloat64Time, true
			snapshot := &transitionSnapshot{live: "saved"}
			m, err := New(Options{
				Outbox: &transitionOutbox{}, BundleStore: newMemStore(),
				Members:                   func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
				LogicalTimeImplementation: func(core.FederationName) (string, bool) { return liveName, found },
				RequiredSnapshots:         []string{"state"},
				ManagerSnapshots:          map[string]ManagerSnapshotter{"state": snapshot},
			})
			if err != nil {
				t.Fatal(err)
			}
			completeSave(t, m, "selection")
			snapshot.live = testLive
			if err := m.RequestFederationRestore(context.Background(), "fed", "selection"); err != nil {
				t.Fatal(err)
			}
			liveName = changed
			found = changed != "missing"
			err = m.FederateRestoreComplete(context.Background(), "fed", 1)
			if !errors.Is(err, ErrSaveBundleIncompatible) || snapshot.live != testLive {
				t.Fatalf("changed time selection committed: live=%q err=%v", snapshot.live, err)
			}
		})
	}
}

func TestReviewCrossGenerationCannotRelabelTimeSelection(t *testing.T) {
	for _, reboundName := range []string{"", "HLAinteger64Time", testHLAfloat64Time} {
		t.Run(reboundName, func(t *testing.T) {
			generation := uint64(1)
			manifest := Manifest{Version: 2, Federation: "fed", FederationGeneration: &generation, Label: "selection", LogicalTimeImplementationName: testHLAfloat64Time, ManagerSnapshots: map[string][]byte{"fom": []byte("saved")}}
			m := &Manager{opts: Options{
				Members:                  func(core.FederationName) []core.FederateHandle { return nil },
				Roster:                   func(core.FederationName) []core.FederationMember { return nil },
				RequiredSnapshots:        []string{"fom"},
				ValidateRestoreSnapshots: func(core.FederationName, Manifest, core.FOMHandle) error { return nil },
				PrepareRestoreTransition: func(context.Context, core.FederationName, []RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
					t.Fatal("unexpected identity publication")
					return nil, nil
				},
				RebindRestore: func(_ context.Context, _ core.FederationName, target uint64, in Manifest) (Manifest, error) {
					in.FederationGeneration = &target
					in.LogicalTimeImplementationName = reboundName
					return in, nil
				},
			}}
			err := m.prepareCrossGeneration(context.Background(), "fed", 2, "digest", &manifest, &activeRestore{})
			if reboundName == testHLAfloat64Time {
				if err != nil || *manifest.FederationGeneration != 2 {
					t.Fatalf("valid rebind: %v", err)
				}
			} else if !errors.Is(err, ErrSaveBundleIncompatible) || *manifest.FederationGeneration != 1 || manifest.LogicalTimeImplementationName != testHLAfloat64Time {
				t.Fatalf("rebinder relabeled time metadata: %+v err=%v", manifest, err)
			}
		})
	}
}

const testHLAfloat64Time = "HLAfloat64Time"
