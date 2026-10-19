package object

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

type registrationGroupFOM struct{ stubFOMHandle }

func (registrationGroupFOM) ObjectClassName(cls core.ObjectClassHandle) (string, bool) {
	return "Item", cls == 1
}
func (registrationGroupFOM) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	return testValueName, cls == 1 && attr >= 1 && attr <= 3
}
func (registrationGroupFOM) AttributeDimensions(cls core.ObjectClassHandle, attr core.AttributeHandle) ([]core.DDMDimensionHandle, bool) {
	return []core.DDMDimensionHandle{1}, cls == 1 && attr >= 1 && attr <= 3
}

type registrationGroupRepo struct{ stubFOMRepo }

func (registrationGroupRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &registrationGroupFOM{}, nil
}

func TestGroupedRegistrationRejectsAllLateErrorsBeforeAllocation(t *testing.T) {
	journal := &recordingEventLog{}
	reg, decl, out := newTestRegistry(t, journal)
	ctx := context.Background()
	reg.opts.FOMs = &registrationGroupRepo{}
	m, err := ddm.New(ddm.Options{Outbox: out, FOMs: reg.opts.FOMs})
	if err != nil {
		t.Fatal(err)
	}
	reg.opts.RegistrationRegions = m
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	space, _ := m.LookupRoutingSpace("fed", "default")
	dim, _ := m.LookupDimension("fed", space, "X")
	valid, err := m.CreateRegion(ctx, "fed", 1, space, []ddm.DimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := m.CreateRegion(ctx, "fed", 2, space, []ddm.DimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.ReserveObjectInstanceName(ctx, "fed", 1, "held"); err != nil {
		t.Fatal(err)
	}
	for region, owner := range map[ddm.RegionHandle]core.FederateHandle{valid: 1, foreign: 2} {
		if err := m.SetRangeBounds("fed", owner, region, dim, ddm.Range{Upper: ^uint64(0)}); err != nil {
			t.Fatal(err)
		}
		if err := m.CommitRegionModifications(ctx, "fed", owner, []ddm.RegionHandle{region}); err != nil {
			t.Fatal(err)
		}
	}
	// Create the empty object state so all compared snapshots use the same shape.
	reg.stateFor("fed")
	hooks := 0
	reg.opts.OnRegister = func(core.FederationName, core.FederateHandle, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle) {
		hooks++
	}
	first := core.AttributeRegionGroup{Attributes: []core.AttributeHandle{1}, Regions: []core.DDMRegionHandleCore{valid}}
	for _, tc := range []struct {
		last core.AttributeRegionGroup
		want error
	}{
		{core.AttributeRegionGroup{Regions: []core.DDMRegionHandleCore{999}}, core.ErrRegionNotFound},
		{core.AttributeRegionGroup{Regions: []core.DDMRegionHandleCore{foreign}}, core.ErrRegionNotOwnedByFederate},
		{core.AttributeRegionGroup{Attributes: []core.AttributeHandle{999}}, core.ErrAttributeNotFound},
		{core.AttributeRegionGroup{Attributes: []core.AttributeHandle{3}}, core.ErrAttributeNotPublished},
	} {
		beforeObjects, beforeRegions := reg.ExportState("fed"), m.ExportState("fed")
		callbacks, events := len(out.Records()), len(journal.Records())
		obj, name, err := reg.RegisterWithRegionGroups(ctx, "fed", 1, 1, "held", []core.AttributeRegionGroup{first, tc.last})
		if !errors.Is(err, tc.want) || obj != 0 || name != "" {
			t.Fatalf("late registration error: obj=%d name=%q error=%v", obj, name, err)
		}
		if !reflect.DeepEqual(beforeObjects, reg.ExportState("fed")) || !reflect.DeepEqual(beforeRegions, m.ExportState("fed")) || len(out.Records()) != callbacks || len(journal.Records()) != events || hooks != 0 {
			t.Fatal("invalid late handle partially registered object")
		}
	}
	journal.failNext = errors.New("journal unavailable")
	before := reg.ExportState("fed")
	if _, _, err := reg.RegisterWithRegionGroups(ctx, "fed", 1, 1, "held", []core.AttributeRegionGroup{first}); err == nil {
		t.Fatal("journal error lost")
	}
	if !reflect.DeepEqual(before, reg.ExportState("fed")) || m.HasObjectAssociations("fed", 1) || hooks != 0 {
		t.Fatal("journal failure partially registered object")
	}
	obj, name, err := reg.RegisterWithRegionGroups(ctx, "fed", 1, 1, "held", []core.AttributeRegionGroup{first, {Attributes: []core.AttributeHandle{2}}})
	if err != nil || obj != 1 || name != "held" || hooks != 1 {
		t.Fatalf("retry lost ID or reservation: obj=%d name=%q error=%v hooks=%d", obj, name, err, hooks)
	}
	if len(m.PublisherRegionsFor("fed", obj, 1)) != 1 || len(m.PublisherRegionsFor("fed", obj, 2)) != 0 {
		t.Fatal("registration installed wrong explicit/default bindings")
	}
	for _, groups := range [][]core.AttributeRegionGroup{nil, {{Attributes: []core.AttributeHandle{1}}}, {{Regions: []core.DDMRegionHandleCore{valid}}}} {
		obj, _, err := reg.RegisterWithRegionGroups(ctx, "fed", 1, 1, "", groups)
		if err != nil || obj == 0 || m.HasObjectAssociations("fed", obj) {
			t.Fatalf("empty-product registration: obj=%d error=%v", obj, err)
		}
	}
}
