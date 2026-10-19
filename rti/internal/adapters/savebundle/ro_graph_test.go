package savebundle

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func roGraphFixture(t *testing.T) (core.CheckpointManifest, core.FOMHandle, map[string]any) {
	t.Helper()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "ro.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>Position</name></attribute></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Ping</name><parameter><name>Value</name></parameter></interactionClass></interactionClass></interactions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	attr, _ := model.LookupAttribute(cls, "Position")
	ic, _ := model.LookupInteractionClass("HLAinteractionRoot.Ping")
	p, _ := model.LookupParameter(ic, "Value")
	const recipient core.FederateHandle = 1<<32 | 2
	const sender core.FederateHandle = 1<<32 | 1
	doc := roSnapshotDocument(t)
	roDocumentFederate(doc)["handle"] = recipient
	for _, entry := range roDocumentQueue(doc) {
		q := entry.(map[string]any)
		q["destination"] = recipient
		q["sender"] = sender
		e := q["event"].(map[string]any)
		e["metadata"].(*core.ObjectCallbackMetadata).Producer = sender
		switch e["kind"] {
		case "reflect":
			e["object_class"] = cls
			e["attributes"] = map[uint64][]byte{uint64(attr): {0, 255}}
			e["attribute_handles"] = []core.AttributeHandle{attr}
		case "receive":
			e["interaction_class"] = ic
			e["parameters"] = map[uint64][]byte{uint64(p): {0, 255}}
		case "remove":
			e["object_class"] = cls
		}
	}
	gen := uint64(1)
	manifest := core.CheckpointManifest{Federation: "fed", FederationGeneration: &gen, Federates: []core.FederateHandle{recipient}, FederateNames: []string{"receiver"}, ManagerSnapshots: map[string][]byte{
		"object":      graphJSON(t, objectSnapshotV1{Version: 1, State: &object.RegistryState{LastObjectHandle: 4, LastOutboundSequence: 33}}),
		"declaration": graphJSON(t, declarationSnapshot{Version: 3}),
		"ownership":   nil, "ddm": nil, "sync": nil, "mom": nil,
		"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
		"time":        graphJSON(t, doc),
	}}
	return manifest, model, doc
}

func TestRORestoreGraphAllowsDeletedObjectAndDepartedSender(t *testing.T) {
	m, model, _ := roGraphFixture(t)
	if err := (RestoreGraph{}).Validate("fed", m, model); err != nil {
		t.Fatalf("valid queued history rejected: %v", err)
	}
	refs, err := (RestoreGraph{}).References(m)
	if err != nil || !reflect.DeepEqual(refs, []core.FederateHandle{1<<32 | 1, 1<<32 | 2}) {
		t.Fatalf("historical producer omitted: %v, %v", refs, err)
	}
}

func TestRORestoreGraphRejectsInvalidReferences(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"old-version":     func(d map[string]any) { d["version"] = 3 },
		"wrong-recipient": func(d map[string]any) { roDocumentQueue(d)[0].(map[string]any)["destination"] = uint64(1<<32 | 3) },
		"producer-mismatch": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["metadata"].(*core.ObjectCallbackMetadata).Producer = 1<<32 | 3
		},
		"wrong-generation": func(d map[string]any) {
			q := roDocumentQueue(d)[0].(map[string]any)
			q["sender"] = uint64(2<<32 | 1)
			q["event"].(map[string]any)["metadata"].(*core.ObjectCallbackMetadata).Producer = 2<<32 | 1
		},
		"unallocated-reflect": func(d map[string]any) { roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["object"] = 5 },
		"unallocated-remove":  func(d map[string]any) { roDocumentQueue(d)[2].(map[string]any)["event"].(map[string]any)["object"] = 5 },
		"unknown-reflect-class": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["object_class"] = 999
		},
		"unknown-remove-class": func(d map[string]any) {
			roDocumentQueue(d)[2].(map[string]any)["event"].(map[string]any)["object_class"] = 999
		},
		"unknown-attribute": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["attributes"] = map[uint64][]byte{999: {1}}
		},
		"unknown-interaction": func(d map[string]any) {
			roDocumentQueue(d)[1].(map[string]any)["event"].(map[string]any)["interaction_class"] = 999
		},
		"unknown-parameter": func(d map[string]any) {
			roDocumentQueue(d)[1].(map[string]any)["event"].(map[string]any)["parameters"] = map[uint64][]byte{999: {1}}
		},
		"zero-region": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["metadata"].(*core.ObjectCallbackMetadata).Regions = []core.DDMRegionHandleCore{0}
		},
		"internal-non-MOM": func(d map[string]any) {
			q := roDocumentQueue(d)[0].(map[string]any)
			q["sender"] = ^core.FederateHandle(0)
			q["event"].(map[string]any)["metadata"].(*core.ObjectCallbackMetadata).Producer = 0
		},
		"delete-marker": func(d map[string]any) {
			roDocumentQueue(d)[2].(map[string]any)["event"].(map[string]any)["kind"] = "delete_marker"
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, model, d := roGraphFixture(t)
			mutate(d)
			m.ManagerSnapshots["time"] = graphJSON(t, d)
			if err := (RestoreGraph{}).Validate("fed", m, model); err == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
}

func TestRORestoreRebindPreservesObjectRegionAndPayload(t *testing.T) {
	m, model, d := roGraphFixture(t)
	const oldSender core.FederateHandle = 1<<32 | 1
	const oldRecipient core.FederateHandle = 1<<32 | 2
	const newSender core.FederateHandle = 9<<32 | 10
	const newRecipient core.FederateHandle = 9<<32 | 20
	// The region deliberately shares the federate's numeric value. Its type,
	// not that number, determines whether an identity is rebound.
	roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["metadata"].(*core.ObjectCallbackMetadata).Regions = []core.DDMRegionHandleCore{core.DDMRegionHandleCore(oldSender)}
	for _, index := range []int{0, 2} {
		roDocumentQueue(d)[index].(map[string]any)["event"].(map[string]any)["object"] = core.ObjectHandle(oldSender)
	}
	m.ManagerSnapshots["object"] = graphJSON(t, objectSnapshotV1{Version: 1, State: &object.RegistryState{LastObjectHandle: uint64(oldSender), LastOutboundSequence: 33}})
	m.ManagerSnapshots["time"] = graphJSON(t, d)
	before := append([]byte(nil), m.ManagerSnapshots["time"]...)
	got, err := (RestoreGraph{}).Rebind(m, 9, map[core.FederateHandle]core.FederateHandle{oldSender: newSender, oldRecipient: newRecipient})
	if err != nil {
		t.Fatal(err)
	}
	if err := (RestoreGraph{}).Validate("fed", got, model); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.ManagerSnapshots["time"], before) {
		t.Fatal("rebind mutated source")
	}
	var decoded struct {
		Federates []struct {
			Handle  core.FederateHandle `json:"handle"`
			ROQueue []struct {
				Destination, Sender core.FederateHandle
				Event               timeEventSnapshot
			} `json:"ro_queue"`
		} `json:"federates"`
	}
	if err := json.Unmarshal(got.ManagerSnapshots["time"], &decoded); err != nil {
		t.Fatal(err)
	}
	f := decoded.Federates[0]
	if f.Handle != newRecipient || len(f.ROQueue) != 3 {
		t.Fatalf("rebound state: %+v", f)
	}
	for _, q := range f.ROQueue {
		if q.Destination != newRecipient || q.Sender != newSender || q.Event.Metadata.Producer != newSender {
			t.Fatalf("unrewritten callback: %+v", q)
		}
	}
	e := f.ROQueue[0].Event
	if e.Object != core.ObjectHandle(oldSender) || e.Metadata.Regions[0] != core.DDMRegionHandleCore(oldSender) || !reflect.DeepEqual(e.UserSuppliedTag, []byte{0, 255, 0}) {
		t.Fatalf("non-federate identity/payload rewritten: %+v", e)
	}
	if f.ROQueue[1].Event.LogicalTime == nil || *f.ROQueue[1].Event.LogicalTime != 12.5 {
		t.Fatal("original timestamp changed")
	}
	if _, err := (RestoreGraph{}).Rebind(m, 9, map[core.FederateHandle]core.FederateHandle{oldRecipient: newRecipient}); err == nil {
		t.Fatal("missing historical sender mapping accepted")
	}
}
