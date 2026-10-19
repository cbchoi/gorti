package savebundle

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"google.golang.org/protobuf/proto"
)

func inactiveDDMFixture(t *testing.T) (*graphValidation, *rtiv1.DDMManagerState, *ddm.Manager) {
	t.Helper()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "inactive.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>Position</name><dimensions><dimension>X</dimension></dimensions></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("fed", model)
	names := model.(core.FOMHandleNameLookup)
	cls, _ := model.LookupObjectClass("Vehicle")
	attr, _ := model.LookupAttribute(cls, "Position")
	dim, _ := names.LookupDimension("X")
	v := &graphValidation{model: model, names: names, members: map[core.FederateHandle]string{1: "owner", 2: "candidate"}, objects: map[core.ObjectHandle]object.InstanceState{100: {Handle: 100, Class: cls, Registrant: 1}}, knownClasses: map[[2]uint64]core.ObjectClassHandle{{100, 2}: cls}, owners: map[[2]uint64]core.FederateHandle{{100, uint64(attr)}: 1}, regions: map[uint64]*rtiv1.DDMRegionEntry{}}
	wire := &rtiv1.DDMManagerState{NextRegionHandle: 10, Regions: []*rtiv1.DDMRegionEntry{{RegionHandle: 10, OwnerFederate: 2, RoutingSpaceHandle: 1, DimensionHandles: []uint64{uint64(dim)}, Committed: []*rtiv1.DDMRangeEntry{{DimensionHandle: uint64(dim), Upper: 100}}}}, InactiveAssociations: []*rtiv1.DDMInactiveAssociationEntry{{ObjectHandle: 100, AttributeHandle: uint64(attr), AssociatingFederate: 2, RegionHandles: []uint64{10}}}}
	m, err := ddm.New(ddm.Options{FOMs: repo, Outbox: objectStateOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	return v, wire, m
}

func TestInactiveDDMRoundTripAndRebind(t *testing.T) {
	v, wire, m := inactiveDDMFixture(t)
	data := graphProto(t, wire)
	if err := v.validateDDM(data); err != nil {
		t.Fatal(err)
	}
	codec := DDM{Manager: m}
	commit, err := codec.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	if m.ExportState("fed") != nil {
		t.Fatal("prepare published live state")
	}
	commit()
	state := m.ExportState("fed")
	if len(state.InactiveAssociations) != 1 || state.InactiveAssociations[0].Federate != 2 {
		t.Fatalf("lost inactive associations: %+v", state)
	}
	roundTrip, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var restored rtiv1.DDMManagerState
	if err := proto.Unmarshal(roundTrip, &restored); err != nil || !proto.Equal(wire, &restored) {
		t.Fatalf("roundtrip=%v err=%v", &restored, err)
	}
	rebound, err := codec.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(rebound, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Regions[0].OwnerFederate != 102 || restored.InactiveAssociations[0].AssociatingFederate != 102 {
		t.Fatalf("partial identity rewrite: %v", &restored)
	}
}

func TestInactiveDDMGraphRejectsInvalidReferences(t *testing.T) {
	for name, change := range map[string]func(*rtiv1.DDMManagerState){
		"foreign region":   func(w *rtiv1.DDMManagerState) { w.Regions[0].OwnerFederate = 1 },
		"absent member":    func(w *rtiv1.DDMManagerState) { w.InactiveAssociations[0].AssociatingFederate = 99 },
		"absent object":    func(w *rtiv1.DDMManagerState) { w.InactiveAssociations[0].ObjectHandle = 99 },
		"absent attribute": func(w *rtiv1.DDMManagerState) { w.InactiveAssociations[0].AttributeHandle = 999 },
		"absent region":    func(w *rtiv1.DDMManagerState) { w.InactiveAssociations[0].RegionHandles = []uint64{99} },
		"empty regions":    func(w *rtiv1.DDMManagerState) { w.InactiveAssociations[0].RegionHandles = nil },
		"duplicate region": func(w *rtiv1.DDMManagerState) { w.InactiveAssociations[0].RegionHandles = []uint64{10, 10} },
		"duplicate row": func(w *rtiv1.DDMManagerState) {
			w.InactiveAssociations = append(w.InactiveAssociations, proto.Clone(w.InactiveAssociations[0]).(*rtiv1.DDMInactiveAssociationEntry))
		},
		"active owner": func(w *rtiv1.DDMManagerState) {
			w.InactiveAssociations[0].AssociatingFederate = 1
			w.Regions[0].OwnerFederate = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			v, wire, m := inactiveDDMFixture(t)
			change(wire)
			before := m.ExportState("fed")
			if err := v.validateDDM(graphProto(t, wire)); err == nil {
				t.Fatal("invalid graph accepted")
			}
			if !reflect.DeepEqual(before, m.ExportState("fed")) {
				t.Fatal("graph validation changed live state")
			}
		})
	}
}
