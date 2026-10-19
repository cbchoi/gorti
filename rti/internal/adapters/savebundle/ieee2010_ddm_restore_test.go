package savebundle

import (
	"context"
	"reflect"
	"slices"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/protobuf/proto"
)

func TestIEEE2010DDMRestoreDisjointOptionRows(t *testing.T) {
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "ddm-restore.xml", XML: []byte(`<objectModel>
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name>
<attribute><name>Position</name><dimensions><dimension>X</dimension></dimensions></attribute>
</objectClass></objectClass></objects>
<dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions>
<updateRates><updateRate><name>Slow</name><rate>2.5</rate></updateRate>
<updateRate><name>Fast</name><rate>10</rate></updateRate></updateRates></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	const fed core.FederationName = "ddm-restore"
	const member core.FederateHandle = 1<<32 | 1
	repo.RememberFor(fed, model)
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	attr, _ := model.LookupAttribute(cls, "Position")
	dim, found := model.(core.FOMHandleNameLookup).LookupDimension("X")
	if !found {
		t.Fatal("missing test dimension")
	}
	manager, err := ddm.New(ddm.Options{FOMs: repo, Outbox: objectStateOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	codec := DDM{Manager: manager}
	if _, err := manager.CreateRegion(ctx, fed, member, 1, []ddm.DimensionHandle{ddm.DimensionHandle(dim)}); err != nil {
		t.Fatal(err)
	}
	liveBefore := manager.ExportState(fed)
	wire := &rtiv1.DDMManagerState{NextRegionHandle: 3}
	for r := uint64(1); r <= 3; r++ {
		wire.Regions = append(wire.Regions, &rtiv1.DDMRegionEntry{
			RegionHandle: r, OwnerFederate: uint64(member), RoutingSpaceHandle: 1,
			DimensionHandles: []uint64{uint64(dim)}, Committed: []*rtiv1.DDMRangeEntry{{DimensionHandle: uint64(dim), Upper: 100}},
		})
	}
	wire.ObjectSubscriptions = []*rtiv1.DDMSubscriptionEntry{
		{ObjectClassHandle: uint64(cls), AttributeHandle: uint64(attr), SubscriberFederate: uint64(member), RegionHandles: []uint64{1, 2}, UpdateRateDesignator: "Slow"},
		{ObjectClassHandle: uint64(cls), AttributeHandle: uint64(attr), SubscriberFederate: uint64(member), RegionHandles: []uint64{3}, Passive: true, UpdateRateDesignator: "Fast"},
	}
	gen := uint64(1)
	manifest := core.CheckpointManifest{
		Federation: fed, FederationGeneration: &gen, Federates: []core.FederateHandle{member}, FederateNames: []string{"subscriber"},
		ManagerSnapshots: map[string][]byte{
			"object": nil, "ownership": nil, "ddm": nil, "sync": nil, "mom": nil,
			"declaration": graphJSON(t, declarationSnapshot{Version: 3, RegionalObjectInterests: []declarationObjectRecord{{Class: cls, Attribute: attr, Federates: []core.FederateHandle{member}}}}),
			"time":        graphJSON(t, timeSnapshot{Version: 1}),
			"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
		},
	}
	graph := RestoreGraph{Snapshots: map[string]core.CheckpointSnapshotter{"ddm": codec}, FOMs: repo}
	for _, reverse := range []bool{false, true} {
		candidate := proto.Clone(wire).(*rtiv1.DDMManagerState)
		if reverse {
			slices.Reverse(candidate.ObjectSubscriptions)
		}
		manifest.ManagerSnapshots["ddm"] = graphProto(t, candidate)
		if err := graph.Validate(fed, manifest, model); err != nil {
			t.Errorf("disjoint active/passive groups (reverse=%v) rejected: %v", reverse, err)
		}
	}
	for _, conflict := range []bool{false, true} {
		candidate := proto.Clone(wire).(*rtiv1.DDMManagerState)
		duplicate := proto.Clone(candidate.ObjectSubscriptions[0]).(*rtiv1.DDMSubscriptionEntry)
		duplicate.RegionHandles = []uint64{2}
		if conflict {
			duplicate.Passive, duplicate.UpdateRateDesignator = true, "Fast"
		}
		candidate.ObjectSubscriptions = append(candidate.ObjectSubscriptions, duplicate)
		manifest.ManagerSnapshots["ddm"] = graphProto(t, candidate)
		before := slices.Clone(manifest.ManagerSnapshots["ddm"])
		if err := graph.Validate(fed, manifest, model); err == nil {
			t.Errorf("overlapping duplicate triple (conflicting options=%v) accepted", conflict)
		}
		if !slices.Equal(before, manifest.ManagerSnapshots["ddm"]) || !reflect.DeepEqual(liveBefore, manager.ExportState(fed)) {
			t.Fatal("rejected restore changed snapshot input or live DDM state")
		}
	}
	manifest.ManagerSnapshots["ddm"] = graphProto(t, wire)
	if err := graph.Validate(fed, manifest, model); err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal(fed, manifest.ManagerSnapshots["ddm"]); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := codec.Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	var got rtiv1.DDMManagerState
	if err := proto.Unmarshal(roundTrip, &got); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(wire, &got) {
		t.Fatalf("mixed-option snapshot changed on restore: %v", &got)
	}
}
