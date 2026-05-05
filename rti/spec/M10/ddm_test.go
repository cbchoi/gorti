package m10spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	ddmpkg "github.com/cbchoi/gorti/rti/internal/ddm"
)

func newTestDDMManager(t *testing.T) *ddmpkg.Manager {
	t.Helper()
	mgr, err := ddmpkg.New(ddmpkg.Options{
		Outbox: newFakeOutbox(),
		FOMs:   newPermissiveFOMRepo(),
	})
	if err != nil && !errors.Is(err, ddmpkg.ErrNotImplemented) {
		t.Logf("ddm.New returned: %v (expected during pre-dispatch)", err)
	}
	return mgr
}

// TestSpec_M10_RegionLifecycle_CreateCommitDelete: a region can be
// created, its bounds set + committed, queried back, and deleted.
//
// Implements: FR-DDM-2.
func TestSpec_M10_RegionLifecycle_CreateCommitDelete(t *testing.T) {
	mgr := newTestDDMManager(t)
	if mgr == nil {
		t.Skip("ddm.Manager not yet wired (M10 RED state)")
	}
	ctx := context.Background()
	const fed core.FederationName = "ddm-test"
	const owner core.FederateHandle = 1

	// Resolve a routing-space + dimension from the (permissive) FOM.
	// Real FOM has these names in <dimensions>; the permissive stub
	// accepts any name and returns handle 1.
	space, ok := mgr.LookupRoutingSpace(fed, "GeoSpace")
	if !ok {
		t.Skip("LookupRoutingSpace not yet wired")
	}
	dim, ok := mgr.LookupDimension(fed, space, "X")
	if !ok {
		t.Skip("LookupDimension not yet wired")
	}

	region, err := mgr.CreateRegion(ctx, fed, owner, space, []ddmpkg.DimensionHandle{dim})
	if errors.Is(err, ddmpkg.ErrNotImplemented) {
		t.Skip("CreateRegion not yet implemented")
	}
	if err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}

	if err := mgr.SetRangeBounds(fed, owner, region, dim, ddmpkg.Range{Lower: 10, Upper: 20}); err != nil {
		t.Fatalf("SetRangeBounds: %v", err)
	}
	if err := mgr.CommitRegionModifications(ctx, fed, owner, []ddmpkg.RegionHandle{region}); err != nil {
		t.Fatalf("CommitRegionModifications: %v", err)
	}

	bounds, ok := mgr.QueryBounds(fed, region, dim)
	if !ok {
		t.Fatalf("QueryBounds returned !ok after commit")
	}
	if bounds.Lower != 10 || bounds.Upper != 20 {
		t.Errorf("QueryBounds = [%d, %d), want [10, 20)", bounds.Lower, bounds.Upper)
	}

	if err := mgr.DeleteRegion(ctx, fed, owner, region); err != nil {
		t.Fatalf("DeleteRegion: %v", err)
	}
}

// TestSpec_M10_RegionOverlap_DeterminesSubscriberFan_out: when
// publisher region A and subscriber region B overlap, B is in the
// subscriber set; when they don't, B is excluded.
//
// Implements: FR-DDM-4, FR-DDM-5.
func TestSpec_M10_RegionOverlap_DeterminesSubscriberFan_out(t *testing.T) {
	mgr := newTestDDMManager(t)
	if mgr == nil {
		t.Skip("ddm.Manager not yet wired")
	}
	ctx := context.Background()
	const fed core.FederationName = "ddm-overlap"
	const publisher core.FederateHandle = 1
	const subInside core.FederateHandle = 2
	const subOutside core.FederateHandle = 3
	const cls core.ObjectClassHandle = 1
	const attr core.AttributeHandle = 1

	space, ok := mgr.LookupRoutingSpace(fed, "GeoSpace")
	if !ok {
		t.Skip("LookupRoutingSpace not yet wired")
	}
	dim, ok := mgr.LookupDimension(fed, space, "X")
	if !ok {
		t.Skip("LookupDimension not yet wired")
	}

	// Publisher region: [10, 20).
	pubR, err := mgr.CreateRegion(ctx, fed, publisher, space, []ddmpkg.DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion publisher: %v", err)
	}
	if err := mgr.SetRangeBounds(fed, publisher, pubR, dim, ddmpkg.Range{Lower: 10, Upper: 20}); err != nil {
		t.Fatalf("SetRangeBounds publisher: %v", err)
	}
	if err := mgr.CommitRegionModifications(ctx, fed, publisher, []ddmpkg.RegionHandle{pubR}); err != nil {
		t.Fatalf("CommitRegionModifications publisher: %v", err)
	}

	// Subscriber inside: [15, 25) — overlaps publisher.
	insideR, err := mgr.CreateRegion(ctx, fed, subInside, space, []ddmpkg.DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion subInside: %v", err)
	}
	if err := mgr.SetRangeBounds(fed, subInside, insideR, dim, ddmpkg.Range{Lower: 15, Upper: 25}); err != nil {
		t.Fatalf("SetRangeBounds subInside: %v", err)
	}
	if err := mgr.CommitRegionModifications(ctx, fed, subInside, []ddmpkg.RegionHandle{insideR}); err != nil {
		t.Fatalf("CommitRegionModifications subInside: %v", err)
	}

	// Subscriber outside: [100, 200) — no overlap.
	outsideR, err := mgr.CreateRegion(ctx, fed, subOutside, space, []ddmpkg.DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion subOutside: %v", err)
	}
	if err := mgr.SetRangeBounds(fed, subOutside, outsideR, dim, ddmpkg.Range{Lower: 100, Upper: 200}); err != nil {
		t.Fatalf("SetRangeBounds subOutside: %v", err)
	}
	if err := mgr.CommitRegionModifications(ctx, fed, subOutside, []ddmpkg.RegionHandle{outsideR}); err != nil {
		t.Fatalf("CommitRegionModifications subOutside: %v", err)
	}

	if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, subInside, cls,
		[]core.AttributeHandle{attr}, []ddmpkg.RegionHandle{insideR}); err != nil {
		t.Fatalf("Subscribe inside: %v", err)
	}
	if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, subOutside, cls,
		[]core.AttributeHandle{attr}, []ddmpkg.RegionHandle{outsideR}); err != nil {
		t.Fatalf("Subscribe outside: %v", err)
	}

	got := mgr.SubscribersForUpdate(fed, cls, attr, []ddmpkg.RegionHandle{pubR})
	if len(got) != 1 || got[0] != subInside {
		t.Errorf("SubscribersForUpdate = %v, want [%d]", got, subInside)
	}
}

// TestSpec_M10_NoOverlap_DropsUpdate: when a publisher's regions don't
// overlap any subscribed region, the subscriber set is empty (the
// update is dropped).
//
// Implements: FR-DDM-3, FR-DDM-4.
func TestSpec_M10_NoOverlap_DropsUpdate(t *testing.T) {
	mgr := newTestDDMManager(t)
	if mgr == nil {
		t.Skip("ddm.Manager not yet wired")
	}
	ctx := context.Background()
	const fed core.FederationName = "ddm-no-overlap"
	const publisher core.FederateHandle = 1
	const subscriber core.FederateHandle = 2
	const cls core.ObjectClassHandle = 1
	const attr core.AttributeHandle = 1

	space, ok := mgr.LookupRoutingSpace(fed, "GeoSpace")
	if !ok {
		t.Skip("LookupRoutingSpace not yet wired")
	}
	dim, ok := mgr.LookupDimension(fed, space, "X")
	if !ok {
		t.Skip("LookupDimension not yet wired")
	}

	pubR, err := mgr.CreateRegion(ctx, fed, publisher, space, []ddmpkg.DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion publisher: %v", err)
	}
	if err := mgr.SetRangeBounds(fed, publisher, pubR, dim, ddmpkg.Range{Lower: 0, Upper: 10}); err != nil {
		t.Fatalf("SetRangeBounds publisher: %v", err)
	}
	if err := mgr.CommitRegionModifications(ctx, fed, publisher, []ddmpkg.RegionHandle{pubR}); err != nil {
		t.Fatalf("CommitRegionModifications publisher: %v", err)
	}

	subR, err := mgr.CreateRegion(ctx, fed, subscriber, space, []ddmpkg.DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion subscriber: %v", err)
	}
	// Closed-open: [10, 20) does NOT touch [0, 10).
	if err := mgr.SetRangeBounds(fed, subscriber, subR, dim, ddmpkg.Range{Lower: 10, Upper: 20}); err != nil {
		t.Fatalf("SetRangeBounds subscriber: %v", err)
	}
	if err := mgr.CommitRegionModifications(ctx, fed, subscriber, []ddmpkg.RegionHandle{subR}); err != nil {
		t.Fatalf("CommitRegionModifications subscriber: %v", err)
	}
	if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, subscriber, cls,
		[]core.AttributeHandle{attr}, []ddmpkg.RegionHandle{subR}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	got := mgr.SubscribersForUpdate(fed, cls, attr, []ddmpkg.RegionHandle{pubR})
	if len(got) != 0 {
		t.Errorf("SubscribersForUpdate = %v, want empty (no overlap)", got)
	}
}

// TestSpec_M10_RangeOverlap_ClosedOpen: the Range.Overlap helper
// computes closed-open interval overlap correctly: [0,5) and [5,10) do
// NOT overlap; [0,5) and [4,10) do.
//
// Implements: FR-DDM-5.
func TestSpec_M10_RangeOverlap_ClosedOpen(t *testing.T) {
	cases := []struct {
		a, b ddmpkg.Range
		want bool
	}{
		{ddmpkg.Range{Lower: 0, Upper: 5}, ddmpkg.Range{Lower: 5, Upper: 10}, false},
		{ddmpkg.Range{Lower: 0, Upper: 5}, ddmpkg.Range{Lower: 4, Upper: 10}, true},
		{ddmpkg.Range{Lower: 0, Upper: 5}, ddmpkg.Range{Lower: 5, Upper: 5}, false},
		{ddmpkg.Range{Lower: 0, Upper: 5}, ddmpkg.Range{Lower: 0, Upper: 5}, true},
		{ddmpkg.Range{Lower: 0, Upper: 0}, ddmpkg.Range{Lower: 0, Upper: 5}, false}, // empty range
	}
	for _, tc := range cases {
		got := tc.a.Overlap(tc.b)
		if got != tc.want {
			t.Errorf("Range%v.Overlap(%v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		// Symmetric
		gotSym := tc.b.Overlap(tc.a)
		if gotSym != tc.want {
			t.Errorf("Range%v.Overlap(%v) [symmetric] = %v, want %v", tc.b, tc.a, gotSym, tc.want)
		}
	}
}

// TestSpec_M10_DeterministicSubscriberOrder: SubscribersForUpdate
// returns federate handles in sorted order (NFR-DET-1).
//
// Implements: FR-DDM-5.
func TestSpec_M10_DeterministicSubscriberOrder(t *testing.T) {
	mgr := newTestDDMManager(t)
	if mgr == nil {
		t.Skip("ddm.Manager not yet wired")
	}
	ctx := context.Background()
	const fed core.FederationName = "ddm-determinism"
	const publisher core.FederateHandle = 1
	const cls core.ObjectClassHandle = 1
	const attr core.AttributeHandle = 1

	space, ok := mgr.LookupRoutingSpace(fed, "GeoSpace")
	if !ok {
		t.Skip("LookupRoutingSpace not yet wired")
	}
	dim, ok := mgr.LookupDimension(fed, space, "X")
	if !ok {
		t.Skip("LookupDimension not yet wired")
	}

	pubR, err := mgr.CreateRegion(ctx, fed, publisher, space, []ddmpkg.DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion publisher: %v", err)
	}
	// Subscribers 7, 3, 9, 5 — out of insertion order to prove the
	// returned slice is sorted by handle, not insertion.
	subscribers := []core.FederateHandle{7, 3, 9, 5}
	for _, sub := range subscribers {
		rh, err := mgr.CreateRegion(ctx, fed, sub, space, []ddmpkg.DimensionHandle{dim})
		if err != nil {
			t.Fatalf("CreateRegion subscriber %d: %v", sub, err)
		}
		// All overlap the publisher's full-range default region.
		if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, sub, cls,
			[]core.AttributeHandle{attr}, []ddmpkg.RegionHandle{rh}); err != nil {
			t.Fatalf("Subscribe %d: %v", sub, err)
		}
	}
	got := mgr.SubscribersForUpdate(fed, cls, attr, []ddmpkg.RegionHandle{pubR})
	want := []core.FederateHandle{3, 5, 7, 9}
	if len(got) != len(want) {
		t.Fatalf("SubscribersForUpdate len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SubscribersForUpdate[%d] = %d, want %d (full = %v)", i, got[i], want[i], got)
		}
	}
}
