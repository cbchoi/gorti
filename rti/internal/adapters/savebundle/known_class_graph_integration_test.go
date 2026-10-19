package savebundle

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func knownClassGraphFixture(t *testing.T, tombstone bool) (core.CheckpointManifest, core.FOMHandle, *object.RegistryState) {
	t.Helper()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "known-class.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name><attribute><name>Common</name></attribute><objectClass><name>Child</name><attribute><name>Added</name></attribute><attribute><name>Another</name></attribute><objectClass><name>Leaf</name></objectClass></objectClass></objectClass><objectClass><name>Sibling</name></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := model.LookupObjectClass("HLAobjectRoot.Base.Child")
	known, _ := model.LookupObjectClass("HLAobjectRoot.Base")
	state := &object.RegistryState{LastObjectHandle: 7, Discoveries: []object.DiscoveryState{{Object: 7, Federate: 2, KnownClass: known}}}
	if tombstone {
		state.Deletions = []object.DeletionState{{Object: 7, Producer: 1, Timestamp: 5, Committed: true, ObjectName: "saved", ObjectClass: actual, Recipients: []object.DeletionRecipientState{{Recipient: 2, ReceivedTSO: true, KnownClass: known}}}}
	} else {
		state.Objects = []object.InstanceState{{Handle: 7, Name: "saved", Class: actual, Registrant: 1}}
	}
	generation := uint64(0)
	manifest := core.CheckpointManifest{Federation: "fed", FederationGeneration: &generation, Federates: []core.FederateHandle{1, 2}, FederateNames: []string{"producer", "recipient"}, ManagerSnapshots: map[string][]byte{
		"object":    graphJSON(t, objectSnapshotV1{Version: 4, State: state}),
		"ownership": nil, "declaration": graphJSON(t, declarationSnapshot{Version: 3}),
		"time": graphJSON(t, timeSnapshot{Version: 1, Federates: []timeFederateSnapshot{{Handle: 2, HasRegulationState: true, Constrained: true, HasAdvanceState: true, CurrentTime: 4}}}),
		"ddm":  nil, "sync": nil, "mom": nil,
		"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
	}}
	return manifest, model, state
}

func TestKnownClassGraphValidatesSavedFOMAncestry(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		for _, tc := range []struct {
			name  string
			valid bool
		}{
			{"", true}, {"HLAobjectRoot", true}, {"HLAobjectRoot.Base", true}, {"HLAobjectRoot.Base.Child", true},
			{"HLAobjectRoot.Base.Child.Leaf", false}, {"HLAobjectRoot.Sibling", false}, {"unknown", false},
		} {
			t.Run(fmt.Sprintf("tombstone=%t/%s", tombstone, tc.name), func(t *testing.T) {
				manifest, model, state := knownClassGraphFixture(t, tombstone)
				known, _ := model.LookupObjectClass(tc.name)
				if tc.name == "unknown" {
					known = 999
				}
				state.Discoveries[0].KnownClass = known
				if tombstone {
					state.Deletions[0].Recipients[0].KnownClass = known
				}
				manifest.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 4, State: state})
				err := (RestoreGraph{}).Validate("fed", manifest, model)
				if (err == nil) != tc.valid {
					t.Fatalf("valid=%t err=%v", tc.valid, err)
				}
			})
		}
	}
}

func TestKnownClassGraphChecksRecipientWithoutDiscoveryAndConflicts(t *testing.T) {
	for _, kind := range []string{"unknown-without-discovery", "sibling-without-discovery", "missing-actual-class", "conflicting-history"} {
		t.Run(kind, func(t *testing.T) {
			manifest, model, state := knownClassGraphFixture(t, true)
			switch kind {
			case "unknown-without-discovery":
				state.Discoveries = nil
				state.Deletions[0].Recipients[0].KnownClass = 999
			case "sibling-without-discovery":
				state.Discoveries = nil
				state.Deletions[0].Recipients[0].KnownClass, _ = model.LookupObjectClass("HLAobjectRoot.Sibling")
			case "missing-actual-class":
				state.Discoveries = nil
				state.Deletions[0].ObjectClass = 0
				state.Deletions[0].ObjectName = ""
			case "conflicting-history":
				state.Deletions[0].Recipients[0].KnownClass = 0
			}
			manifest.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 4, State: state})
			if err := (RestoreGraph{}).Validate("fed", manifest, model); err == nil {
				t.Fatal("invalid recipient knowledge accepted")
			}
		})
	}
}

type knownClassWithoutAncestry struct {
	core.FOMHandle
	core.FOMHandleNameLookup
}

func TestKnownClassGraphRequiresAncestryInterfaceOnlyForNontrivialClass(t *testing.T) {
	manifest, model, state := knownClassGraphFixture(t, false)
	limited := knownClassWithoutAncestry{model, model.(core.FOMHandleNameLookup)}
	if err := (RestoreGraph{}).Validate("fed", manifest, limited); err == nil {
		t.Fatal("unverifiable known-class ancestor accepted")
	}
	for _, known := range []core.ObjectClassHandle{0, state.Objects[0].Class} {
		state.Discoveries[0].KnownClass = known
		manifest.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 4, State: state})
		if err := (RestoreGraph{}).Validate("fed", manifest, limited); err != nil {
			t.Fatalf("registered class needs no ancestry lookup: %v", err)
		}
	}
}

func TestKnownClassGraphRebindPreservesDiscoveryAndRecipientClasses(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		manifest, model, state := knownClassGraphFixture(t, tombstone)
		want := state.Discoveries[0].KnownClass
		mapping := map[core.FederateHandle]core.FederateHandle{1: 9<<32 | 2, 2: 9<<32 | 1}
		rebound, err := (RestoreGraph{}).Rebind(manifest, 9, mapping)
		if err != nil {
			t.Fatal(err)
		}
		if err := (RestoreGraph{}).Validate("fed", rebound, model); err != nil {
			t.Fatal(err)
		}
		var wire objectSnapshotV1
		if err := json.Unmarshal(rebound.ManagerSnapshots["object"], &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Version != 4 || wire.State.Discoveries[0].KnownClass != want || wire.State.Discoveries[0].Federate != mapping[2] {
			t.Fatalf("discovery history rewritten: %+v", wire)
		}
		if tombstone && (wire.State.Deletions[0].Recipients[0].KnownClass != want || wire.State.Deletions[0].Recipients[0].Recipient != mapping[2]) {
			t.Fatal("recipient class was rewritten or lost")
		}
	}
}
