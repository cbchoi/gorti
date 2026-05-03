package ddm

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// BenchmarkSubscribersForUpdate_ZeroCost: the FR-DDM-6 zero-cost path.
// SubscribersForUpdate with empty publisherRegions must return nil
// without taking any locks or hitting the per-federation map.
func BenchmarkSubscribersForUpdate_ZeroCost(b *testing.B) {
	mgr := newPermissiveManagerB(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.SubscribersForUpdate("nope", 1, 1, nil)
	}
}

// BenchmarkSubscribersForUpdate_Size25_100Regions: the FR-DDM-6
// reference workload. 25 federate subscribers × 100 regions/sub × 1
// publisher × 100 publisher regions on a 2-dimension routing space.
//
// Two sub-benchmarks:
//   - "with_regions": every subscriber has region-scoped subscriptions;
//     the manager runs the O(P*S) overlap check per call.
//   - "without_regions": same federation size, but no regions are
//     associated with the published object; the manager fast-paths
//     to nil and the registry takes the cut-1 path. Demonstrates the
//     FR-DDM-6 zero-cost-when-empty contract by direct comparison.
func BenchmarkSubscribersForUpdate_Size25_100Regions(b *testing.B) {
	const (
		fed         core.FederationName = "perf"
		publisher   core.FederateHandle = 1
		cls         core.ObjectClassHandle = 1
		attr        core.AttributeHandle = 1
		nFederates                      = 25
		nRegionsPer                     = 4 // 25 * 4 = 100 subscriber regions total
	)
	mgr := newPermissiveManagerB(b)
	ctx := context.Background()
	space, _ := mgr.LookupRoutingSpace(fed, "GeoSpace")
	dimX, _ := mgr.LookupDimension(fed, space, "X")
	dimY, _ := mgr.LookupDimension(fed, space, "Y")

	// Publisher: 100 regions, each scoped on both X+Y.
	pubRegions := make([]RegionHandle, 0, 100)
	for i := 0; i < 100; i++ {
		rh, err := mgr.CreateRegion(ctx, fed, publisher, space, []DimensionHandle{dimX, dimY})
		if err != nil {
			b.Fatalf("CreateRegion publisher: %v", err)
		}
		_ = mgr.SetRangeBounds(fed, publisher, rh, dimX, Range{Lower: uint64(i * 10), Upper: uint64(i*10 + 100)})
		_ = mgr.SetRangeBounds(fed, publisher, rh, dimY, Range{Lower: uint64(i * 10), Upper: uint64(i*10 + 100)})
		pubRegions = append(pubRegions, rh)
	}
	if err := mgr.CommitRegionModifications(ctx, fed, publisher, pubRegions); err != nil {
		b.Fatalf("Commit publisher: %v", err)
	}

	// 25 subscribers × 4 regions each.
	for f := 0; f < nFederates; f++ {
		sub := core.FederateHandle(f + 2) // 2..26 (federate 1 is the publisher)
		regions := make([]RegionHandle, 0, nRegionsPer)
		for r := 0; r < nRegionsPer; r++ {
			rh, err := mgr.CreateRegion(ctx, fed, sub, space, []DimensionHandle{dimX, dimY})
			if err != nil {
				b.Fatalf("CreateRegion sub %d: %v", sub, err)
			}
			// Spread subscriber regions across the routing space so
			// some overlap publisher regions and some don't.
			lo := uint64((f*100 + r*25) % 1000)
			_ = mgr.SetRangeBounds(fed, sub, rh, dimX, Range{Lower: lo, Upper: lo + 50})
			_ = mgr.SetRangeBounds(fed, sub, rh, dimY, Range{Lower: lo, Upper: lo + 50})
			regions = append(regions, rh)
		}
		if err := mgr.CommitRegionModifications(ctx, fed, sub, regions); err != nil {
			b.Fatalf("Commit sub %d: %v", sub, err)
		}
		if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, sub, cls,
			[]core.AttributeHandle{attr}, regions); err != nil {
			b.Fatalf("Subscribe sub %d: %v", sub, err)
		}
	}

	b.Run("with_regions", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = mgr.SubscribersForUpdate(fed, cls, attr, pubRegions)
		}
	})
	b.Run("without_regions", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = mgr.SubscribersForUpdate(fed, cls, attr, nil)
		}
	})
}

// newPermissiveManagerB is the *testing.B analogue of
// newPermissiveManager. Duplicated to keep test/bench helpers aligned
// without importing test code from bench code.
func newPermissiveManagerB(b *testing.B) *Manager {
	b.Helper()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	return mgr
}
