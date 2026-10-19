package savebundle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func deletionOwnerGraphFixture(t *testing.T) (core.CheckpointManifest, core.FOMHandle, *object.RegistryState) {
	t.Helper()
	m, model, _ := retractionGraphFixture(t)
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	attr, _ := model.LookupAttribute(cls, "Position")
	state := &object.RegistryState{LastObjectHandle: 4, LastOutboundSequence: 33,
		Objects: []object.InstanceState{{Handle: 4, Name: "vehicle", Class: cls, Registrant: 1<<32 | 1}},
		Deletions: []object.DeletionState{{Object: 4, Producer: 1<<32 | 1, Timestamp: 5, RetractionHandle: 7,
			OwnershipSnapshot: true, AttributeOwners: []object.DeletionAttributeOwner{{Attribute: attr, Owner: 1<<32 | 2}},
		}},
	}
	m.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 2, State: state})
	return m, model, state
}

func TestDeletionOwnerSnapshotGraphRebindsTypedOwners(t *testing.T) {
	m, model, state := deletionOwnerGraphFixture(t)
	if err := (RestoreGraph{}).Validate("fed", m, model); err != nil {
		t.Fatal(err)
	}
	refs, err := (RestoreGraph{}).References(m)
	if err != nil || !reflect.DeepEqual(refs, []core.FederateHandle{1<<32 | 1, 1<<32 | 2}) {
		t.Fatalf("owner references: %v %v", refs, err)
	}
	mapping := map[core.FederateHandle]core.FederateHandle{1<<32 | 1: 9<<32 | 10, 1<<32 | 2: 9<<32 | 20}
	rebound, err := (RestoreGraph{}).Rebind(m, 9, mapping)
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
	owner := wire.State.Deletions[0].AttributeOwners[0]
	if wire.Version != 2 || owner.Owner != mapping[1<<32|2] || owner.Attribute != state.Deletions[0].AttributeOwners[0].Attribute || wire.State.Deletions[0].Object != 4 || wire.State.Deletions[0].RetractionHandle != 7 {
		t.Fatalf("wrong ownership identity rewrite: %+v", wire)
	}
}

func TestDeletionOwnerSnapshotGraphRejectsCorruption(t *testing.T) {
	for name, mutate := range map[string]func(*object.RegistryState){
		"undefined attribute": func(s *object.RegistryState) { s.Deletions[0].AttributeOwners[0].Attribute = 999 },
		"zero attribute":      func(s *object.RegistryState) { s.Deletions[0].AttributeOwners[0].Attribute = 0 },
		"duplicate attribute": func(s *object.RegistryState) {
			s.Deletions[0].AttributeOwners = append(s.Deletions[0].AttributeOwners, s.Deletions[0].AttributeOwners[0])
		},
		"absent snapshot flag":   func(s *object.RegistryState) { s.Deletions[0].OwnershipSnapshot = false },
		"departed owner":         func(s *object.RegistryState) { s.Deletions[0].AttributeOwners[0].Owner = 1<<32 | 3 },
		"wrong execution":        func(s *object.RegistryState) { s.Deletions[0].AttributeOwners[0].Owner = 2<<32 | 2 },
		"internal non-MOM owner": func(s *object.RegistryState) { s.Deletions[0].AttributeOwners[0].Owner = ^core.FederateHandle(0) },
	} {
		t.Run(name, func(t *testing.T) {
			m, model, state := deletionOwnerGraphFixture(t)
			mutate(state)
			m.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 2, State: state})
			if err := (RestoreGraph{}).Validate("fed", m, model); err == nil {
				t.Fatal("corrupt deletion owner state accepted")
			}
		})
	}
	m, model, state := deletionOwnerGraphFixture(t)
	m.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 1, State: state})
	if err := (RestoreGraph{}).Validate("fed", m, model); err == nil {
		t.Fatal("legacy snapshot accepted ownership extension")
	}
	state.Deletions[0].AttributeOwners[0].Owner = 0
	m.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 2, State: state})
	if err := (RestoreGraph{}).Validate("fed", m, model); err != nil {
		t.Fatalf("unowned attribute rejected: %v", err)
	}
}
