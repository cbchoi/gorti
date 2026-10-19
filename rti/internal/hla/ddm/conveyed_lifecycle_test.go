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

func conveyedFixture(t *testing.T) (*Manager, []core.ConveyedRegionCopy) {
	t.Helper()
	m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: realizationRepo{}, Objects: w4Objects{}})
	if err != nil {
		t.Fatal(err)
	}
	r := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 12, Upper: 15}})
	_, copies, _, err := m.InteractionDeliverySnapshot(context.Background(), w4Fed, 1, 1, []RegionHandle{r})
	if err != nil || len(copies) != 1 {
		t.Fatalf("capture: %v %v", copies, err)
	}
	return m, copies
}

func TestConveyedRegionReceiverLeases(t *testing.T) {
	m, copies := conveyedFixture(t)
	ctx := context.Background()
	handles := []RegionHandle{copies[0].Designator}
	for _, h := range []core.FederateHandle{2, 3} {
		if err := m.ConveyCopies(ctx, w4Fed, h, copies); err != nil {
			t.Fatal(err)
		}
		if _, err := m.QueryBoundsFor(w4Fed, h, handles[0], 1); !errors.Is(err, core.ErrRegionNotFound) {
			t.Fatalf("pending query: %v", err)
		}
		if err := m.EnterConveyedRegions(ctx, w4Fed, h, handles); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.RegionDimensionsFor(w4Fed, 4, handles[0]); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("foreign dimension query: %v", err)
	}
	if err := m.ReleaseConveyedRegions(ctx, w4Fed, 4, handles); err != nil {
		t.Fatal(err)
	}
	if err := m.ReleaseConveyedRegions(ctx, w4Fed, 2, handles); err != nil {
		t.Fatal(err)
	}
	if err := m.ReleaseConveyedRegions(ctx, w4Fed, 2, handles); err != nil {
		t.Fatal(err)
	}
	if _, err := m.QueryBoundsFor(w4Fed, 2, handles[0], 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("released query: %v", err)
	}
	if got, err := m.QueryBoundsFor(w4Fed, 3, handles[0], 1); err != nil || got != (Range{Lower: 12, Upper: 15}) {
		t.Fatalf("other receiver: %v %v", got, err)
	}
	if err := m.SetRangeBounds(w4Fed, 3, handles[0], 1, Range{Lower: 20, Upper: 30}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("readonly mutation: %v", err)
	}
	if err := m.DeleteRegion(ctx, w4Fed, 3, handles[0]); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("readonly deletion: %v", err)
	}
}

func TestConveyedRegionBatchValidationIsAtomic(t *testing.T) {
	m, copies := conveyedFixture(t)
	ctx := context.Background()
	first := copies[0]
	first.Designator += 100
	bad := first
	bad.Designator = copies[0].Realization.SourceRegion()
	before := m.ExportState(w4Fed)
	if err := m.RestoreConveyedRegions(ctx, w4Fed, []core.ConveyedRegionCopy{first, bad}); err == nil {
		t.Fatal("accepted specification collision")
	}
	if after := m.ExportState(w4Fed); !reflect.DeepEqual(before, after) {
		t.Fatal("invalid batch changed state")
	}
	if _, found := m.ConveyedRegionDimensions(w4Fed, first.Designator); found {
		t.Fatal("partial cache insertion")
	}
	if err := m.ConveyCopies(ctx, w4Fed, 2, []core.ConveyedRegionCopy{first, bad}); err == nil {
		t.Fatal("accepted invalid receiver batch")
	}
	if after := m.ExportState(w4Fed); !reflect.DeepEqual(before, after) {
		t.Fatal("invalid receiver batch changed state")
	}
}

func TestConveyedRegionStateAndUniqueCaptures(t *testing.T) {
	m, copies := conveyedFixture(t)
	ctx := context.Background()
	_, later, _, err := m.InteractionDeliverySnapshot(ctx, w4Fed, 1, 1, []RegionHandle{copies[0].Realization.SourceRegion()})
	if err != nil {
		t.Fatal(err)
	}
	if later[0].Designator == copies[0].Designator {
		t.Fatal("different callbacks alias one designator")
	}
	if err := m.ConveyCopies(ctx, w4Fed, 2, copies); err != nil {
		t.Fatal(err)
	}
	if err := m.EnterConveyedRegions(ctx, w4Fed, 2, []RegionHandle{copies[0].Designator}); err != nil {
		t.Fatal(err)
	}
	if err := m.ConveyCopies(ctx, w4Fed, 3, copies); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState(w4Fed)
	m.ImportState(w4Fed, state)
	if got, err := m.QueryBoundsFor(w4Fed, 2, copies[0].Designator, 1); err != nil || got != (Range{Lower: 12, Upper: 15}) {
		t.Fatalf("active restore: %v %v", got, err)
	}
	if _, err := m.QueryBoundsFor(w4Fed, 3, copies[0].Designator, 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("pending restore: %v", err)
	}
	m.ForgetFederateAssociations(w4Fed, 2)
	if _, err := m.QueryBoundsFor(w4Fed, 2, copies[0].Designator, 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("resigned query: %v", err)
	}
	if err := m.EnterConveyedRegions(ctx, w4Fed, 3, []RegionHandle{copies[0].Designator}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.QueryBoundsFor(w4Fed, 3, copies[0].Designator, 1); err != nil {
		t.Fatalf("remaining receiver: %v", err)
	}
}

func TestConveyedRegionRoutingUsesPortableValuesAfterCacheRemoval(t *testing.T) {
	m, copies := conveyedFixture(t)
	ctx := context.Background()
	sub := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 12, Upper: 15}})
	if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, []RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 2, 1, []core.AttributeHandle{1}, []RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	source := copies[0].Realization.SourceRegion()
	if err := m.SetRangeBounds(w4Fed, 1, source, 1, Range{Lower: 50, Upper: 60}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, w4Fed, 1, []RegionHandle{source}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	delete(m.fed[w4Fed].conveyed, copies[0].Designator)
	m.mu.Unlock()
	if got, err := m.InteractionSubscribersForConveyedRegions(ctx, w4Fed, 1, copies); err != nil || !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("portable interaction: %v %v", got, err)
	}
	if got, err := m.SubscribersForConveyedUpdate(ctx, w4Fed, 1, 1, copies); err != nil || !slices.Equal(got, []core.FederateHandle{2}) {
		t.Fatalf("portable attribute: %v %v", got, err)
	}
}

func TestConveyedRegionCaptureAndOverlapAreAtomic(t *testing.T) {
	m, _ := conveyedFixture(t)
	ctx := context.Background()
	pub := w4Region(t, m, 1, map[DimensionHandle]Range{1: {Lower: 12, Upper: 15}})
	sub := w4Region(t, m, 2, map[DimensionHandle]Range{1: {Lower: 12, Upper: 15}})
	if err := m.SubscribeInteractionClassWithRegions(ctx, w4Fed, 2, 1, []RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			bounds := Range{Lower: 12, Upper: 15}
			if i%2 != 0 {
				bounds = Range{Lower: 50, Upper: 60}
			}
			if err := m.SetRangeBounds(w4Fed, 1, pub, 1, bounds); err != nil {
				t.Error(err)
				return
			}
			if err := m.CommitRegionModifications(ctx, w4Fed, 1, []RegionHandle{pub}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		subs, copies, _, err := m.InteractionDeliverySnapshot(ctx, w4Fed, 1, 1, []RegionHandle{pub})
		if err != nil {
			t.Error(err)
			break
		}
		bounds, _ := copies[0].Realization.RangeBounds(1)
		want := bounds == (Range{Lower: 12, Upper: 15})
		if slices.Contains(subs, core.FederateHandle(2)) != want {
			t.Errorf("routing and copied bounds disagree: %v %v", subs, bounds)
			break
		}
	}
	wg.Wait()
}

func TestConveyedRegionInvalidEntryKeepsWholeBatchPending(t *testing.T) {
	m, copies := conveyedFixture(t)
	ctx := context.Background()
	if err := m.ConveyCopies(ctx, w4Fed, 2, copies); err != nil {
		t.Fatal(err)
	}
	if err := m.EnterConveyedRegions(ctx, w4Fed, 2, []RegionHandle{copies[0].Designator, 999}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("invalid entry: %v", err)
	}
	if _, err := m.QueryBoundsFor(w4Fed, 2, copies[0].Designator, 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("partial entry: %v", err)
	}
	m.OnFederateResign(w4Fed, 2)
	if err := m.EnterConveyedRegions(ctx, w4Fed, 2, []RegionHandle{copies[0].Designator}); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("resigned pending lease survived: %v", err)
	}
}

func TestConveyedRegionSourceResignPreservesReceiverValues(t *testing.T) {
	m, copies := conveyedFixture(t)
	ctx := context.Background()
	if err := m.ConveyCopies(ctx, w4Fed, 2, copies); err != nil {
		t.Fatal(err)
	}
	m.OnFederateResign(w4Fed, 1)
	if err := m.EnterConveyedRegions(ctx, w4Fed, 2, []RegionHandle{copies[0].Designator}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.QueryBoundsFor(w4Fed, 2, copies[0].Designator, 1); err != nil || got != (Range{Lower: 12, Upper: 15}) {
		t.Fatalf("source resign changed copy: %v %v", got, err)
	}
	if dims, err := m.RegionDimensionsFor(w4Fed, 2, copies[0].Designator); err != nil || !slices.Equal(dims, []DimensionHandle{1}) {
		t.Fatalf("realized dimensions: %v %v", dims, err)
	}
	m.OnFederationDestroyed(w4Fed)
	if _, err := m.QueryBoundsFor(w4Fed, 2, copies[0].Designator, 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("destroy retained copy: %v", err)
	}
}

func TestRegionQueryIsCreatorOrActiveCopiedReceiverOnly(t *testing.T) {
	m, copies := conveyedFixture(t)
	source := copies[0].Realization.SourceRegion()
	if _, err := m.QueryBoundsFor(w4Fed, 2, source, 1); !errors.Is(err, core.ErrRegionNotOwnedByFederate) {
		t.Fatalf("foreign specification bounds: %v", err)
	}
	if _, err := m.RegionDimensionsFor(w4Fed, 2, source); !errors.Is(err, core.ErrRegionNotOwnedByFederate) {
		t.Fatalf("foreign specification dimensions: %v", err)
	}
	if _, err := m.QueryBoundsFor(w4Fed, 1, source, 1); err != nil {
		t.Fatal(err)
	}
	template, err := m.CreateRegion(context.Background(), w4Fed, 1, 1, []DimensionHandle{1})
	if err != nil {
		t.Fatal(err)
	}
	if dimensions, err := m.RegionDimensionsFor(w4Fed, 1, template); err != nil || !slices.Equal(dimensions, []DimensionHandle{1}) {
		t.Fatalf("template dimensions: %v %v", dimensions, err)
	}
	if _, err := m.QueryBoundsFor(w4Fed, 1, template, 1); !errors.Is(err, core.ErrRegionNotFound) {
		t.Fatalf("template bounds: %v", err)
	}
}

func TestConveyedLegacyEmptyAxisDoesNotOverlapDefault(t *testing.T) {
	for _, bounds := range []regionBounds{{1: {Lower: 2, Upper: 2}}, {1: {Lower: 1, Upper: 5}, 2: {Lower: 2, Upper: 2}}, {}} {
		if overlapsDefault([]regionBounds{bounds}) {
			t.Fatalf("empty realization overlaps default: %v", bounds)
		}
	}
	if !overlapsDefault([]regionBounds{{1: {Lower: 1, Upper: 5}}}) {
		t.Fatal("nonempty realization rejected")
	}
}

func TestConveyedCaptureWithoutDeliveryDoesNotRetainCache(t *testing.T) {
	m, copies := conveyedFixture(t)
	source := copies[0].Realization.SourceRegion()
	previous := copies[0].Designator
	for i := 0; i < 2000; i++ {
		_, next, _, err := m.InteractionDeliverySnapshot(context.Background(), w4Fed, 1, 1, []RegionHandle{source})
		if err != nil || len(next) != 1 || next[0].Designator <= previous {
			t.Fatalf("capture %d: %v %v", i, next, err)
		}
		previous = next[0].Designator
	}
	if state := m.ExportState(w4Fed); len(state.ConveyedRegions) != 0 {
		t.Fatalf("unused captures retained %d snapshots", len(state.ConveyedRegions))
	}
}

func TestConveyedFinalReleaseReclaimsCacheAndQueuedCopyRehydrates(t *testing.T) {
	m, copies := conveyedFixture(t)
	ctx := context.Background()
	handles := []RegionHandle{copies[0].Designator}
	for _, receiver := range []core.FederateHandle{2, 3} {
		if err := m.ConveyCopies(ctx, w4Fed, receiver, copies); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnterConveyedRegions(ctx, w4Fed, 3, handles); err != nil {
		t.Fatal(err)
	}
	if err := m.ReleaseConveyedRegions(ctx, w4Fed, 2, handles); err != nil {
		t.Fatal(err)
	}
	if got, err := m.QueryBoundsFor(w4Fed, 3, handles[0], 1); err != nil || got != (Range{Lower: 12, Upper: 15}) {
		t.Fatalf("other receiver was evicted: %v %v", got, err)
	}
	if err := m.ReleaseConveyedRegions(ctx, w4Fed, 3, handles); err != nil {
		t.Fatal(err)
	}
	if state := m.ExportState(w4Fed); len(state.ConveyedRegions) != 0 {
		t.Fatalf("completed copy retained: %+v", state.ConveyedRegions)
	}
	// A later callback still owns portable values, independent of the query cache.
	if err := m.ConveyCopies(ctx, w4Fed, 4, copies); err != nil {
		t.Fatal(err)
	}
	if err := m.EnterConveyedRegions(ctx, w4Fed, 4, handles); err != nil {
		t.Fatal(err)
	}
	if got, err := m.QueryBoundsFor(w4Fed, 4, handles[0], 1); err != nil || got != (Range{Lower: 12, Upper: 15}) {
		t.Fatalf("queued copy did not rehydrate: %v %v", got, err)
	}
	m.OnFederateResign(w4Fed, 4)
	if state := m.ExportState(w4Fed); len(state.ConveyedRegions) != 0 {
		t.Fatal("resign retained copied cache")
	}
}

func TestConveyedHighVolumeCompletedDeliveryCacheIsBounded(t *testing.T) {
	m, initial := conveyedFixture(t)
	ctx := context.Background()
	source := initial[0].Realization.SourceRegion()
	for i := 0; i < 2000; i++ {
		_, copies, _, err := m.InteractionDeliverySnapshot(ctx, w4Fed, 1, 1, []RegionHandle{source})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.ConveyCopies(ctx, w4Fed, 2, copies); err != nil {
			t.Fatal(err)
		}
		if err := m.ReleaseConveyedRegions(ctx, w4Fed, 2, []RegionHandle{copies[0].Designator}); err != nil {
			t.Fatal(err)
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if st := m.fed[w4Fed]; len(st.conveyed) != 0 || len(st.conveyedRecipients) != 0 {
		t.Fatalf("completed caches: values=%d recipients=%d", len(st.conveyed), len(st.conveyedRecipients))
	}
}

func TestConveyedRestoreDiscardsUnleasedCacheButReservesDesignator(t *testing.T) {
	m, copies := conveyedFixture(t)
	state := m.ExportState(w4Fed)
	state.ConveyedRegions = []ConveyedRegionState{{Designator: copies[0].Designator, Realization: copies[0].Realization}}
	m.ImportState(w4Fed, state)
	if got := m.ExportState(w4Fed); len(got.ConveyedRegions) != 0 || got.NextRegionHandle < copies[0].Designator {
		t.Fatalf("unleased restored cache or reused namespace: %+v", got)
	}
	if err := m.RestoreConveyedRegions(context.Background(), w4Fed, copies); err != nil {
		t.Fatal(err)
	}
	if got := m.ExportState(w4Fed); len(got.ConveyedRegions) != 0 {
		t.Fatal("portable restore cached values without a receiver")
	}
}
