package ddm

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Source: S1516.1 active 2010 text, PDF pp. 224-225 (9.1.3-9.1.4),
// pp. 235/239 (9.6.3(i)/9.8.3(h)), pp. 241-243 (9.10-9.11).
// Persistence and hook rollback are engineering compatibility assertions.
const w4Fed core.FederationName = "w4-ddm"

type w4FOM struct{ permissiveFOMHandle }

func (w4FOM) Dimensions() []model.Dimension {
	// This fixture exercises overlap on explicit shared axes. Omitted axes are
	// deliberately excluded; default-range realization has separate coverage.
	return []model.Dimension{{Name: "X", UpperBound: 100, DefaultValue: "Excluded"}, {Name: "Y", UpperBound: 100, DefaultValue: "Excluded"}, {Name: "Z", UpperBound: 100, DefaultValue: "Excluded"}}
}
func (w4FOM) AttributeDimensions(cls core.ObjectClassHandle, attr core.AttributeHandle) ([]DimensionHandle, bool) {
	if cls != 1 {
		return nil, false
	}
	switch attr {
	case 1:
		return []DimensionHandle{2, 1}, true
	case 2:
		return []DimensionHandle{1, 3}, true
	case 3:
		return nil, true
	default:
		return nil, false
	}
}
func (w4FOM) InteractionDimensions(cls core.InteractionClassHandle) ([]DimensionHandle, bool) {
	switch cls {
	case 1:
		return []DimensionHandle{2, 1}, true
	case 2:
		return []DimensionHandle{1, 3}, true
	case 3:
		return nil, true
	default:
		return nil, false
	}
}

type w4Repo struct{ permissiveFOMRepo }

func (w4Repo) Get(context.Context, core.FederationName) (core.FOMHandle, error) { return w4FOM{}, nil }

type w4Objects struct{}

func (w4Objects) PrepareRegionAssociation(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, func(), error) {
	return 1, func() {}, nil
}
func w4Manager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: w4Repo{}, Objects: w4Objects{}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func w4Region(t *testing.T, m *Manager, owner core.FederateHandle, bounds map[DimensionHandle]Range) RegionHandle {
	t.Helper()
	space, ok := m.LookupRoutingSpace(w4Fed, DefaultRoutingSpace)
	if !ok {
		t.Fatal("missing routing space")
	}
	var dims []DimensionHandle
	for d := range bounds {
		dims = append(dims, d)
	}
	slices.Sort(dims)
	r, err := m.CreateRegion(context.Background(), w4Fed, owner, space, dims)
	if err != nil {
		t.Fatal(err)
	}
	for d, b := range bounds {
		if err := m.SetRangeBounds(w4Fed, owner, r, d, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.CommitRegionModifications(context.Background(), w4Fed, owner, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	return r
}
func w4Pairs(t *testing.T, m *Manager, sub core.FederateHandle) map[RegionHandle]bool {
	t.Helper()
	got := map[RegionHandle]bool{}
	for _, row := range m.ExportState(w4Fed).InteractionSubscriptions {
		if row.Subscriber != sub || row.InteractionClass != 1 {
			continue
		}
		for _, r := range row.Regions {
			if _, exists := got[r]; exists {
				t.Fatalf("duplicate interaction/region pair %d", r)
			}
			got[r] = !row.Passive
		}
	}
	return got
}

func TestIEEE2010W4ProperSubsetEntryPoints(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"register", "associate", "object-subscribe", "request", "interaction-subscribe", "send"} {
		t.Run(name, func(t *testing.T) {
			m := w4Manager(t)
			r := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}})
			bindings := map[core.AttributeHandle][]RegionHandle{1: {r}, 2: {r}}
			var err error
			switch name {
			case "register":
				var prepared core.ObjectRegionRegistration
				prepared, err = m.PrepareObjectRegistration(ctx, w4Fed, 1, 1, bindings)
				if err == nil {
					prepared.Commit(42)
					prepared.Release()
				}
			case "associate":
				err = m.AssociateRegionsForUpdates(ctx, w4Fed, 1, 42, bindings)
			case "object-subscribe":
				err = m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 1, 1, []core.AttributeHandle{1, 2}, []RegionHandle{r})
			case "request":
				var request core.RegionUpdateRequest
				request, err = m.PrepareRegionUpdateRequest(ctx, w4Fed, 1, 1, []core.AttributeHandle{1, 2}, []RegionHandle{r})
				if err == nil {
					request.Release()
				}
			case "interaction-subscribe":
				err = m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{r})
			case "send":
				_, err = m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{r})
			}
			if err != nil {
				t.Fatalf("proper subset {X} rejected for available {X,Y}/{X,Z}: %v", err)
			}
		})
	}
}

func TestIEEE2010W4OmittedDimensionsRouting(t *testing.T) {
	ctx := context.Background()
	m := w4Manager(t)
	x := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}})
	xy := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 15, Upper: 25}, 2: {Lower: 80, Upper: 90}})
	y := w4Region(t, m, 3, map[DimensionHandle]Range{2: {Lower: 10, Upper: 20}})
	xMiss := w4Region(t, m, 4, map[DimensionHandle]Range{1: {Lower: 30, Upper: 40}})
	// Seed valid saved state to isolate overlap from entry-point acceptance.
	state := m.ExportState(w4Fed)
	for i, r := range []RegionHandle{xy, y, xMiss} {
		sub := core.FederateHandle(i + 2)
		state.ObjectSubscriptions = append(state.ObjectSubscriptions, ObjectSubscriptionState{ObjectClass: 1, Attribute: 1, Subscriber: sub, Regions: []RegionHandle{r}})
		state.InteractionSubscriptions = append(state.InteractionSubscriptions, InteractionSubscriptionState{InteractionClass: 1, Subscriber: sub, Regions: []RegionHandle{r}})
	}
	state.ObjectPublications = []ObjectPublicationState{{Object: 42, Attribute: 1, Regions: []RegionHandle{xy}}, {Object: 43, Attribute: 1, Regions: []RegionHandle{y}}}
	m.ImportState(w4Fed, state)
	want := []core.FederateHandle{2}
	if got := m.SubscribersForUpdate(w4Fed, 1, 1, []RegionHandle{x}); !slices.Equal(got, want) {
		t.Errorf("object routing = %v, want %v", got, want)
	}
	if got := m.InteractionSubscribersForSend(w4Fed, 1, []RegionHandle{x}); !slices.Equal(got, want) {
		t.Errorf("interaction routing = %v, want %v", got, want)
	}
	if got, err := m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{x}); err != nil || !slices.Equal(got, want) {
		t.Errorf("validated send = %v, %v; want %v", got, err, want)
	}
	query, err := m.PrepareRegionUpdateRequest(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{x})
	if err != nil {
		t.Fatal(err)
	}
	defer query.Release()
	if !query.Matches(42, 1) || query.Matches(43, 1) || !query.Matches(44, 1) {
		t.Fatal("request matching lost shared-axis/default distinction")
	}
}

func TestIEEE2010W4OverlapSharedDimensions(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b regionBounds
		want bool
	}{
		{"omitted", regionBounds{1: {Lower: 10, Upper: 20}}, regionBounds{1: {Lower: 15, Upper: 25}, 2: {Lower: 80, Upper: 90}}, true},
		{"disjoint-dimensions", regionBounds{1: {Lower: 10, Upper: 20}}, regionBounds{2: {Lower: 10, Upper: 20}}, false},
		{"empty-explicit", regionBounds{}, regionBounds{1: {Lower: 10, Upper: 20}}, false},
		{"both-empty-explicit", regionBounds{}, regionBounds{}, false},
		{"shared-miss", regionBounds{1: {Lower: 10, Upper: 20}}, regionBounds{1: {Lower: 20, Upper: 30}, 2: {Lower: 10, Upper: 20}}, false},
		{"one-shared-axis-misses", regionBounds{1: {Lower: 10, Upper: 20}, 2: {Lower: 10, Upper: 20}}, regionBounds{1: {Lower: 15, Upper: 25}, 2: {Lower: 30, Upper: 40}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := regionPairOverlap(tc.a, tc.b); got != tc.want {
				t.Errorf("forward overlap = %v, want %v", got, tc.want)
			}
			if got := regionPairOverlap(tc.b, tc.a); got != tc.want {
				t.Errorf("reverse overlap = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIEEE2010W4InvalidSubsetCallsPreserveState(t *testing.T) {
	ctx := context.Background()
	m := w4Manager(t)
	xy := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 10, Upper: 20}})
	z := w4Region(t, m, 1, map[DimensionHandle]Range{3: {Lower: 10, Upper: 20}})
	foreign := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 10, Upper: 20}})
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{xy}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{xy}); err != nil {
		t.Fatal(err)
	}
	if err := m.AssociateRegionsForUpdates(ctx, w4Fed, 1, 42, map[core.AttributeHandle][]RegionHandle{1: {xy}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		name   string
		region RegionHandle
		want   error
	}{
		{"outside-dimension", z, core.ErrInvalidRegionContext},
		{"foreign", foreign, core.ErrRegionNotOwnedByFederate},
		{"missing", 999, core.ErrRegionNotFound},
	} {
		for _, op := range []string{"register", "associate", "object-subscribe", "interaction-subscribe", "interaction-unsubscribe", "send", "request"} {
			t.Run(bad.name+"/"+op, func(t *testing.T) {
				before := m.ExportState(w4Fed)
				rs := []RegionHandle{xy, bad.region}
				var err error
				switch op {
				case "register":
					var p core.ObjectRegionRegistration
					p, err = m.PrepareObjectRegistration(ctx, w4Fed, 1, 1, map[core.AttributeHandle][]RegionHandle{1: rs})
					if p != nil {
						p.Release()
					}
				case "associate":
					err = m.AssociateRegionsForUpdates(ctx, w4Fed, 1, 42, map[core.AttributeHandle][]RegionHandle{1: rs})
				case "object-subscribe":
					err = m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, rs)
				case "interaction-subscribe":
					err = m.SubscribeInteractionClassWithRegionsAndOptions(ctx, w4Fed, 1, 1, rs, false)
				case "interaction-unsubscribe":
					err = m.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 1, 1, rs)
				case "send":
					_, err = m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, rs)
				case "request":
					var q core.RegionUpdateRequest
					q, err = m.PrepareRegionUpdateRequest(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, rs)
					if q != nil {
						q.Release()
					}
				}
				if !errors.Is(err, bad.want) {
					t.Fatalf("error = %v, want %v", err, bad.want)
				}
				if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
					t.Fatal("invalid call mutated state")
				}
			})
		}
	}
	before := m.ExportState(w4Fed)
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 1, 1, []core.AttributeHandle{1, 2}, []RegionHandle{xy}); !errors.Is(err, core.ErrInvalidRegionContext) {
		t.Fatalf("mixed attribute dimensions: %v", err)
	}
	if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 1, 3, []RegionHandle{xy}); !errors.Is(err, core.ErrInvalidRegionContext) {
		t.Fatalf("dimensionless interaction accepted nonempty dimensions: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
		t.Fatal("invalid mixed context mutated state")
	}
}

func TestIEEE2010W4InteractionAdditiveOptions(t *testing.T) {
	ctx := context.Background()
	m := w4Manager(t)
	r1 := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 10, Upper: 20}})
	r2 := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 70, Upper: 80}, 2: {Lower: 10, Upper: 20}})
	pub := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 0, Upper: 90}, 2: {Lower: 0, Upper: 90}})
	var interest bool
	m.opts.OnInteractionSubscriptionInterest = func(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, active bool) error {
		interest = active
		return nil
	}
	subscribe := func(rs []RegionHandle, active bool) {
		t.Helper()
		if err := m.SubscribeInteractionClassWithRegionsAndOptions(ctx, w4Fed, 2, 1, rs, active); err != nil {
			t.Fatal(err)
		}
	}
	subscribe([]RegionHandle{r1}, true)
	subscribe([]RegionHandle{r2}, false)
	want := map[RegionHandle]bool{r1: true, r2: false}
	if got := w4Pairs(t, m, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("sequential region additions = %v, want %v", got, want)
	}
	if !interest {
		t.Fatal("passive addition erased earlier active interest")
	}
	for _, r := range []RegionHandle{r1, r2, pub} {
		if got := m.InteractionSubscribersForSend(w4Fed, 1, []RegionHandle{r}); !slices.Equal(got, []core.FederateHandle{2}) {
			t.Fatalf("region %d routing = %v", r, got)
		}
	}
	before := m.ExportState(w4Fed)
	subscribe(nil, false)
	subscribe([]RegionHandle{r2, r2}, false)
	if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
		t.Fatal("empty/duplicate call changed pairs")
	}
	subscribe([]RegionHandle{r1}, false)
	if interest {
		t.Fatal("all passive pairs retained active interest")
	}
	subscribe([]RegionHandle{r2}, true)
	if !interest {
		t.Fatal("active pair omitted from interest union")
	}
	if err := m.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, []RegionHandle{r1}); err != nil {
		t.Fatal(err)
	}
	if got := w4Pairs(t, m, 2); !reflect.DeepEqual(got, map[RegionHandle]bool{r2: true}) {
		t.Fatalf("targeted unsubscribe = %v", got)
	}
	if err := m.DeleteRegion(ctx, w4Fed, 2, r1); err != nil {
		t.Fatalf("removed region remains in use: %v", err)
	}
	if err := m.DeleteRegion(ctx, w4Fed, 2, r2); !errors.Is(err, core.ErrRegionInUse) {
		t.Fatalf("retained region lost in-use protection: %v", err)
	}
}

func TestIEEE2010W4InteractionEmptySets(t *testing.T) {
	for _, unsubscribe := range []bool{false, true} {
		t.Run(map[bool]string{false: "subscribe", true: "unsubscribe"}[unsubscribe], func(t *testing.T) {
			m := w4Manager(t)
			r := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 10, Upper: 20}})
			ctx := context.Background()
			if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, []RegionHandle{r}); err != nil {
				t.Fatal(err)
			}
			before := m.ExportState(w4Fed)
			var err error
			if unsubscribe {
				err = m.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, nil)
			} else {
				err = m.SubscribeInteractionClassWithRegionsAndOptions(ctx, w4Fed, 2, 1, nil, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
				t.Fatal("empty set changed existing regional subscription")
			}
		})
	}
}

func TestIEEE2010W4InteractionRestoreAndRollback(t *testing.T) {
	ctx := context.Background()
	m := w4Manager(t)
	var rs []RegionHandle
	for i := 0; i < 3; i++ {
		rs = append(rs, w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: uint64(i * 20), Upper: uint64(i*20 + 10)}, 2: {Lower: 0, Upper: 100}}))
	}
	peer := w4Region(t, m, 3, map[DimensionHandle]Range{1: {Lower: 0, Upper: 100}, 2: {Lower: 0, Upper: 100}})
	legacy := m.ExportState(w4Fed)
	legacy.InteractionSubscriptions = []InteractionSubscriptionState{
		{InteractionClass: 1, Subscriber: 2, Regions: rs[:2]},
		{InteractionClass: 1, Subscriber: 2, Regions: rs[2:], Passive: true},
		{InteractionClass: 1, Subscriber: 3, Regions: []RegionHandle{peer}},
	}
	m.ImportState(w4Fed, legacy)
	before := m.ExportState(w4Fed)
	rejected := errors.New("interest admission rejected")
	m.opts.OnInteractionSubscriptionInterest = func(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, bool) error {
		return rejected
	}
	change := func() error { return m.SubscribeInteractionClassWithRegionsAndOptions(ctx, w4Fed, 2, 1, rs[1:], false) }
	if err := change(); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
		t.Fatal("failed group split changed saved state")
	}
	if err := m.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, rs[:1]); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
		t.Fatal("failed targeted unsubscribe changed saved state")
	}
	m.opts.OnInteractionSubscriptionInterest = nil
	if err := change(); err != nil {
		t.Fatal(err)
	}
	want := map[RegionHandle]bool{rs[0]: true, rs[1]: false, rs[2]: false}
	if got := w4Pairs(t, m, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy group split = %v, want %v", got, want)
	}
	if got := w4Pairs(t, m, 3); !reflect.DeepEqual(got, map[RegionHandle]bool{peer: true}) {
		t.Fatalf("peer state changed: %v", got)
	}
	saved := m.ExportState(w4Fed)
	restored := w4Manager(t)
	restored.ImportState(w4Fed, saved)
	if !reflect.DeepEqual(saved, restored.ExportState(w4Fed)) {
		t.Fatal("mixed-option snapshot did not round trip")
	}
	if err := restored.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, rs[:1]); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, m.ExportState(w4Fed)) {
		t.Fatal("restore mutation aliased saved/source state")
	}
	for _, r := range rs[1:] {
		if got := restored.InteractionSubscribersForSend(w4Fed, 1, []RegionHandle{r}); !slices.Equal(got, []core.FederateHandle{2, 3}) {
			t.Fatalf("restored passive routing = %v", got)
		}
	}
}

func TestIEEE2010W4InteractionOrdinaryIndependence(t *testing.T) {
	ctx := context.Background()
	m := w4Manager(t)
	d := declaration.New()
	m.opts.OnInteractionSubscriptionInterest = d.SetRegionalInteractionSubscriptionInterest
	r := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 10, Upper: 20}})
	if err := d.SubscribeInteractionClass(ctx, w4Fed, 2, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	if err := m.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	if got := d.InteractionSubscribersFor(ctx, w4Fed, 1); !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("regional unsubscribe removed ordinary subscription: %v", got)
	}
	if err := m.SubscribeInteractionClassWithRegionsAndOptions(ctx, w4Fed, 2, 1, []RegionHandle{r}, false); err != nil {
		t.Fatal(err)
	}
	if err := d.UnsubscribeInteractionClass(ctx, w4Fed, 2, 1); err != nil {
		t.Fatal(err)
	}
	if got := m.InteractionSubscribersForSend(w4Fed, 1, []RegionHandle{r}); !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("ordinary unsubscribe removed passive regional subscription: %v", got)
	}
	if got := d.InteractionSubscribersFor(ctx, w4Fed, 1); len(got) != 0 {
		t.Fatalf("regional subscription widened ordinary recipients: %v", got)
	}
}

func TestIEEE2010W4InteractionConcurrentAdditions(t *testing.T) {
	m := w4Manager(t)
	var rs []RegionHandle
	for i := 0; i < 16; i++ {
		rs = append(rs, w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 0, Upper: 100}, 2: {Lower: 0, Upper: 100}}))
	}
	var wg sync.WaitGroup
	for _, r := range rs {
		wg.Add(1)
		go func(r RegionHandle) {
			defer wg.Done()
			if err := m.SubscribeInteractionClassWithRegions(context.Background(), w4Fed, 2, 1, []RegionHandle{r}); err != nil {
				t.Error(err)
			}
		}(r)
	}
	wg.Wait()
	if got := w4Pairs(t, m, 2); len(got) != len(rs) {
		t.Fatalf("retained %d concurrent additions, want %d", len(got), len(rs))
	}
	if got := m.InteractionSubscribersForSend(w4Fed, 1, rs); !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("union recipients not deduplicated: %v", got)
	}
}

func TestIEEE2010W4InteractionSnapshotOrder(t *testing.T) {
	m := w4Manager(t)
	var rs []RegionHandle
	for i := 0; i < 3; i++ {
		rs = append(rs, w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 0, Upper: 100}, 2: {Lower: 0, Upper: 100}}))
	}
	state := m.ExportState(w4Fed)
	state.InteractionSubscriptions = []InteractionSubscriptionState{
		{InteractionClass: 1, Subscriber: 2, Regions: rs[:1]},
		{InteractionClass: 1, Subscriber: 2, Regions: rs[1:], Passive: true},
	}
	m.ImportState(w4Fed, state)
	want := m.ExportState(w4Fed)
	slices.Reverse(state.InteractionSubscriptions)
	slices.Reverse(state.InteractionSubscriptions[0].Regions)
	m.ImportState(w4Fed, state)
	if got := m.ExportState(w4Fed); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot depends on restored row/region order: got %+v, want %+v", got.InteractionSubscriptions, want.InteractionSubscriptions)
	}
}

func TestIEEE2010W4SubsetSaveStateAndTargetedRemoval(t *testing.T) {
	ctx := context.Background()
	m := w4Manager(t)
	pub := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}})
	r1 := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 15, Upper: 25}, 2: {Lower: 80, Upper: 90}})
	r2 := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 70, Upper: 80}})
	if err := m.AssociateRegionsForUpdates(ctx, w4Fed, 1, 42, map[core.AttributeHandle][]RegionHandle{1: {pub}}); err != nil {
		t.Fatal(err)
	}
	for i, r := range []RegionHandle{r1, r2} {
		if err := m.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, w4Fed, 2, 1, []core.AttributeHandle{1}, []RegionHandle{r}, core.SubscriptionOptions{Active: i == 0}); err != nil {
			t.Fatal(err)
		}
		if err := m.SubscribeInteractionClassWithRegionsAndOptions(ctx, w4Fed, 2, 1, []RegionHandle{r}, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.SetRangeBounds(w4Fed, 1, pub, 1, Range{Lower: 70, Upper: 75}); err != nil {
		t.Fatal(err)
	}
	saved := m.ExportState(w4Fed)
	restored := w4Manager(t)
	_, _ = restored.LookupRoutingSpace(w4Fed, DefaultRoutingSpace)
	restored.ImportState(w4Fed, saved)
	if !reflect.DeepEqual(saved, restored.ExportState(w4Fed)) {
		t.Fatal("subset regions/options/pending bounds did not round trip")
	}
	checkRouting := func(want []core.FederateHandle) {
		t.Helper()
		if got := restored.SubscribersForUpdate(w4Fed, 1, 1, restored.PublisherRegionsFor(w4Fed, 42, 1)); !slices.Equal(got, want) {
			t.Fatalf("restored object recipients = %v, want %v", got, want)
		}
		if got, err := restored.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{pub}); err != nil || !slices.Equal(got, want) {
			t.Fatalf("restored interaction recipients = %v, %v; want %v", got, err, want)
		}
	}
	checkRouting([]core.FederateHandle{2})
	if err := restored.UnsubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 2, 1, []core.AttributeHandle{1}, []RegionHandle{r1}); err != nil {
		t.Fatal(err)
	}
	if err := restored.UnsubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, []RegionHandle{r1}); err != nil {
		t.Fatal(err)
	}
	checkRouting(nil)
	if err := restored.CommitRegionModifications(ctx, w4Fed, 1, []RegionHandle{pub}); err != nil {
		t.Fatal(err)
	}
	checkRouting([]core.FederateHandle{2})
	if err := restored.UnassociateRegionsForUpdates(ctx, w4Fed, 1, 42, map[core.AttributeHandle][]RegionHandle{1: {pub}}); err != nil {
		t.Fatal(err)
	}
	if got := restored.PublisherRegionsFor(w4Fed, 42, 1); len(got) != 0 {
		t.Fatalf("subset association was not removed: %v", got)
	}
	if !reflect.DeepEqual(saved, m.ExportState(w4Fed)) {
		t.Fatal("restored mutation aliased source or saved state")
	}
}
