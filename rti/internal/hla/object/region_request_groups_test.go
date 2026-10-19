package object

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

func TestGroupedRegionUpdateRequestsSelectOnceAndValidateAll(t *testing.T) {
	reg, decl, out := newTestRegistry(t, nil)
	ctx := context.Background()
	var objects []core.ObjectHandle
	for _, owner := range []core.FederateHandle{1, 2} {
		if err := decl.PublishObjectClassAttributes(ctx, "fed", owner, 1, []core.AttributeHandle{1, 2}); err != nil {
			t.Fatal(err)
		}
		obj, _, err := reg.Register(ctx, "fed", owner, 1, "")
		if err != nil {
			t.Fatal(err)
		}
		objects = append(objects, obj)
	}
	m, err := ddm.New(ddm.Options{Outbox: out, FOMs: reg.opts.FOMs})
	if err != nil {
		t.Fatal(err)
	}
	reg.opts.RegistrationRegions = m
	m.ImportState("fed", &ddm.ManagerState{
		NextRegionHandle: 5,
		Regions: []ddm.RegionState{
			{Handle: 1, Owner: 1, Dimensions: []ddm.DimensionHandle{1}, Committed: []ddm.RangeState{{Dimension: 1, Lower: 0, Upper: 10}}},
			{Handle: 2, Owner: 1, Dimensions: []ddm.DimensionHandle{1}, Committed: []ddm.RangeState{{Dimension: 1, Lower: 80, Upper: 100}}},
			{Handle: 3, Owner: 2, Dimensions: []ddm.DimensionHandle{1}, Committed: []ddm.RangeState{{Dimension: 1, Lower: 0, Upper: 10}}},
			{Handle: 4, Owner: 2, Dimensions: []ddm.DimensionHandle{1}, Committed: []ddm.RangeState{{Dimension: 1, Lower: 80, Upper: 100}}},
		},
		ObjectPublications: []ddm.ObjectPublicationState{
			{Object: objects[0], Attribute: 1, Regions: []ddm.RegionHandle{1}},
			{Object: objects[0], Attribute: 2, Regions: []ddm.RegionHandle{2}},
		},
	})
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: []ddm.RegionHandle{3}}, {Attributes: []core.AttributeHandle{2}, Regions: []ddm.RegionHandle{4}}}
	before := len(out.Records())
	for _, invalid := range []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{0}}, {Regions: []ddm.RegionHandle{999}}, {Regions: []ddm.RegionHandle{1}}} {
		err := reg.RequestAttributeValueUpdateWithRegionGroups(ctx, "fed", 2, 1, append(slices.Clone(groups), invalid), nil)
		if err == nil || len(out.Records()) != before {
			t.Fatalf("invalid late group emitted a callback: %v", err)
		}
	}
	groups = append(groups, groups[0])
	if err := reg.RequestAttributeValueUpdateWithRegionGroups(ctx, "fed", 2, 1, groups, []byte{0, 255}); err != nil {
		t.Fatal(err)
	}
	records := out.Records()[before:]
	if len(records) != 1 || records[0].Federate != 1 {
		t.Fatalf("request emitted duplicate or self callbacks: %+v", records)
	}
	callback := records[0].Event.(*CallbackEvent)
	if callback.Object != objects[0] || !slices.Equal(callback.AttributeHandles, []core.AttributeHandle{1, 2}) || !slices.Equal(callback.UserSuppliedTag, []byte{0, 255}) {
		t.Fatalf("grouped callback = %+v", callback)
	}
	groups[1].Regions = []ddm.RegionHandle{3}
	before = len(out.Records())
	if err := reg.RequestAttributeValueUpdateWithRegionGroups(ctx, "fed", 2, 1, groups, nil); err != nil {
		t.Fatal(err)
	}
	callback = out.Records()[before].Event.(*CallbackEvent)
	if !slices.Equal(callback.AttributeHandles, []core.AttributeHandle{1}) {
		t.Fatalf("heterogeneous query flattened: %+v", callback)
	}
	before = len(out.Records())
	if err := reg.RequestAttributeValueUpdateWithRegionGroups(ctx, "fed", 2, 1, []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1, 2}}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(out.Records()) != before {
		t.Fatal("empty region set emitted a callback")
	}
	failure := errors.New("outbox full")
	rejected := &rejectingUpdateOutbox{err: failure}
	reg.opts.Outbox = rejected
	err = reg.RequestAttributeValueUpdateWithRegionGroups(ctx, "fed", 2, 1, groups, nil)
	if !errors.Is(err, failure) || rejected.sendCalls != 0 {
		t.Fatalf("reservation failure was lost: %v", err)
	}
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 3, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Register(ctx, "fed", 3, 1, ""); err != nil {
		t.Fatal(err)
	}
	legacyOut := &failingUpdateOutbox{}
	reg.opts.Outbox = legacyOut
	if err := reg.RequestAttributeValueUpdateWithRegionGroups(ctx, "fed", 2, 1, groups, nil); err == nil || legacyOut.sendCalls != 0 {
		t.Fatalf("nonreservable outbox partially delivered grouped request: error=%v sends=%d", err, legacyOut.sendCalls)
	}
}

type legacyRequestRegions struct {
	core.ObjectRegionRegistrar
	core.RegionRouting
	calls int
}

func (r *legacyRequestRegions) PrepareRegionUpdateRequest(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle, []core.DDMRegionHandleCore) (core.RegionUpdateRequest, error) {
	r.calls++
	return legacyRequestFilter{}, nil
}

type legacyRequestFilter struct{}

func (legacyRequestFilter) Release()                                             {}
func (legacyRequestFilter) Matches(core.ObjectHandle, core.AttributeHandle) bool { return true }

func TestLegacyRegionRequestProviderRemainsSupported(t *testing.T) {
	reg, decl, out := newTestRegistry(t, nil)
	ctx := context.Background()
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Register(ctx, "fed", 1, 1, ""); err != nil {
		t.Fatal(err)
	}
	legacy := &legacyRequestRegions{}
	reg.opts.RegistrationRegions = legacy
	before := len(out.Records())
	if err := reg.RequestAttributeValueUpdateWithRegions(ctx, "fed", 2, 1, []core.AttributeHandle{1}, []core.DDMRegionHandleCore{1}, nil); err != nil {
		t.Fatal(err)
	}
	if legacy.calls != 1 || len(out.Records()) != before+1 {
		t.Fatal("legacy provider did not receive request")
	}
	if err := reg.RequestAttributeValueUpdateWithRegionGroups(ctx, "fed", 2, 1, nil, nil); !errors.Is(err, core.ErrInvalidRegionContext) {
		t.Fatalf("missing grouped capability: %v", err)
	}
}
