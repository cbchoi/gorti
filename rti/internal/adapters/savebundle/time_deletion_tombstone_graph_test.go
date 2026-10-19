package savebundle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func deletionTombstoneGraphFixture(t *testing.T) (core.CheckpointManifest, core.FOMHandle, map[string]any) {
	t.Helper()
	m, model, _ := retractionGraphFixture(t)
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	doc := deletionTombstoneDocument()
	d := tombstoneDeletion(doc)
	d["Producer"], d["ObjectClass"] = core.FederateHandle(1<<32|1), cls
	d["Recipients"].([]any)[0].(map[string]any)["Recipient"] = core.FederateHandle(1<<32 | 2)
	doc["state"].(map[string]any)["Discoveries"].([]any)[0].(map[string]any)["Federate"] = core.FederateHandle(1<<32 | 2)
	m.ManagerSnapshots["object"] = graphJSON(t, doc)
	m.ManagerSnapshots["time"] = graphJSON(t, timeSnapshot{Version: 1, StallTimeout: 15000000000, Federates: []timeFederateSnapshot{{Handle: 1<<32 | 2, HasRegulationState: true, Constrained: true, HasAdvanceState: true, CurrentTime: 4}}})
	return m, model, doc
}

func TestDeletionTombstoneV3GraphKnownStateAndRebind(t *testing.T) {
	m, model, _ := deletionTombstoneGraphFixture(t)
	if err := (RestoreGraph{}).Validate("fed", m, model); err != nil {
		t.Fatal(err)
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
	d := wire.State.Deletions[0]
	if wire.Version != 3 || d.Producer != mapping[1<<32|1] || d.Recipients[0].Recipient != mapping[1<<32|2] || wire.State.Discoveries[0].Federate != mapping[1<<32|2] || d.Object != 4 || d.ObjectName != "vehicle" {
		t.Fatalf("incomplete known-state rebind: %+v", wire)
	}
}

func TestDeletionTombstoneV3GraphDeadlineAndIdentityChecks(t *testing.T) {
	for name, mutate := range map[string]func(*core.CheckpointManifest, map[string]any){
		"forgotten still discovered": func(_ *core.CheckpointManifest, d map[string]any) {
			tombstoneDeletion(d)["Recipients"].([]any)[0].(map[string]any)["Forgotten"] = true
		},
		"unknown class": func(_ *core.CheckpointManifest, d map[string]any) { tombstoneDeletion(d)["ObjectClass"] = 999 },
		"missing name":  func(_ *core.CheckpointManifest, d map[string]any) { delete(tombstoneDeletion(d), "ObjectName") },
		"departed recipient": func(_ *core.CheckpointManifest, d map[string]any) {
			tombstoneDeletion(d)["Recipients"].([]any)[0].(map[string]any)["Recipient"] = core.FederateHandle(1<<32 | 3)
		},
		"missing time": func(m *core.CheckpointManifest, _ map[string]any) { m.ManagerSnapshots["time"] = nil },
		"past forgetting deadline": func(m *core.CheckpointManifest, _ map[string]any) {
			m.ManagerSnapshots["time"] = graphJSON(t, timeSnapshot{Version: 1, StallTimeout: 15000000000, Federates: []timeFederateSnapshot{{Handle: 1<<32 | 2, HasRegulationState: true, Constrained: true, HasAdvanceState: true, CurrentTime: 5}}})
		},
		"constraint disabled": func(m *core.CheckpointManifest, _ map[string]any) {
			m.ManagerSnapshots["time"] = graphJSON(t, timeSnapshot{Version: 1, StallTimeout: 15000000000, Federates: []timeFederateSnapshot{{Handle: 1<<32 | 2, HasRegulationState: true, HasAdvanceState: true, CurrentTime: 4}}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, model, doc := deletionTombstoneGraphFixture(t)
			mutate(&m, doc)
			m.ManagerSnapshots["object"] = graphJSON(t, doc)
			if err := (RestoreGraph{}).Validate("fed", m, model); err == nil {
				t.Fatal("inconsistent tombstone graph accepted")
			}
		})
	}
}

func TestDeletionTombstoneV3GraphQueuedRemovalAndNoRetraction(t *testing.T) {
	m, model, doc := deletionTombstoneGraphFixture(t)
	tombstoneDeletion(doc)["Recipients"] = nil
	delete(tombstoneDeletion(doc), "Recipients")
	m.ManagerSnapshots["object"] = graphJSON(t, doc)
	if err := (RestoreGraph{}).Validate("fed", m, model); err != nil {
		t.Fatalf("known object before removal callback rejected: %v", err)
	}
	tombstoneDeletion(doc)["RetractionHandle"] = 0
	m.ManagerSnapshots["object"] = graphJSON(t, doc)
	if err := (RestoreGraph{}).Validate("fed", m, model); err != nil {
		t.Fatalf("non-retractable deletion tombstone rejected: %v", err)
	}
}

func TestDeletionTombstoneV3RebindSortsRecipientRecords(t *testing.T) {
	doc := deletionTombstoneDocument()
	tombstoneDeletion(doc)["Recipients"] = []any{map[string]any{"Recipient": 2, "ReceivedTSO": true}, map[string]any{"Recipient": 3, "ReceivedTSO": true}}
	data := graphJSON(t, doc)
	rebound, err := (Object{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
		return map[core.FederateHandle]core.FederateHandle{1: 13, 2: 12, 3: 11}[h], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var wire objectSnapshotV1
	if err := json.Unmarshal(rebound, &wire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wire.State.Deletions[0].Recipients, []object.DeletionRecipientState{{Recipient: 11, ReceivedTSO: true}, {Recipient: 12, ReceivedTSO: true}}) {
		t.Fatal("rebound recipient records not canonical")
	}
	if _, err := (Object{}).RewriteFederates(data, func(core.FederateHandle) (core.FederateHandle, error) { return 10, nil }); err == nil {
		t.Fatal("colliding recipient identities accepted")
	}
}
