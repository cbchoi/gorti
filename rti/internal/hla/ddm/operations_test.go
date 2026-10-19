package ddm

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestDDMForeignSubscriptionPreservesState(t *testing.T) {
	m, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatal(err)
	}
	space, _ := m.LookupRoutingSpace("fed", "default")
	dim, _ := m.LookupDimension("fed", space, "X")
	ctx := context.Background()
	region, err := m.CreateRegion(ctx, "fed", 1, space, []DimensionHandle{dim})
	if err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("fed")
	for _, operation := range []func() error{
		func() error {
			return m.SubscribeObjectClassAttributesWithRegions(ctx, "fed", 2, 1, []core.AttributeHandle{1}, []RegionHandle{region})
		},
		func() error { return m.SubscribeInteractionClassWithRegions(ctx, "fed", 2, 1, []RegionHandle{region}) },
		func() error {
			return m.UnsubscribeObjectClassAttributesWithRegions(ctx, "fed", 2, 1, []core.AttributeHandle{1}, []RegionHandle{region})
		},
		func() error {
			return m.UnsubscribeInteractionClassWithRegions(ctx, "fed", 2, 1, []RegionHandle{region})
		},
	} {
		if err := operation(); !errors.Is(err, core.ErrRegionNotOwnedByFederate) {
			t.Errorf("foreign region accepted: %v", err)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) {
			t.Fatal("foreign region changed subscription state")
		}
	}
}
