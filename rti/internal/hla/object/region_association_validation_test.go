package object

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type associationValidationFixture struct {
	registry *Registry
	regions  *ddm.Manager
	owners   *ownership.Manager
	outbox   *recordingOutbox
	object   core.ObjectHandle
	first    ddm.RegionHandle
	second   ddm.RegionHandle
	foreign  ddm.RegionHandle
	spare    ddm.RegionHandle
}

func newAssociationValidationFixture(t *testing.T) *associationValidationFixture {
	t.Helper()
	ctx := context.Background()
	r, decl, out := newTestRegistry(t, nil)
	r.opts.FOMs = &registrationGroupRepo{}
	decl.SetFOMRepository(r.opts.FOMs)
	owners, err := ownership.New(ownership.Options{
		Outbox:             out,
		PrepareAcquisition: r.PrepareOwnershipAcquisition,
		PrepareDivestiture: r.PrepareOwnershipReleaseDenied,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.opts.Ownership = owners
	r.opts.OnRegister = func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
		owners.RegisterInitialOwnership(fed, owner, obj, attrs)
	}
	for _, caller := range []core.FederateHandle{1, 2} {
		if err := decl.PublishObjectClassAttributes(ctx, "fed", caller, 1, []core.AttributeHandle{1, 2}); err != nil {
			t.Fatal(err)
		}
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := r.Register(ctx, "fed", 1, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	m, region := roRegions(t, r)
	f := &associationValidationFixture{registry: r, regions: m, owners: owners, outbox: out, object: obj,
		first: region(1, 0, 10), second: region(1, 20, 30), foreign: region(2, 0, 10), spare: region(1, 40, 50)}
	if err := m.AssociateRegionGroupsForUpdates(ctx, "fed", 1, obj, []core.AttributeRegionGroup{
		{Attributes: []core.AttributeHandle{1}, Regions: []ddm.RegionHandle{f.first}},
		{Attributes: []core.AttributeHandle{2}, Regions: []ddm.RegionHandle{f.second}},
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *associationValidationFixture) transferFirst(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := f.owners.UnconditionalDivest(ctx, "fed", 1, f.object, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := f.owners.Acquire(ctx, "fed", 2, f.object, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if !f.owners.IsOwnedBy("fed", 2, f.object, 1) {
		t.Fatal("fixture did not transfer ownership")
	}
}

// IEEE 1516.1-2010 9.6/9.7: empty products still validate their handles,
// but do not require ownership of an attribute they cannot associate.
func TestIEEE2010AssociationEmptyProductsWithoutOwnership(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "associate"
		if remove {
			name = "unassociate"
		}
		t.Run(name, func(t *testing.T) {
			f := newAssociationValidationFixture(t)
			before, objects, owners := f.regions.ExportState("fed"), f.registry.ExportState("fed"), f.owners.ExportState("fed")
			callbacks := len(f.outbox.Records())
			hooks := 0
			f.regions.SetUpdateRateHooks(func(context.Context, core.FederationName, bool) error { hooks++; return nil }, nil, nil)
			for _, groups := range [][]core.AttributeRegionGroup{
				nil, {{}}, {{Attributes: []core.AttributeHandle{1}}}, {{Attributes: []core.AttributeHandle{3}}},
				{{Regions: []ddm.RegionHandle{f.foreign}}},
				{{Attributes: []core.AttributeHandle{1, 3}}, {Regions: []ddm.RegionHandle{f.foreign}}},
			} {
				call := f.regions.AssociateRegionGroupsForUpdates
				if remove {
					call = f.regions.UnassociateRegionGroupsForUpdates
				}
				if err := call(context.Background(), "fed", 2, f.object, groups); err != nil {
					t.Errorf("empty products %+v: %v", groups, err)
				}
				if !reflect.DeepEqual(before, f.regions.ExportState("fed")) || !reflect.DeepEqual(objects, f.registry.ExportState("fed")) || !reflect.DeepEqual(owners, f.owners.ExportState("fed")) || len(f.outbox.Records()) != callbacks || hooks != 0 {
					t.Fatal("empty association products changed state or notified observers")
				}
			}
		})
	}
}

func TestIEEE2010AssociationMixedEmptyAndOwnedProducts(t *testing.T) {
	f := newAssociationValidationFixture(t)
	groups := []core.AttributeRegionGroup{
		{Attributes: []core.AttributeHandle{1}, Regions: []ddm.RegionHandle{f.spare}},
		{Attributes: []core.AttributeHandle{3}},
	}
	if err := f.regions.AssociateRegionGroupsForUpdates(context.Background(), "fed", 1, f.object, groups); err != nil {
		t.Fatal(err)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 1); !slices.Equal(got, []ddm.RegionHandle{f.first, f.spare}) {
		t.Fatalf("owned association=%v", got)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 3); len(got) != 0 {
		t.Fatalf("empty product created association=%v", got)
	}
	if err := f.registry.ChangeAttributeOrderType(context.Background(), "fed", 2, f.object, []core.AttributeHandle{1}, core.OrderTimestamp); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("order change lost its ownership precondition: %v", err)
	}
}

func TestIEEE2010UnassociationWithoutOwnershipPreservesOtherPairs(t *testing.T) {
	f := newAssociationValidationFixture(t)
	ctx := context.Background()
	before := f.regions.ExportState("fed")
	for _, attr := range []core.AttributeHandle{1, 3} {
		if err := f.regions.UnassociateRegionGroupsForUpdates(ctx, "fed", 2, f.object, []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{attr}, Regions: []ddm.RegionHandle{f.foreign}}}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, f.regions.ExportState("fed")) {
		t.Fatal("removing absent caller-owned regions changed another federate's associations")
	}
	f.transferFirst(t)
	owners := f.owners.ExportState("fed")
	if err := f.regions.UnassociateRegionGroupsForUpdates(ctx, "fed", 1, f.object, []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: []ddm.RegionHandle{f.first}}}); err != nil {
		t.Fatal(err)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 1); len(got) != 0 {
		t.Fatalf("removed association=%v", got)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 2); !slices.Equal(got, []ddm.RegionHandle{f.second}) {
		t.Fatalf("unrelated association=%v", got)
	}
	if !reflect.DeepEqual(owners, f.owners.ExportState("fed")) {
		t.Fatal("unassociation changed ownership")
	}
	if err := f.regions.DeleteRegion(ctx, "fed", 1, f.first); err != nil {
		t.Fatalf("explicitly removed region remains in use: %v", err)
	}
}

func TestIEEE2010AssociationKnowledgeAndLateInvalidPairsRemainAtomic(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "associate"
		if remove {
			name = "unassociate"
		}
		t.Run(name, func(t *testing.T) {
			f := newAssociationValidationFixture(t)
			region := f.spare
			if remove {
				f.transferFirst(t)
				region = f.first
			}
			first := core.AttributeRegionGroup{Attributes: []core.AttributeHandle{1}, Regions: []ddm.RegionHandle{region}}
			call := f.regions.AssociateRegionGroupsForUpdates
			if remove {
				call = f.regions.UnassociateRegionGroupsForUpdates
			}
			for _, tc := range []struct {
				name   string
				caller core.FederateHandle
				object core.ObjectHandle
				groups []core.AttributeRegionGroup
				want   error
			}{
				{"undefined-empty", 1, f.object, []core.AttributeRegionGroup{first, {Attributes: []core.AttributeHandle{999}}}, core.ErrAttributeNotFound},
				{"invalid-orphan", 1, f.object, []core.AttributeRegionGroup{first, {Regions: []ddm.RegionHandle{999}}}, core.ErrRegionNotFound},
				{"foreign-orphan", 1, f.object, []core.AttributeRegionGroup{first, {Regions: []ddm.RegionHandle{f.foreign}}}, core.ErrRegionNotOwnedByFederate},
				{"missing-empty", 1, 999, nil, core.ErrObjectHandleInvalid},
				{"unknown-empty", 3, f.object, nil, core.ErrObjectHandleInvalid},
			} {
				t.Run(tc.name, func(t *testing.T) {
					before, objects, owners := f.regions.ExportState("fed"), f.registry.ExportState("fed"), f.owners.ExportState("fed")
					callbacks := len(f.outbox.Records())
					if err := call(context.Background(), "fed", tc.caller, tc.object, tc.groups); !errors.Is(err, tc.want) {
						t.Errorf("error=%v, want %v", err, tc.want)
					}
					if !reflect.DeepEqual(before, f.regions.ExportState("fed")) || !reflect.DeepEqual(objects, f.registry.ExportState("fed")) || !reflect.DeepEqual(owners, f.owners.ExportState("fed")) || len(f.outbox.Records()) != callbacks {
						t.Fatal("invalid later pair changed state or emitted callbacks")
					}
				})
			}
		})
	}
}
