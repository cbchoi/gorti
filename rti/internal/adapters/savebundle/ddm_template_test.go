package savebundle

import (
	"context"
	"errors"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"google.golang.org/protobuf/proto"
	"reflect"
	"testing"
)

func templateCodec(t *testing.T) DDM {
	t.Helper()
	repo := composition.NewFOMRepository()
	repo.RememberFor("fed", composition.NewModelHandle(model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{{Name: "X", UpperBound: ^uint64(0)}, {Name: "Y", UpperBound: 100}})))
	m, err := ddm.New(ddm.Options{Outbox: objectStateOutbox{}, FOMs: repo})
	if err != nil {
		t.Fatal(err)
	}
	return DDM{Manager: m}
}

func TestDDMTemplateSnapshotCoverageAndPrecision(t *testing.T) {
	ctx := context.Background()
	source := templateCodec(t)
	r, err := source.Manager.CreateRegion(ctx, "fed", 1, 1, []ddm.DimensionHandle{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	const exact uint64 = 1<<53 + 1
	if err := source.Manager.SetRangeBounds("fed", 1, r, 1, ddm.Range{Lower: exact, Upper: ^uint64(0)}); err != nil {
		t.Fatal(err)
	}
	data, err := source.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	restored := templateCodec(t)
	if err := restored.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.Manager.ExportState("fed"), restored.Manager.ExportState("fed")) {
		t.Fatal("snapshot lost template or exact pending bounds")
	}
	if err := restored.Manager.CommitRegionModifications(ctx, "fed", 1, []ddm.RegionHandle{r}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("incomplete restored template: %v", err)
	}
	if err := restored.Manager.SetRangeBounds("fed", 1, r, 2, ddm.Range{Lower: 2, Upper: 20}); err != nil {
		t.Fatal(err)
	}
	if err := restored.Manager.CommitRegionModifications(ctx, "fed", 1, []ddm.RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	if got, found := restored.Manager.QueryBounds("fed", r, 1); !found || got.Lower != exact || got.Upper != ^uint64(0) {
		t.Fatalf("lost integer precision: %+v %v", got, found)
	}
}

func TestDDMLegacySnapshotDefaultsToSpecification(t *testing.T) {
	data, err := proto.Marshal(&rtiv1.DDMManagerState{NextRegionHandle: 1, Regions: []*rtiv1.DDMRegionEntry{{RegionHandle: 1, OwnerFederate: 1, RoutingSpaceHandle: 1, DimensionHandles: []uint64{1}, Committed: []*rtiv1.DDMRangeEntry{{DimensionHandle: 1, Lower: 1<<53 + 1, Upper: ^uint64(0)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	restored := templateCodec(t)
	if err := restored.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if got, found := restored.Manager.QueryBounds("fed", 1, 1); !found || got.Lower != 1<<53+1 {
		t.Fatalf("legacy specification rejected: %+v %v", got, found)
	}
	if err := restored.Manager.CommitRegionModifications(context.Background(), "fed", 1, []ddm.RegionHandle{1}); err != nil {
		t.Fatal(err)
	}
}

func TestDDMTemplateRestoreRejectsBindingsBeforeMutation(t *testing.T) {
	for _, kind := range []string{"object subscription", "interaction subscription", "publication", "inactive association"} {
		t.Run(kind, func(t *testing.T) {
			v, wire, manager := inactiveDDMFixture(t)
			wire.Regions[0].Template = true
			wire.InactiveAssociations = nil
			attr := uint64(0)
			for key := range v.owners {
				attr = key[1]
			}
			switch kind {
			case "object subscription":
				wire.ObjectSubscriptions = []*rtiv1.DDMSubscriptionEntry{{ObjectClassHandle: uint64(v.objects[100].Class), AttributeHandle: attr, SubscriberFederate: 2, RegionHandles: []uint64{10}}}
			case "interaction subscription":
				wire.InteractionSubscriptions = []*rtiv1.DDMInteractionSubscriptionEntry{{InteractionClassHandle: 1, SubscriberFederate: 2, RegionHandles: []uint64{10}}}
			case "publication":
				wire.Regions[0].OwnerFederate = 1
				wire.ObjectPublications = []*rtiv1.DDMObjectPublicationEntry{{ObjectHandle: 100, AttributeHandle: attr, RegionHandles: []uint64{10}}}
			case "inactive association":
				wire.InactiveAssociations = []*rtiv1.DDMInactiveAssociationEntry{{ObjectHandle: 100, AttributeHandle: attr, AssociatingFederate: 2, RegionHandles: []uint64{10}}}
			}
			data := graphProto(t, wire)
			before := manager.ExportState("fed")
			if commit, err := (DDM{Manager: manager}).PrepareRestore("fed", data); err == nil || commit != nil {
				t.Fatal("template binding admitted by snapshot codec")
			}
			if err := v.validateDDM(data); err == nil {
				t.Fatal("template binding admitted by restore graph")
			}
			if !reflect.DeepEqual(before, manager.ExportState("fed")) {
				t.Fatal("failed prepare changed live state")
			}
		})
	}
}

func TestDDMTemplateRestoreRejectsMalformedDraft(t *testing.T) {
	for name, change := range map[string]func(*rtiv1.DDMRegionEntry){
		"missing full bounds":    func(r *rtiv1.DDMRegionEntry) { r.Committed = nil },
		"nonfull initial bounds": func(r *rtiv1.DDMRegionEntry) { r.Committed[0].Lower = 1 },
		"empty pending range": func(r *rtiv1.DDMRegionEntry) {
			r.Pending = []*rtiv1.DDMRangeEntry{{DimensionHandle: r.DimensionHandles[0], Lower: 4, Upper: 4}}
		},
		"pending unknown dimension": func(r *rtiv1.DDMRegionEntry) { r.Pending = []*rtiv1.DDMRangeEntry{{DimensionHandle: 999, Upper: 1}} },
	} {
		t.Run(name, func(t *testing.T) {
			v, wire, manager := inactiveDDMFixture(t)
			wire.InactiveAssociations = nil
			wire.Regions[0].Template = true
			change(wire.Regions[0])
			data := graphProto(t, wire)
			if commit, err := (DDM{Manager: manager}).PrepareRestore("fed", data); err == nil || commit != nil {
				t.Fatal("malformed draft admitted")
			}
			if err := v.validateDDM(data); err == nil {
				t.Fatal("malformed draft admitted by graph")
			}
		})
	}
}
