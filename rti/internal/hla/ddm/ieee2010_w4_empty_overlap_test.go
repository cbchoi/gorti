package ddm

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

// Source: active IEEE 1516.1-2010 9.1.3.2(b), physical PDF p224,
// and 9.9/9.9.3(d-e), p240. Restore checks are compatibility assertions.
func TestIEEE2010W4ObjectEmptyRegionUnsubscribeNoOp(t *testing.T) {
	for _, empty := range []struct {
		name    string
		regions []RegionHandle
	}{{"nil", nil}, {"empty", []RegionHandle{}}} {
		for _, restored := range []bool{false, true} {
			name := empty.name + "/live"
			if restored {
				name = empty.name + "/restored"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				m := w4Manager(t)
				d := declaration.New()
				m.opts.OnObjectSubscriptionInterest = d.SetRegionalObjectSubscriptionInterest
				m.opts.ValidateSubscriptionOptions = func(context.Context, core.FederationName, core.SubscriptionOptions) error { return nil }
				r1 := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}})
				r2 := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 30, Upper: 40}})
				peer := w4Region(t, m, 3, map[DimensionHandle]Range{1: {Lower: 50, Upper: 60}})
				for _, sub := range []struct {
					owner core.FederateHandle
					attrs []core.AttributeHandle
					r     RegionHandle
					opts  core.SubscriptionOptions
				}{
					{2, []core.AttributeHandle{1, 2}, r1, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "Slow"}},
					{2, []core.AttributeHandle{1}, r2, core.SubscriptionOptions{UpdateRateDesignator: "Fast"}},
					{3, []core.AttributeHandle{1}, peer, core.SubscriptionOptions{Active: true}},
				} {
					if err := m.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, w4Fed, sub.owner, 1, sub.attrs, []RegionHandle{sub.r}, sub.opts); err != nil {
						t.Fatal(err)
					}
				}
				if err := d.SubscribeObjectClassAttributes(ctx, w4Fed, 2, 1, []core.AttributeHandle{1, 3}); err != nil {
					t.Fatal(err)
				}
				before := m.ExportState(w4Fed)
				declarationBefore := d.ExportState(w4Fed)
				if restored {
					m = w4Manager(t)
					m.ImportState(w4Fed, before)
					m.opts.OnObjectSubscriptionInterest = d.SetRegionalObjectSubscriptionInterest
				}
				if err := m.UnsubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 2, 1, []core.AttributeHandle{1, 2}, empty.regions); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
					t.Error("empty unsubscribe changed regional subscriptions or their options")
				}
				if !reflect.DeepEqual(declarationBefore, d.ExportState(w4Fed)) {
					t.Error("empty unsubscribe changed ordinary subscriptions or active regional interest")
				}
				for _, r := range []RegionHandle{r1, r2} {
					if got := m.SubscribersForUpdate(w4Fed, 1, 1, []RegionHandle{r}); !slices.Equal(got, []core.FederateHandle{2}) {
						t.Errorf("retained region %d recipients = %v, want [2]", r, got)
					}
					if err := m.DeleteRegion(ctx, w4Fed, 2, r); !errors.Is(err, core.ErrRegionInUse) {
						t.Errorf("retained region %d deletion = %v, want ErrRegionInUse", r, err)
					}
				}
			})
		}
	}
}

type w4NamedObjectFOM struct{ w4FOM }

func (w4NamedObjectFOM) ObjectClassName(cls core.ObjectClassHandle) (string, bool) {
	return "Object", cls == 1
}

type w4NamedObjectRepo struct{ w4Repo }

func (w4NamedObjectRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return w4NamedObjectFOM{}, nil
}

func TestIEEE2010W4ObjectEmptyRegionUnsubscribeValidation(t *testing.T) {
	m := w4Manager(t)
	m.opts.FOMs = w4NamedObjectRepo{}
	ctx := context.Background()
	r := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}})
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 2, 1, []core.AttributeHandle{1}, []RegionHandle{r}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState(w4Fed)
	for _, tc := range []struct {
		name  string
		class core.ObjectClassHandle
		attrs []core.AttributeHandle
		want  error
	}{
		{"unknown-class", 99, []core.AttributeHandle{1}, core.ErrObjectClassNotFound},
		{"unknown-class-empty-attributes", 99, nil, core.ErrObjectClassNotFound},
		{"zero-attribute", 1, []core.AttributeHandle{0}, core.ErrAttributeNotFound},
		{"unknown-later-attribute", 1, []core.AttributeHandle{1, 99}, core.ErrAttributeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, regions := range [][]RegionHandle{nil, {}} {
				err := m.UnsubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 2, tc.class, tc.attrs, regions)
				if !errors.Is(err, tc.want) {
					t.Errorf("invalid empty unsubscribe = %v, want %v", err, tc.want)
				}
				if !reflect.DeepEqual(before, m.ExportState(w4Fed)) {
					t.Error("invalid empty unsubscribe changed state")
				}
			}
		})
	}
}

// 9.1.4, physical PDF p225, explicitly includes equal lower bounds.
// These are predicate tests, not Set Range Bounds admission assertions:
// 10.30.3(i), p272, separately requires lower < upper for that service.
func TestIEEE2010W4RangeOverlapEqualLower(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b Range
		want bool
	}{
		{"equal-lower-positive", Range{Lower: 10, Upper: 20}, Range{Lower: 10, Upper: 30}, true},
		{"equal-lower-zero-width", Range{Lower: 10, Upper: 10}, Range{Lower: 10, Upper: 20}, true},
		{"both-zero-width", Range{Lower: 10, Upper: 10}, Range{Lower: 10, Upper: 10}, true},
		{"zero-origin", Range{Lower: 0, Upper: 0}, Range{Lower: 0, Upper: 10}, true},
		{"maximum-equal-lower", Range{Lower: ^uint64(0), Upper: ^uint64(0)}, Range{Lower: ^uint64(0), Upper: ^uint64(0)}, true},
		{"zero-width-inside", Range{Lower: 15, Upper: 15}, Range{Lower: 10, Upper: 20}, true},
		{"zero-width-at-upper", Range{Lower: 20, Upper: 20}, Range{Lower: 10, Upper: 20}, false},
		{"different-zero-width", Range{Lower: 10, Upper: 10}, Range{Lower: 20, Upper: 20}, false},
		{"touching-positive", Range{Lower: 10, Upper: 20}, Range{Lower: 20, Upper: 30}, false},
		{"separated", Range{Lower: 10, Upper: 20}, Range{Lower: 30, Upper: 40}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Overlap(tc.b); got != tc.want {
				t.Errorf("forward overlap = %v, want %v", got, tc.want)
			}
			if got := tc.b.Overlap(tc.a); got != tc.want {
				t.Errorf("reverse overlap = %v, want %v", got, tc.want)
			}
			if got := regionPairOverlap(regionBounds{1: tc.a}, regionBounds{1: tc.b, 2: {Lower: 80, Upper: 90}}); got != tc.want {
				t.Errorf("shared-dimension overlap = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIEEE2010W4RangeOverlapRoutingAndRestore(t *testing.T) {
	ctx := context.Background()
	m := w4Manager(t)
	pub := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 10, Upper: 20}})
	regions := []RegionHandle{
		w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}}),
		w4Region(t, m, 3, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 15, Upper: 25}}),
		w4Region(t, m, 4, map[DimensionHandle]Range{1: {Lower: 10, Upper: 20}, 2: {Lower: 30, Upper: 40}}),
		w4Region(t, m, 5, map[DimensionHandle]Range{}),
		w4Region(t, m, 6, map[DimensionHandle]Range{1: {Lower: 20, Upper: 30}}),
	}
	state := m.ExportState(w4Fed)
	// Seed the zero-width predicate input without asserting setter admission.
	for i := range state.Regions {
		if state.Regions[i].Handle == pub {
			state.Regions[i].Committed[0].Upper = 10
		}
	}
	for i, r := range regions {
		sub := core.FederateHandle(i + 2)
		state.ObjectSubscriptions = append(state.ObjectSubscriptions, ObjectSubscriptionState{ObjectClass: 1, Attribute: 1, Subscriber: sub, Regions: []RegionHandle{r}, Passive: i == 1})
		state.InteractionSubscriptions = append(state.InteractionSubscriptions, InteractionSubscriptionState{InteractionClass: 1, Subscriber: sub, Regions: []RegionHandle{r}, Passive: i == 1})
		state.ObjectPublications = append(state.ObjectPublications, ObjectPublicationState{Object: core.ObjectHandle(i + 42), Attribute: 1, Regions: []RegionHandle{r}})
	}
	m.ImportState(w4Fed, state)
	restored := w4Manager(t)
	restored.ImportState(w4Fed, m.ExportState(w4Fed))
	for _, tc := range []struct {
		name string
		m    *Manager
	}{{"live", m}, {"restored", restored}} {
		t.Run(tc.name, func(t *testing.T) {
			want := []core.FederateHandle{2, 3}
			if got := tc.m.SubscribersForUpdate(w4Fed, 1, 1, []RegionHandle{pub}); !slices.Equal(got, want) {
				t.Errorf("object recipients = %v, want %v", got, want)
			}
			if got := tc.m.InteractionSubscribersForSend(w4Fed, 1, []RegionHandle{pub}); !slices.Equal(got, want) {
				t.Errorf("interaction recipients = %v, want %v", got, want)
			}
			if got, err := tc.m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{pub}); err != nil || !slices.Equal(got, want) {
				t.Errorf("validated interaction recipients = %v, %v; want %v", got, err, want)
			}
			query, err := tc.m.PrepareRegionUpdateRequest(ctx, w4Fed, 1, 1, []core.AttributeHandle{1}, []RegionHandle{pub})
			if err != nil {
				t.Fatal(err)
			}
			defer query.Release()
			for i := range regions {
				if got := query.Matches(core.ObjectHandle(i+42), 1); got != (i < 2) {
					t.Errorf("regional request match %d = %v, want %v", i, got, i < 2)
				}
			}
		})
	}
	if !reflect.DeepEqual(state, m.ExportState(w4Fed)) || !reflect.DeepEqual(state, restored.ExportState(w4Fed)) {
		t.Fatal("routing or restore changed supplied range/subscription state")
	}
}
