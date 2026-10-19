package ddm

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"slices"
	"testing"
)

type realizationFOM struct{ w4FOM }

func (realizationFOM) Dimensions() []model.Dimension {
	return []model.Dimension{{Name: "X", UpperBound: 100, DefaultValue: "[10..20)"}, {Name: "Y", UpperBound: 100, DefaultValue: "Excluded"}, {Name: "Z", UpperBound: 100, DefaultValue: "[30..40)"}}
}
func (realizationFOM) AttributeDimensions(_ core.ObjectClassHandle, a core.AttributeHandle) ([]DimensionHandle, bool) {
	switch a {
	case 1:
		return []DimensionHandle{1, 2}, true
	case 2:
		return []DimensionHandle{2}, true
	case 3:
		return []DimensionHandle{1, 3}, true
	case 4:
		return nil, true
	}
	return nil, false
}
func (f realizationFOM) InteractionDimensions(c core.InteractionClassHandle) ([]DimensionHandle, bool) {
	return f.AttributeDimensions(1, core.AttributeHandle(c))
}

type realizationRepo struct{ permissiveFOMRepo }

func (realizationRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return realizationFOM{}, nil
}

func TestContextualRealizationDefaultsAndExcluded(t *testing.T) {
	m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: realizationRepo{}, Objects: w4Objects{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	empty := w4Region(t, m, 2, nil)
	low := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 1, Upper: 5}})
	match := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 12, Upper: 15}})
	for _, cls := range []core.InteractionClassHandle{1, 2, 3} {
		if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 2, cls, []RegionHandle{empty}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{low}); err != nil || len(got) != 0 {
		t.Fatalf("default nonoverlap: %v %v", got, err)
	}
	if got, err := m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{match}); err != nil || !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("default overlap: %v %v", got, err)
	}
	producerEmpty := w4Region(t, m, 1, nil)
	if m.InteractionRegionsOverlapDefaultRegion(ctx, w4Fed, 2, []RegionHandle{producerEmpty}) {
		t.Fatal("all-excluded producer overlaps default interaction region")
	}
	if m.PublisherRegionsOverlapDefaultRegion(w4Fed, 1, 2, []RegionHandle{producerEmpty}) {
		t.Fatal("all-excluded producer overlaps default attribute region")
	}
	if !m.PublisherRegionsOverlapDefaultRegion(w4Fed, 1, 2, nil) {
		t.Fatal("absent publisher association is not the default region")
	}
	if got, err := m.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 2, []RegionHandle{producerEmpty}); err != nil || len(got) != 0 {
		t.Fatalf("all excluded: %v %v", got, err)
	}
	for _, attr := range []core.AttributeHandle{1, 2, 3} {
		if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 2, 1, []core.AttributeHandle{attr}, []RegionHandle{empty}); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.SubscribersForUpdate(w4Fed, 1, 1, []RegionHandle{match}); !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("object default overlap: %v", got)
	}
	if got := m.RegionSubscribersFor(w4Fed, 1, 2); len(got) != 0 {
		t.Fatalf("excluded realization overlaps default: %v", got)
	}
	if got := m.InteractionSubscribersForDefaultRegion(ctx, w4Fed, 2); len(got) != 0 {
		t.Fatalf("excluded interaction overlaps default: %v", got)
	}
	if got := m.InteractionSubscribersForDefaultRegion(ctx, w4Fed, 1); !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("nonempty interaction default: %v", got)
	}
	if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 4, []RegionHandle{empty}); err != core.ErrInvalidRegionContext {
		t.Fatalf("no available dimensions: %v", err)
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 2, 1, []core.AttributeHandle{4}, []RegionHandle{empty}); err != core.ErrInvalidRegionContext {
		t.Fatalf("attribute no dimensions: %v", err)
	}
}

func TestRestoredRealizationsRetainFOMDefaultsAndExcluded(t *testing.T) {
	newManager := func() *Manager {
		t.Helper()
		m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: realizationRepo{}, Objects: w4Objects{}})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	source := newManager()
	empty := w4Region(t, source, 2, nil)
	match := w4Region(t, source, 1, map[DimensionHandle]Range{1: {Lower: 12, Upper: 15}})
	ctx := context.Background()
	for _, cls := range []core.InteractionClassHandle{1, 2} {
		if err := source.SubscribeInteractionClassWithRegions(ctx, w4Fed, 2, cls, []RegionHandle{empty}); err != nil {
			t.Fatal(err)
		}
	}
	restored := newManager()
	restored.ImportState(w4Fed, source.ExportState(w4Fed))
	if got := restored.InteractionSubscribersForDefaultRegion(ctx, w4Fed, 1); !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("restored default: %v", got)
	}
	if got := restored.InteractionSubscribersForDefaultRegion(ctx, w4Fed, 2); len(got) != 0 {
		t.Fatalf("restored excluded default: %v", got)
	}
	if got, err := restored.InteractionSubscribersWithRegions(ctx, w4Fed, 1, 1, []RegionHandle{match}); err != nil || !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("restored contextual range: %v %v", got, err)
	}
}
