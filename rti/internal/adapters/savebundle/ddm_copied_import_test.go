package savebundle

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/proto"
)

func copiedWireFixture() *rtiv1.DDMManagerState {
	return &rtiv1.DDMManagerState{NextRegionHandle: 92, ConveyedRegions: []*rtiv1.DDMConveyedRegionEntry{
		{Designator: 91, SourceRegionHandle: 90, Realization: []*rtiv1.DDMRangeEntry{{DimensionHandle: 1, Lower: 1<<53 + 1, Upper: ^uint64(0)}}, Recipients: []*rtiv1.DDMConveyedRegionRecipientEntry{{FederateHandle: 2, Active: true}, {FederateHandle: 3}}},
		{Designator: 92, SourceRegionHandle: 90, Realization: []*rtiv1.DDMRangeEntry{{DimensionHandle: 1, Lower: 4, Upper: 4}}, Recipients: []*rtiv1.DDMConveyedRegionRecipientEntry{{FederateHandle: 3}}},
	}}
}

func TestDDMConveyedMalformedImportIsAtomic(t *testing.T) {
	for name, change := range map[string]func(*rtiv1.DDMManagerState){
		"zero designator":      func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Designator = 0 },
		"zero source":          func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].SourceRegionHandle = 0 },
		"source designator":    func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Designator = 90 },
		"duplicate designator": func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Designator = 91 },
		"namespace overflow":   func(s *rtiv1.DDMManagerState) { s.NextRegionHandle = 91 },
		"regular collision":    func(s *rtiv1.DDMManagerState) { s.Regions = []*rtiv1.DDMRegionEntry{{RegionHandle: 92}} },
		"zero dimension":       func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Realization[0].DimensionHandle = 0 },
		"duplicate dimension": func(s *rtiv1.DDMManagerState) {
			s.ConveyedRegions[1].Realization = append(s.ConveyedRegions[1].Realization, proto.Clone(s.ConveyedRegions[1].Realization[0]).(*rtiv1.DDMRangeEntry))
		},
		"inverted bounds":   func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Realization[0].Lower = 5 },
		"zero receiver":     func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Recipients[0].FederateHandle = 0 },
		"internal receiver": func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Recipients[0].FederateHandle = ^uint64(0) },
		"duplicate receiver": func(s *rtiv1.DDMManagerState) {
			s.ConveyedRegions[1].Recipients = append(s.ConveyedRegions[1].Recipients, proto.Clone(s.ConveyedRegions[1].Recipients[0]).(*rtiv1.DDMConveyedRegionRecipientEntry))
		},
		"unleased copy": func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Recipients = nil },
		"empty copy":    func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1] = &rtiv1.DDMConveyedRegionEntry{} },
		"empty range":   func(s *rtiv1.DDMManagerState) { s.ConveyedRegions[1].Realization[0] = &rtiv1.DDMRangeEntry{} },
		"empty receiver": func(s *rtiv1.DDMManagerState) {
			s.ConveyedRegions[1].Recipients[0] = &rtiv1.DDMConveyedRegionRecipientEntry{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			codec := templateCodec(t)
			codec.Manager.ImportState("fed", copiedStateFixture(t))
			before := codec.Manager.ExportState("fed")
			wire := copiedWireFixture()
			change(wire)
			data := graphProto(t, wire)
			if commit, err := codec.PrepareRestore("fed", data); err == nil || commit != nil {
				t.Fatal("malformed later copy admitted by prepare")
			}
			if err := codec.Unmarshal("fed", data); err == nil {
				t.Fatal("malformed later copy imported")
			}
			if _, err := codec.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil }); err == nil {
				t.Fatal("malformed later copy rebound")
			}
			if !reflect.DeepEqual(before, codec.Manager.ExportState("fed")) {
				t.Fatal("failed copied import partially mutated live state")
			}
		})
	}
}

func TestDDMConveyedCandidateFOMAndRoster(t *testing.T) {
	for _, kind := range []string{"valid historical source", "unknown dimension", "excess bound", "missing receiver"} {
		t.Run(kind, func(t *testing.T) {
			v, wire, manager := inactiveDDMFixture(t)
			wire.NextRegionHandle = 92
			wire.ConveyedRegions = copiedWireFixture().ConveyedRegions
			for _, entry := range wire.ConveyedRegions {
				entry.SourceRegionHandle = 9000
				entry.Realization[0].DimensionHandle = wire.Regions[0].DimensionHandles[0]
				entry.Realization[0].Lower = 4
				entry.Realization[0].Upper = 20
				entry.Recipients = []*rtiv1.DDMConveyedRegionRecipientEntry{{FederateHandle: 2, Active: true}}
			}
			switch kind {
			case "unknown dimension":
				wire.ConveyedRegions[1].Realization[0].DimensionHandle = 999
			case "excess bound":
				wire.ConveyedRegions[1].Realization[0].Upper = 101
			case "missing receiver":
				wire.ConveyedRegions[1].Recipients[0].FederateHandle = 3
			}
			before := manager.ExportState("fed")
			data := graphProto(t, wire)
			graphErr := v.validateDDM(data)
			if (graphErr == nil) != (kind == "valid historical source") {
				t.Fatalf("candidate graph %s: %v", kind, graphErr)
			}
			commit, err := (DDM{Manager: manager}).PrepareRestoreWithFOM("fed", data, v.model)
			fomValid := kind == "valid historical source" || kind == "missing receiver"
			if (err == nil) != fomValid || (commit != nil) != fomValid {
				t.Fatalf("candidate FOM %s: %v", kind, err)
			}
			if !reflect.DeepEqual(before, manager.ExportState("fed")) {
				t.Fatal("candidate validation published live state")
			}
		})
	}
}

func TestDDMConveyedRebindRejectsReceiverCollision(t *testing.T) {
	data := graphProto(t, copiedWireFixture())
	if rewritten, err := (DDM{}).RewriteFederates(data, func(core.FederateHandle) (core.FederateHandle, error) { return 7, nil }); err == nil || rewritten != nil {
		t.Fatal("colliding receiver identities admitted")
	}
}

func TestDDMConveyedLegacyAndNamespace(t *testing.T) {
	codec := templateCodec(t)
	wire := copiedWireFixture()
	data := graphProto(t, wire)
	commit, err := codec.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	// Candidate decoding is detached from later caller buffer mutation.
	for i := range data {
		data[i] = 0
	}
	commit()
	if got, err := codec.Manager.QueryBoundsFor("fed", 2, 91, 1); err != nil || got.Lower != 1<<53+1 {
		t.Fatalf("detached restore: %+v %v", got, err)
	}
	region, err := codec.Manager.CreateRegion(context.Background(), "fed", 2, 1, []core.DDMDimensionHandle{1})
	if err != nil || region != 93 {
		t.Fatalf("namespace not preserved: %v %v", region, err)
	}
	legacy := graphProto(t, &rtiv1.DDMManagerState{NextRegionHandle: 100})
	if err := codec.Unmarshal("fed", legacy); err != nil {
		t.Fatal(err)
	}
	if state := codec.Manager.ExportState("fed"); state.NextRegionHandle != 100 || len(state.ConveyedRegions) != 0 {
		t.Fatal("absent legacy tag7 retained live copies")
	}
}
