package ddm

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestObjectAttributePointRoutingCommittedContext(t *testing.T) {
	m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: realizationRepo{}, Objects: w4Objects{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	explicit := w4Region(t, m, 2, map[DimensionHandle]Range{2: {Lower: 10, Upper: 20}})
	excluded := w4Region(t, m, 3, nil)
	for _, row := range []struct {
		h core.FederateHandle
		r RegionHandle
	}{{2, explicit}, {3, excluded}} {
		if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, row.h, 1, []core.AttributeHandle{2}, []RegionHandle{row.r}); err != nil {
			t.Fatal(err)
		}
	}
	for _, point := range []uint64{10, 15, 19} {
		if got, err := m.SubscribersForObjectAttributePoint(ctx, w4Fed, 1, 2, 2, point); err != nil || !slices.Equal(got, []core.FederateHandle{2}) {
			t.Fatalf("inside point %v: %v %v", point, got, err)
		}
	}
	for _, point := range []uint64{9, 20, 100} {
		if got, err := m.SubscribersForObjectAttributePoint(ctx, w4Fed, 1, 2, 2, point); err != nil || len(got) != 0 {
			t.Fatalf("outside point %v: %v %v", point, got, err)
		}
	}
	if err := m.SetRangeBounds(w4Fed, 2, explicit, 2, Range{Lower: 30, Upper: 40}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.SubscribersForObjectAttributePoint(ctx, w4Fed, 1, 2, 2, 15); err != nil || len(got) != 1 {
		t.Fatalf("pending affected routing: %v %v", got, err)
	}
	if err := m.CommitRegionModifications(ctx, w4Fed, 2, []RegionHandle{explicit}); err != nil {
		t.Fatal(err)
	}
	if got, err := m.SubscribersForObjectAttributePoint(ctx, w4Fed, 1, 2, 2, 15); err != nil || len(got) != 0 {
		t.Fatalf("committed bounds ignored: %v %v", got, err)
	}
	if _, err := m.SubscribersForObjectAttributePoint(ctx, w4Fed, 1, 2, 1, 15); !errors.Is(err, core.ErrInvalidRegionContext) {
		t.Fatalf("unavailable point dimension: %v", err)
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, w4Fed, 4, 1, []core.AttributeHandle{2}, []RegionHandle{explicit}); !errors.Is(err, core.ErrRegionNotOwnedByFederate) {
		t.Fatalf("foreign region: %v", err)
	}
}
