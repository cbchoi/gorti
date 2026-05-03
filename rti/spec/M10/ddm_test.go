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
// SCAFFOLD — full overlap-driven fan-out requires DDM + object.Registry
// integration. M10 W1 unskips after wiring.
//
// Implements: FR-DDM-4, FR-DDM-5.
func TestSpec_M10_RegionOverlap_DeterminesSubscriberFan_out(t *testing.T) {
	mgr := newTestDDMManager(t)
	if mgr == nil {
		t.Skip("ddm.Manager not yet wired")
	}
	t.Skip("Agent A wires the SubscribersForUpdate end-to-end test in M10 W1 once region store + overlap test land")
}

// TestSpec_M10_NoOverlap_DropsUpdate: when a publisher's regions don't
// overlap any subscribed region, the subscriber receives nothing.
// SCAFFOLD.
//
// Implements: FR-DDM-3, FR-DDM-4.
func TestSpec_M10_NoOverlap_DropsUpdate(t *testing.T) {
	mgr := newTestDDMManager(t)
	if mgr == nil {
		t.Skip("ddm.Manager not yet wired")
	}
	t.Skip("Agent A wires the no-overlap test in M10 W1")
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
		{ddmpkg.Range{0, 5}, ddmpkg.Range{5, 10}, false},
		{ddmpkg.Range{0, 5}, ddmpkg.Range{4, 10}, true},
		{ddmpkg.Range{0, 5}, ddmpkg.Range{5, 5}, false},
		{ddmpkg.Range{0, 5}, ddmpkg.Range{0, 5}, true},
		{ddmpkg.Range{0, 0}, ddmpkg.Range{0, 5}, false}, // empty range
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
// SCAFFOLD.
//
// Implements: FR-DDM-5.
func TestSpec_M10_DeterministicSubscriberOrder(t *testing.T) {
	mgr := newTestDDMManager(t)
	if mgr == nil {
		t.Skip("ddm.Manager not yet wired")
	}
	t.Skip("Agent A wires sort-order assertion in M10 W1")
}
