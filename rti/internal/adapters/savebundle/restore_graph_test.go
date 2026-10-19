package savebundle

import (
	"context"
	"encoding/json"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/protobuf/proto"
)

func graphJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func graphProto(t *testing.T, v proto.Message) []byte {
	t.Helper()
	b, e := proto.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestSnapshotFederateRewriteIncludesHistoricalAndNestedIdentity(t *testing.T) {
	const source core.FederateHandle = 1<<32 | 7
	const target core.FederateHandle = 9<<32 | 20
	ts := timeSnapshotNumber(5)
	for _, tc := range []struct {
		name    string
		adapter any
		data    []byte
		count   int
	}{
		{"object", Object{}, graphJSON(t, objectSnapshotV1{Version: 1, State: &object.RegistryState{Objects: []object.InstanceState{{Handle: 1, Class: 1, Registrant: source}}, Deletions: []object.DeletionState{{Object: 2, Producer: source, Timestamp: 5, RetractionHandle: 3, Committed: true}}, Reservations: []object.NameReservationState{{Name: "r", Holder: source}}}}), 3},
		{"declaration", Declaration{}, graphJSON(t, declarationSnapshot{Version: 3, ObjectPublications: []declarationObjectRecord{{Class: 1, Attribute: 1, Federates: []core.FederateHandle{source}}}}), 1},
		{"time", Time{}, graphJSON(t, timeSnapshot{Version: 1, Federates: []timeFederateSnapshot{{Handle: source, Queue: []timeQueuedSnapshot{{Destination: source, Sender: source, Timestamp: 5, RetractionHandle: 3, Event: timeEventSnapshot{Kind: "remove", Object: 1, LogicalTime: &ts, Metadata: &core.ObjectCallbackMetadata{Producer: source, RetractionHandle: 3}}}}}}}), 4},
		{"retractions", Retractions{}, graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true, Producers: []timepkg.RetractionProducerHistory{{Producer: source, Accepted: []timepkg.RetractionIDRange{{First: 3, Last: 3}}}}}}), 1},
		{"ddm", DDM{}, graphProto(t, &rtiv1.DDMManagerState{Regions: []*rtiv1.DDMRegionEntry{{OwnerFederate: uint64(source)}}, ObjectSubscriptions: []*rtiv1.DDMSubscriptionEntry{{SubscriberFederate: uint64(source)}}}), 2},
		{"ownership", Ownership{}, graphProto(t, &rtiv1.OwnershipManagerState{Owners: []*rtiv1.OwnershipRecordEntry{{OwnerFederate: uint64(source)}}}), 1},
		{"sync", Synchronization{}, graphProto(t, &rtiv1.SyncManagerState{Points: []*rtiv1.SyncPointStateEntry{{Required: []uint64{uint64(source)}, Achieved: []uint64{uint64(source)}}}}), 2},
		{"mom", MOM{}, graphProto(t, &rtiv1.MOMManagerState{Federation: &rtiv1.MOMFederationRecord{FederateHandles: []uint64{uint64(source)}}, Federates: []*rtiv1.MOMFederateRecord{{Handle: uint64(source)}}}), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rw, ok := tc.adapter.(savepoint.FederateSnapshotRebinder)
			if !ok {
				t.Fatal("snapshot has no checked cross-execution identity rewrite")
			}
			calls := 0
			b, err := rw.RewriteFederates(tc.data, func(h core.FederateHandle) (core.FederateHandle, error) {
				if h != source {
					t.Fatalf("unexpected source %d", h)
				}
				calls++
				return target, nil
			})
			if err != nil || calls != tc.count {
				t.Fatalf("rewrite calls=%d expected=%d err=%v", calls, tc.count, err)
			}
			calls = 0
			_, err = rw.RewriteFederates(b, func(h core.FederateHandle) (core.FederateHandle, error) {
				if h != target {
					t.Fatalf("unrewritten source %d", h)
				}
				calls++
				return h, nil
			})
			if err != nil || calls != tc.count {
				t.Fatalf("roundtrip calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestRestoreGraphRejectsCrossManagerReferences(t *testing.T) {
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "graph.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>Position</name></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	attr, _ := model.LookupAttribute(cls, "Position")
	const member core.FederateHandle = 1<<32 | 1
	gen := uint64(1)
	base := func() savepoint.Manifest {
		return savepoint.Manifest{Federation: "f", FederationGeneration: &gen, Federates: []core.FederateHandle{member}, FederateNames: []string{"one"}, ManagerSnapshots: map[string][]byte{
			"object":      graphJSON(t, objectSnapshotV1{Version: 1, State: &object.RegistryState{LastObjectHandle: 1, Objects: []object.InstanceState{{Handle: 1, Class: cls, Name: "v", Registrant: member}}}}),
			"ownership":   graphProto(t, &rtiv1.OwnershipManagerState{Owners: []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 1, AttributeHandle: uint64(attr), OwnerFederate: uint64(member)}}}),
			"declaration": graphJSON(t, declarationSnapshot{Version: 3}), "time": graphJSON(t, timeSnapshot{Version: 1}), "ddm": nil, "sync": nil, "mom": nil,
			"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
		}}
	}
	graph := RestoreGraph{}
	if err := graph.Validate("f", core.CheckpointManifest(base()), model); err != nil {
		t.Fatalf("valid graph: %v", err)
	}
	t.Run("ownership-confirmation-history", func(t *testing.T) {
		m := base()
		wire := &rtiv1.OwnershipManagerState{
			Owners:         []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 1, AttributeHandle: uint64(attr), OwnerFederate: uint64(member)}},
			PendingDivests: []*rtiv1.PendingDivestEntry{{ObjectHandle: 1, AttributeHandle: uint64(attr), OwnerFederate: uint64(member), TwoPhase: true}},
		}
		m.ManagerSnapshots["ownership"], err = marshalOwnershipSnapshot(wire, []bool{true})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.Validate("f", core.CheckpointManifest(m), model); err != nil {
			t.Fatalf("valid confirmation history: %v", err)
		}
		wire.PendingDivests[0].TwoPhase = false
		m.ManagerSnapshots["ownership"], err = marshalOwnershipSnapshot(wire, []bool{true})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.Validate("f", core.CheckpointManifest(m), model); err == nil {
			t.Fatal("orphan confirmation history accepted")
		}
	})
	t.Run("strict-time-history-version", func(t *testing.T) {
		for _, version := range []uint32{1, 2, 3, 4, 5} {
			m := base()
			m.ManagerSnapshots["time"] = graphJSON(t, timeSnapshot{Version: version, Federates: []timeFederateSnapshot{{Handle: member, StrictZeroLookahead: true}}})
			err := graph.Validate("f", core.CheckpointManifest(m), model)
			if (err == nil) != (version == 3 || version == 4) {
				t.Fatalf("strict history version %d: %v", version, err)
			}
		}
	})
	t.Run("admitted-empty-dimension-region", func(t *testing.T) {
		repo.RememberFor("f", model)
		manager, err := ddm.New(ddm.Options{FOMs: repo, Outbox: objectStateOutbox{}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.CreateRegion(context.Background(), "f", member, 1, nil); err != nil {
			t.Fatal(err)
		}
		m := base()
		m.ManagerSnapshots["ddm"], err = (DDM{Manager: manager}).Marshal("f")
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.Validate("f", core.CheckpointManifest(m), model); err != nil {
			t.Fatalf("live admitted region rejected by checkpoint: %v", err)
		}
	})
	for _, tc := range []struct {
		name   string
		mutate func(*savepoint.Manifest)
	}{
		{"owner-not-member", func(m *savepoint.Manifest) {
			m.ManagerSnapshots["ownership"] = graphProto(t, &rtiv1.OwnershipManagerState{Owners: []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 1, AttributeHandle: uint64(attr), OwnerFederate: uint64(member + 8)}}})
		}},
		{"ownership-unknown-object", func(m *savepoint.Manifest) {
			m.ManagerSnapshots["ownership"] = graphProto(t, &rtiv1.OwnershipManagerState{Owners: []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 9, AttributeHandle: uint64(attr), OwnerFederate: uint64(member)}}})
		}},
		{"declaration-unknown-attribute", func(m *savepoint.Manifest) {
			m.ManagerSnapshots["declaration"] = graphJSON(t, declarationSnapshot{Version: 3, ObjectPublications: []declarationObjectRecord{{Class: cls, Attribute: 999, Federates: []core.FederateHandle{member}}}})
		}},
		{"regional-interest-without-subscription", func(m *savepoint.Manifest) {
			m.ManagerSnapshots["declaration"] = graphJSON(t, declarationSnapshot{Version: 3, RegionalObjectInterests: []declarationObjectRecord{{Class: cls, Attribute: attr, Federates: []core.FederateHandle{member}}}})
		}},
		{"region-unknown-dimension", func(m *savepoint.Manifest) {
			m.ManagerSnapshots["ddm"] = graphProto(t, &rtiv1.DDMManagerState{NextRegionHandle: 2, Regions: []*rtiv1.DDMRegionEntry{{RegionHandle: 1, OwnerFederate: uint64(member), RoutingSpaceHandle: 1, DimensionHandles: []uint64{999}}}})
		}},
		{"sync-invalid-state", func(m *savepoint.Manifest) {
			m.ManagerSnapshots["sync"] = graphProto(t, &rtiv1.SyncManagerState{Points: []*rtiv1.SyncPointStateEntry{{Label: "s", State: 99, Achieved: []uint64{uint64(member)}}}})
		}},
		{"queued-unaccepted-id", func(m *savepoint.Manifest) {
			ts := timeSnapshotNumber(5)
			m.ManagerSnapshots["time"] = graphJSON(t, timeSnapshot{Version: 1, Federates: []timeFederateSnapshot{{Handle: member, Queue: []timeQueuedSnapshot{{Destination: member, Sender: member, Timestamp: 5, RetractionHandle: 7, Event: timeEventSnapshot{Kind: "reflect", Object: 1, ObjectClass: cls, LogicalTime: &ts}}}}}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.mutate(&m)
			if err := graph.Validate("f", core.CheckpointManifest(m), model); err == nil {
				t.Fatal("inconsistent saved manager graph accepted")
			}
		})
	}
}
