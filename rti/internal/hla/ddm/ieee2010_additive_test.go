package ddm

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func ieee2010Regions(t *testing.T, m *Manager, owner core.FederateHandle, count int) []RegionHandle {
	t.Helper()
	space, _ := m.LookupRoutingSpace("additive", DefaultRoutingSpace)
	dim, _ := m.LookupDimension("additive", space, "X")
	var regions []RegionHandle
	for i := 0; i < count; i++ {
		r, err := m.CreateRegion(context.Background(), "additive", owner, space, []DimensionHandle{dim})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.SetRangeBounds("additive", owner, r, dim, Range{Lower: uint64(i * 10), Upper: uint64(i*10 + 5)}); err != nil {
			t.Fatal(err)
		}
		regions = append(regions, r)
	}
	if err := m.CommitRegionModifications(context.Background(), "additive", owner, regions); err != nil {
		t.Fatal(err)
	}
	return regions
}

func ieee2010Triples(t *testing.T, m *Manager, sub core.FederateHandle, attr core.AttributeHandle) map[RegionHandle]core.SubscriptionOptions {
	t.Helper()
	got := map[RegionHandle]core.SubscriptionOptions{}
	for _, row := range m.ExportState("additive").ObjectSubscriptions {
		if row.ObjectClass != 1 || row.Attribute != attr || row.Subscriber != sub {
			continue
		}
		for _, r := range row.Regions {
			if _, exists := got[r]; exists {
				t.Fatalf("duplicate class/attribute/region triple: %+v", row)
			}
			got[r] = core.SubscriptionOptions{Active: !row.Passive, UpdateRateDesignator: row.UpdateRateDesignator}
		}
	}
	return got
}

func TestIEEE2010RegionalAdditiveOptions(t *testing.T) {
	ctx := context.Background()
	m := newPermissiveManager(t)
	m.opts.ValidateSubscriptionOptions = func(context.Context, core.FederationName, core.SubscriptionOptions) error { return nil }
	var interest []core.AttributeHandle
	m.opts.OnObjectSubscriptionInterest = func(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) error {
		interest = slices.Clone(attrs)
		return nil
	}
	regions := ieee2010Regions(t, m, 2, 3)
	active := core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Slow"}
	passive := core.SubscriptionOptions{UpdateRateDesignator: "Fast"}
	subscribe := func(attrs []core.AttributeHandle, rs []RegionHandle, opts core.SubscriptionOptions) {
		t.Helper()
		if err := m.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, "additive", 2, 1, attrs, rs, opts); err != nil {
			t.Fatal(err)
		}
	}
	subscribe([]core.AttributeHandle{1, 2}, regions[:1], active)
	subscribe([]core.AttributeHandle{1}, regions[1:2], passive)
	want := map[RegionHandle]core.SubscriptionOptions{regions[0]: active, regions[1]: passive}
	if got := ieee2010Triples(t, m, 2, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("sequential singleton subscriptions = %v, want %v", got, want)
	}
	if !slices.Equal(interest, []core.AttributeHandle{1, 2}) {
		t.Fatalf("passive addition removed active interest: %v", interest)
	}
	for _, r := range regions[:2] {
		if got := m.SubscribersForUpdate("additive", 1, 1, []RegionHandle{r}); !slices.Equal(got, []core.FederateHandle{2}) {
			t.Fatalf("region %d delivery = %v", r, got)
		}
	}
	before := m.ExportState("additive")
	subscribe([]core.AttributeHandle{1}, nil, active)
	subscribe([]core.AttributeHandle{1, 1}, []RegionHandle{regions[1], regions[1]}, passive)
	if !reflect.DeepEqual(before, m.ExportState("additive")) {
		t.Fatal("empty or duplicate subscription changed the set/options")
	}
	for _, bad := range []RegionHandle{999999, ieee2010Regions(t, m, 3, 1)[0]} {
		before := m.ExportState("additive")
		err := m.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, "additive", 2, 1, []core.AttributeHandle{1, 2}, []RegionHandle{regions[2], bad}, passive)
		if !errors.Is(err, core.ErrRegionNotFound) && !errors.Is(err, core.ErrRegionNotOwnedByFederate) {
			t.Fatalf("invalid later region error = %v", err)
		}
		if !reflect.DeepEqual(before, m.ExportState("additive")) {
			t.Fatal("invalid later region partially changed subscriptions")
		}
	}
	subscribe([]core.AttributeHandle{1}, regions[:1], passive)
	if !slices.Equal(interest, []core.AttributeHandle{2}) {
		t.Fatalf("triple option change affected another attribute: %v", interest)
	}
	want[regions[0]] = passive
	if got := ieee2010Triples(t, m, 2, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("resubscribed triple = %v, want %v", got, want)
	}
	subscribe([]core.AttributeHandle{1}, regions[1:2], active)
	want[regions[1]] = active
	saved := m.ExportState("additive")
	restored := newPermissiveManager(t)
	restored.ImportState("additive", saved)
	if got := ieee2010Triples(t, restored, 2, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored options = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(saved, restored.ExportState("additive")) {
		t.Fatal("snapshot round trip changed grouped subscription state")
	}
	if err := restored.UnsubscribeObjectClassAttributesWithRegions(ctx, "additive", 2, 1, []core.AttributeHandle{1}, regions[:1]); err != nil {
		t.Fatal(err)
	}
	delete(want, regions[0])
	if got := ieee2010Triples(t, restored, 2, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("targeted unsubscribe = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(saved, m.ExportState("additive")) {
		t.Fatal("restored mutation aliased the snapshot or source manager")
	}
}

func TestIEEE2010RegionalGroupedRestoreAndRollback(t *testing.T) {
	ctx := context.Background()
	m := newPermissiveManager(t)
	regions := ieee2010Regions(t, m, 2, 3)
	peer := ieee2010Regions(t, m, 3, 1)[0]
	state := m.ExportState("additive")
	state.ObjectSubscriptions = []ObjectSubscriptionState{
		{ObjectClass: 1, Attribute: 1, Subscriber: 2, Regions: regions[:2], UpdateRateDesignator: "Slow"},
		{ObjectClass: 1, Attribute: 1, Subscriber: 2, Regions: regions[2:], Passive: true, UpdateRateDesignator: "Fast"},
		{ObjectClass: 1, Attribute: 1, Subscriber: 3, Regions: []RegionHandle{peer}},
	}
	m.ImportState("additive", state)
	m.opts.ValidateSubscriptionOptions = func(context.Context, core.FederationName, core.SubscriptionOptions) error { return nil }
	rejected := errors.New("advisory admission rejected")
	m.opts.OnObjectSubscriptionInterest = func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) error {
		return rejected
	}
	before := m.ExportState("additive")
	change := func() error {
		return m.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, "additive", 2, 1, []core.AttributeHandle{1}, regions[1:], core.SubscriptionOptions{UpdateRateDesignator: "Slow"})
	}
	if err := change(); !errors.Is(err, rejected) {
		t.Fatalf("hook failure = %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("additive")) {
		t.Fatal("failed option split did not restore all rows/options")
	}
	if err := m.UnsubscribeObjectClassAttributesWithRegions(ctx, "additive", 2, 1, []core.AttributeHandle{1}, regions[:1]); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("additive")) {
		t.Fatal("failed unsubscribe did not restore all rows/options")
	}
	m.opts.OnObjectSubscriptionInterest = nil
	if err := change(); err != nil {
		t.Fatal(err)
	}
	want := map[RegionHandle]core.SubscriptionOptions{
		regions[0]: {Active: true, UpdateRateDesignator: "Slow"},
		regions[1]: {UpdateRateDesignator: "Slow"},
		regions[2]: {UpdateRateDesignator: "Slow"},
	}
	if got := ieee2010Triples(t, m, 2, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("split legacy group = %v, want %v", got, want)
	}
	if got := ieee2010Triples(t, m, 3, 1); !reflect.DeepEqual(got, map[RegionHandle]core.SubscriptionOptions{peer: {Active: true}}) {
		t.Fatalf("other subscriber changed: %v", got)
	}
}

func TestIEEE2010RegionalConcurrentAdditions(t *testing.T) {
	m := newPermissiveManager(t)
	regions := ieee2010Regions(t, m, 2, 16)
	var wg sync.WaitGroup
	for _, r := range regions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.SubscribeObjectClassAttributesWithRegions(context.Background(), "additive", 2, 1, []core.AttributeHandle{1}, []RegionHandle{r}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := ieee2010Triples(t, m, 2, 1); len(got) != len(regions) {
		t.Fatalf("concurrent additions retained %d regions, want %d", len(got), len(regions))
	}
	if got := m.RegionSubscribersFor("additive", 1, 1); !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("duplicate delivery recipients: %v", got)
	}
}
