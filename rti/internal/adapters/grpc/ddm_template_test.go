package grpc_test

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"testing"
)

func TestDDMTemplateLifecycleRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t)
	space, _ := f.regions.LookupRoutingSpace(ieeeOODDMFederation, "default")
	x, _ := f.regions.LookupDimension(ieeeOODDMFederation, space, "X")
	y, _ := f.regions.LookupDimension(ieeeOODDMFederation, space, "Y")
	r, err := f.ddmClient.CreateRegion(f.ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), RoutingSpaceHandle: uint64(space), DimensionHandles: []uint64{uint64(x), uint64(y)}})
	ieeeOODDMOK(t, err)
	var trailer metadata.MD
	_, err = f.ddmClient.QueryBounds(f.ctx, &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, RegionHandle: r.RegionHandle, DimensionHandle: uint64(x)}, grpcgo.Trailer(&trailer))
	ieeeOODDMException(t, err, trailer, "InvalidRegion")
	_, err = f.ddmClient.SetRangeBounds(f.ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), RegionHandle: r.RegionHandle, DimensionHandle: uint64(x), Bounds: &rtiv1.Range{Lower: 1, Upper: 10}})
	ieeeOODDMOK(t, err)
	trailer = nil
	_, err = f.ddmClient.CommitRegionModifications(f.ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), RegionHandles: []uint64{r.RegionHandle}}, grpcgo.Trailer(&trailer))
	ieeeOODDMException(t, err, trailer, "InvalidRegion")
	_, err = f.ddmClient.SetRangeBounds(f.ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), RegionHandle: r.RegionHandle, DimensionHandle: uint64(y), Bounds: &rtiv1.Range{Lower: 2, Upper: 20}})
	ieeeOODDMOK(t, err)
	_, err = f.ddmClient.CommitRegionModifications(f.ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), RegionHandles: []uint64{r.RegionHandle}})
	ieeeOODDMOK(t, err)
	result, err := f.ddmClient.QueryBounds(f.ctx, &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, RegionHandle: r.RegionHandle, DimensionHandle: uint64(x)})
	ieeeOODDMOK(t, err)
	if !result.Found || result.Bounds.Lower != 1 || result.Bounds.Upper != 10 {
		t.Fatalf("lost initial draft: %v", result)
	}
}
