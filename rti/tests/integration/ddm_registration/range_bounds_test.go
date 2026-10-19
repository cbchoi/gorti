package ddm_registration_test

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

func TestProductionSetRangeBoundsMissingBoundsPreservesPending(t *testing.T) {
	f := startFederation(t)
	region := f.region(0, 10, 20)
	check(t, f.setRange(0, region, 30, 40))
	var trailer metadata.MD
	_, err := f.ddm.SetRangeBounds(f.ctx, &rtiv1.SetRangeBoundsRequest{
		WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0],
		RegionHandle: region, DimensionHandle: f.dimension,
	}, grpc.Trailer(&trailer))
	expectRPCError(t, err, trailer, codes.InvalidArgument, "InvalidRangeBound")
	f.bounds(region, 10, 20)
	f.commit(0, region)
	f.bounds(region, 30, 40)
}
