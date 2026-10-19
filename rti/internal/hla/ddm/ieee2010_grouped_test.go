package ddm

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func groupedFixture(t *testing.T) (*Manager, []RegionHandle) {
	t.Helper()
	m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatal(err)
	}
	space, _ := m.LookupRoutingSpace("fed", "default")
	dim, _ := m.LookupDimension("fed", space, "X")
	var regions []RegionHandle
	for i := 0; i < 3; i++ {
		r, err := m.CreateRegion(context.Background(), "fed", 1, space, []DimensionHandle{dim})
		if err != nil {
			t.Fatal(err)
		}
		regions = append(regions, r)
		initializeRegion(t, m, "fed", 1, r, map[DimensionHandle]Range{dim: {Upper: ^uint64(0)}})
	}
	return m, regions
}

func TestIEEE2010AssociationAddsAndEmptyGroupsDoNotClear(t *testing.T) {
	m, regions := groupedFixture(t)
	ctx := context.Background()
	for _, r := range []RegionHandle{regions[0], regions[1], regions[0]} {
		if err := m.AssociateRegionsForUpdates(ctx, "fed", 1, 9, map[core.AttributeHandle][]RegionHandle{1: {r}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.PublisherRegionsFor("fed", 9, 1); !slices.Equal(got, regions[:2]) {
		t.Fatalf("additive regions = %v", got)
	}
	before := m.ExportState("fed")
	for _, groups := range [][]core.AttributeRegionGroup{nil, {{Attributes: []core.AttributeHandle{1}}}} {
		if err := m.AssociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups); err != nil {
			t.Fatal(err)
		}
		if err := m.UnassociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) {
			t.Fatal("empty groups changed associations")
		}
	}
}

func TestIEEE2010GroupedSubscriptionsPrevalidateAndPreserveProducts(t *testing.T) {
	m, r := groupedFixture(t)
	ctx := context.Background()
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: r[:1]}, {Attributes: []core.AttributeHandle{2}, Regions: r[1:2]}}
	active := core.SubscriptionOptions{Active: true}
	if err := m.SubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, groups, active); err != nil {
		t.Fatal(err)
	}
	st := m.ExportState("fed")
	if len(st.ObjectSubscriptions) != 2 {
		t.Fatalf("subscriptions = %+v", st.ObjectSubscriptions)
	}
	for _, row := range st.ObjectSubscriptions {
		if !slices.Equal(row.Regions, []RegionHandle{r[int(row.Attribute)-1]}) {
			t.Fatalf("cross product leaked: %+v", row)
		}
	}
	invalid := append(slices.Clone(groups), core.AttributeRegionGroup{Regions: []RegionHandle{999}})
	for _, call := range []func() error{
		func() error {
			return m.SubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, invalid, core.SubscriptionOptions{})
		},
		func() error { return m.UnsubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, invalid) },
	} {
		if err := call(); !errors.Is(err, core.ErrRegionNotFound) {
			t.Fatalf("invalid orphan region: %v", err)
		}
		if !reflect.DeepEqual(st, m.ExportState("fed")) {
			t.Fatal("later invalid group partially committed")
		}
	}
	if err := m.UnsubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, groups); err != nil {
		t.Fatal(err)
	}
	if len(m.ExportState("fed").ObjectSubscriptions) != 0 {
		t.Fatal("grouped unsubscribe left subscriptions")
	}
}

func TestIEEE2010GroupedUpdateEmptyDoesNotMatchDefaultRegion(t *testing.T) {
	m, r := groupedFixture(t)
	query, err := m.PrepareRegionUpdateGroups(context.Background(), "fed", 1, 1, []core.AttributeRegionGroup{
		{Attributes: []core.AttributeHandle{1}}, {Attributes: []core.AttributeHandle{2}, Regions: r[:1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer query.Release()
	if query.Matches(9, 1, 1) {
		t.Fatal("empty query matched default region")
	}
	if !query.Matches(9, 2, 2) {
		t.Fatal("nonempty query did not match default region")
	}
}

func TestIEEE2010GroupedMutationRollsBackAllPairsOnHookFailure(t *testing.T) {
	m, r := groupedFixture(t)
	ctx := context.Background()
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: r[:1]}, {Attributes: []core.AttributeHandle{2}, Regions: r[1:2]}}
	if err := m.SubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, groups, core.SubscriptionOptions{Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.AssociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("admission failed")
	interestCalls := 0
	m.opts.OnObjectSubscriptionInterest = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) error {
		interestCalls++
		return failure
	}
	m.onRateDemandChanged = func(context.Context, core.FederationName, bool) error { return failure }
	before := m.ExportState("fed")
	for _, call := range []func() error{
		func() error {
			return m.SubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, groups, core.SubscriptionOptions{})
		},
		func() error { return m.UnsubscribeObjectClassAttributeRegionGroups(ctx, "fed", 1, 1, groups) },
		func() error {
			return m.AssociateRegionGroupsForUpdates(ctx, "fed", 1, 9, []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1, 2}, Regions: r[2:]}})
		},
		func() error { return m.UnassociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups) },
	} {
		if err := call(); !errors.Is(err, failure) {
			t.Fatalf("hook error = %v", err)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) {
			t.Fatal("failed grouped admission changed state")
		}
	}
	if interestCalls != 2 {
		t.Fatalf("hook calls = %d, want one per operation", interestCalls)
	}
}

func TestIEEE2010GroupedAssociationValidatesEvenEmptyProducts(t *testing.T) {
	m, r := groupedFixture(t)
	ctx := context.Background()
	before := m.ExportState("fed")
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: r[:1]}, {Regions: []RegionHandle{999}}}
	for _, call := range []func() error{
		func() error { return m.AssociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups) },
		func() error { return m.UnassociateRegionGroupsForUpdates(ctx, "fed", 1, 9, groups) },
	} {
		if err := call(); !errors.Is(err, core.ErrRegionNotFound) {
			t.Fatalf("invalid handle in empty product = %v", err)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) {
			t.Fatal("invalid later group changed state")
		}
	}
}

func TestIEEE2010GroupedDimensionsAreCheckedPerPairNotFlattened(t *testing.T) {
	m := w4Manager(t)
	ctx := context.Background()
	y := w4Region(t, m, 1, map[DimensionHandle]Range{2: {Lower: 10, Upper: 20}})
	z := w4Region(t, m, 1, map[DimensionHandle]Range{3: {Lower: 30, Upper: 40}})
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: []RegionHandle{y}}, {Attributes: []core.AttributeHandle{2}, Regions: []RegionHandle{z}}}
	if err := m.SubscribeObjectClassAttributeRegionGroups(ctx, w4Fed, 1, 1, groups, core.SubscriptionOptions{Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.AssociateRegionGroupsForUpdates(ctx, w4Fed, 1, 9, groups); err != nil {
		t.Fatal(err)
	}
	q, err := m.PrepareRegionUpdateGroups(ctx, w4Fed, 1, 1, groups)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Matches(9, 1, 1) || !q.Matches(9, 2, 2) {
		t.Fatal("valid disjoint dimensions did not match")
	}
	q.Release()
	before := m.ExportState(w4Fed)
	invalid := slices.Clone(groups)
	invalid[1].Regions = []RegionHandle{y}
	if err := m.SubscribeObjectClassAttributeRegionGroups(ctx, w4Fed, 1, 1, invalid, core.SubscriptionOptions{}); !errors.Is(err, core.ErrInvalidRegionContext) {
		t.Fatalf("wrong dimensions accepted: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
		t.Fatal("later dimensional mismatch partially committed")
	}
	if err := m.AssociateRegionGroupsForUpdates(ctx, w4Fed, 1, 9, invalid); !errors.Is(err, core.ErrInvalidRegionContext) {
		t.Fatalf("wrong association dimensions accepted: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
		t.Fatal("later association mismatch partially committed")
	}
	if q, err := m.PrepareRegionUpdateGroups(ctx, w4Fed, 1, 1, invalid); !errors.Is(err, core.ErrInvalidRegionContext) {
		if q != nil {
			q.Release()
		}
		t.Fatalf("wrong query dimensions accepted: %v", err)
	}
	absent := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{2}, Regions: []RegionHandle{y}}}
	if err := m.UnsubscribeObjectClassAttributeRegionGroups(ctx, w4Fed, 1, 1, absent); err != nil {
		t.Fatal(err)
	}
	if err := m.UnassociateRegionGroupsForUpdates(ctx, w4Fed, 1, 9, absent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
		t.Fatal("removing absent pairs changed other pairs")
	}
}
