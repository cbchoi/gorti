package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestConveyedMetadataUsesCopyDesignatorsAndEmptyPresence(t *testing.T) {
	snapshot, err := core.NewRegionRealizationSnapshot(7, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 1<<53 + 1, Upper: 1<<53 + 2}}})
	if err != nil {
		t.Fatal(err)
	}
	metadata := &core.ObjectCallbackMetadata{Regions: []core.DDMRegionHandleCore{7}, ConveyedRegions: []core.ConveyedRegionCopy{{Designator: 101, Realization: snapshot}}}
	wire := callbackMetadata(metadata)
	if wire.ReceivedRegions == nil || len(wire.ReceivedRegions.RegionHandles) != 1 || wire.ReceivedRegions.RegionHandles[0] != 101 {
		t.Fatal("wire exposes mutable source region instead of readonly copy")
	}
	metadata.ConveyedRegions = []core.ConveyedRegionCopy{}
	wire = callbackMetadata(metadata)
	if wire.ReceivedRegions == nil || len(wire.ReceivedRegions.RegionHandles) != 0 {
		t.Fatal("default-region conveyance did not preserve present-empty")
	}
	wire = callbackMetadata(&core.ObjectCallbackMetadata{})
	if wire.ReceivedRegions != nil {
		t.Fatal("absent conveyance became present")
	}
}

type readonlyDimensionQueries struct{}

func (readonlyDimensionQueries) ExportState(core.FederationName) *ddm.ManagerState { return nil }
func (readonlyDimensionQueries) ConveyedRegionDimensions(fed core.FederationName, handle ddm.RegionHandle) ([]ddm.DimensionHandle, bool) {
	if fed == "f" && handle == 101 {
		return []ddm.DimensionHandle{1, 3}, true
	}
	return nil, false
}

func TestConveyedDimensionQueriesUseReadonlyCopyPort(t *testing.T) {
	service := newSupportService(nil)
	service.regions = readonlyDimensionQueries{}
	result, err := service.GetDimensionHandleSet(context.Background(), &rtiv1.GetDimensionHandleSetRequest{WireVersion: 1, FederationName: "f", FederateHandle: 2, RegionHandle: 101})
	if err != nil || len(result.GetDimensionHandles()) != 2 || result.DimensionHandles[0] != 1 || result.DimensionHandles[1] != 3 {
		t.Fatal("readonly copied region dimensions unavailable", result, err)
	}
}

func TestConveyedRangeQueryRequiresAuthenticatedFederation(t *testing.T) {
	service := newDDMService(nil, nil)
	service.callerIdentity = func(context.Context) (core.MemberIdentity, error) {
		return core.MemberIdentity{Federation: "other", Handle: 2}, nil
	}
	_, err := service.QueryBounds(context.Background(), &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: "f", RegionHandle: 101, DimensionHandle: 1})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatal("query accepted another federation's caller", err)
	}
}
