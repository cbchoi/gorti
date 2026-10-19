package grpc

import (
	"context"
	"errors"
	"slices"
	"strings"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func validateSupportCaller(version rtiv1.WireVersion, fed string, caller uint64) error {
	if err := validateWireVersion(version); err != nil {
		return err
	}
	if strings.TrimSpace(fed) == "" || caller == 0 {
		return status.Error(codes.InvalidArgument, "federation_name and federate_handle are required")
	}
	return nil
}

func supportException(ctx context.Context, code codes.Code, name, message string) error {
	_ = grpcgo.SetTrailer(ctx, metadata.Pairs(specExceptionTrailerKey, name))
	return status.Error(code, message)
}

func (s *supportService) GetUpdateRateValue(ctx context.Context, req *rtiv1.GetUpdateRateValueRequest) (*rtiv1.GetUpdateRateValueResponse, error) {
	if req == nil {
		return nil, nilRequest("GetUpdateRateValue")
	}
	if err := validateSupportCaller(req.WireVersion, req.FederationName, req.FederateHandle); err != nil {
		return nil, err
	}
	model, _, err := s.domain.Lookup(ctx, core.FederationName(req.FederationName))
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	rates, ok := model.(core.FOMUpdateRates)
	if !ok {
		return nil, supportException(ctx, codes.Internal, rtiInternalErrorName, "update-rate metadata unavailable")
	}
	value, ok := rates.UpdateRateValue(req.UpdateRateDesignator)
	if !ok {
		return nil, supportException(ctx, codes.InvalidArgument, "InvalidUpdateRateDesignator", core.ErrInvalidUpdateRateDesignator.Error())
	}
	return &rtiv1.GetUpdateRateValueResponse{UpdateRate: value}, nil
}

func (s *supportService) GetUpdateRateValueForAttribute(ctx context.Context, req *rtiv1.GetUpdateRateValueForAttributeRequest) (*rtiv1.GetUpdateRateValueForAttributeResponse, error) {
	if req == nil {
		return nil, nilRequest("GetUpdateRateValueForAttribute")
	}
	if err := validateSupportCaller(req.WireVersion, req.FederationName, req.FederateHandle); err != nil {
		return nil, err
	}
	if s.updateRates == nil {
		return nil, supportException(ctx, codes.Internal, rtiInternalErrorName, "attribute update-rate query unavailable")
	}
	value, err := s.updateRates.GetUpdateRateValueForAttribute(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.ObjectHandle(req.ObjectHandle), core.AttributeHandle(req.AttributeHandle))
	if errors.Is(err, core.ErrUpdateRateValueUnavailable) {
		return nil, supportException(ctx, codes.Internal, rtiInternalErrorName, err.Error())
	}
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.GetUpdateRateValueForAttributeResponse{UpdateRate: value}, nil
}

func dimensionValues(dims []core.DDMDimensionHandle) []uint64 {
	result := make([]uint64, len(dims))
	for i, h := range dims {
		result[i] = uint64(h)
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func (s *supportService) GetAvailableDimensionsForClassAttribute(ctx context.Context, req *rtiv1.GetAvailableDimensionsForClassAttributeRequest) (*rtiv1.GetAvailableDimensionsForClassAttributeResponse, error) {
	if req == nil {
		return nil, nilRequest("GetAvailableDimensionsForClassAttribute")
	}
	if err := validateSupportCaller(req.WireVersion, req.FederationName, req.FederateHandle); err != nil {
		return nil, err
	}
	model, names, err := s.domain.Lookup(ctx, core.FederationName(req.FederationName))
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	cls, attr := core.ObjectClassHandle(req.ObjectClassHandle), core.AttributeHandle(req.AttributeHandle)
	if _, ok := names.ObjectClassName(cls); !ok {
		return nil, supportException(ctx, codes.NotFound, "InvalidObjectClassHandle", "unknown object class handle")
	}
	if attr == 0 {
		return nil, supportException(ctx, codes.InvalidArgument, "InvalidAttributeHandle", "zero attribute handle")
	}
	if _, ok := names.AttributeName(cls, attr); !ok {
		return nil, errToStatus(ctx, core.ErrAttributeNotFound)
	}
	dims, ok := model.(core.FOMAttributeDimensions)
	if !ok {
		return nil, supportException(ctx, codes.Internal, rtiInternalErrorName, "attribute dimension metadata unavailable")
	}
	values, ok := dims.AttributeDimensions(cls, attr)
	if !ok {
		return nil, errToStatus(ctx, core.ErrAttributeNotFound)
	}
	return &rtiv1.GetAvailableDimensionsForClassAttributeResponse{DimensionHandles: dimensionValues(values)}, nil
}

func (s *supportService) GetAvailableDimensionsForInteractionClass(ctx context.Context, req *rtiv1.GetAvailableDimensionsForInteractionClassRequest) (*rtiv1.GetAvailableDimensionsForInteractionClassResponse, error) {
	if req == nil {
		return nil, nilRequest("GetAvailableDimensionsForInteractionClass")
	}
	if err := validateSupportCaller(req.WireVersion, req.FederationName, req.FederateHandle); err != nil {
		return nil, err
	}
	model, names, err := s.domain.Lookup(ctx, core.FederationName(req.FederationName))
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	cls := core.InteractionClassHandle(req.InteractionClassHandle)
	if _, ok := names.InteractionClassName(cls); !ok {
		return nil, supportException(ctx, codes.NotFound, "InvalidInteractionClassHandle", "unknown interaction class handle")
	}
	dims, ok := model.(core.FOMInteractionDimensions)
	if !ok {
		return nil, supportException(ctx, codes.Internal, rtiInternalErrorName, "interaction dimension metadata unavailable")
	}
	values, ok := dims.InteractionDimensions(cls)
	if !ok {
		return nil, supportException(ctx, codes.NotFound, "InvalidInteractionClassHandle", "unknown interaction class handle")
	}
	return &rtiv1.GetAvailableDimensionsForInteractionClassResponse{DimensionHandles: dimensionValues(values)}, nil
}

func (s *supportService) GetDimensionHandleSet(ctx context.Context, req *rtiv1.GetDimensionHandleSetRequest) (*rtiv1.GetDimensionHandleSetResponse, error) {
	if req == nil {
		return nil, nilRequest("GetDimensionHandleSet")
	}
	if err := validateSupportCaller(req.WireVersion, req.FederationName, req.FederateHandle); err != nil {
		return nil, err
	}
	caller := core.FederateHandle(req.FederateHandle)
	if s.callerIdentity != nil {
		identity, err := s.callerIdentity(ctx)
		if err != nil {
			return nil, err
		}
		if identity.Federation != core.FederationName(req.FederationName) || identity.Handle != caller {
			return nil, status.Error(codes.PermissionDenied, "region query must use the authenticated federate")
		}
	}
	if s.regions == nil {
		return nil, supportException(ctx, codes.Internal, rtiInternalErrorName, "region dimension query unavailable")
	}
	if queries, ok := s.regions.(interface {
		RegionDimensionsFor(core.FederationName, core.FederateHandle, ddm.RegionHandle) ([]ddm.DimensionHandle, error)
	}); ok {
		dimensions, err := queries.RegionDimensionsFor(core.FederationName(req.FederationName), caller, ddm.RegionHandle(req.RegionHandle))
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.GetDimensionHandleSetResponse{DimensionHandles: dimensionValues(dimensions)}, nil
	}
	if s.callerIdentity != nil {
		return nil, status.Error(codes.Unimplemented, "authenticated region dimension query unavailable")
	}
	return s.legacyDimensionHandleSet(ctx, req)
}
func (s *supportService) legacyDimensionHandleSet(ctx context.Context, req *rtiv1.GetDimensionHandleSetRequest) (*rtiv1.GetDimensionHandleSetResponse, error) {
	if copies, ok := s.regions.(interface {
		ConveyedRegionDimensions(core.FederationName, ddm.RegionHandle) ([]ddm.DimensionHandle, bool)
	}); ok {
		if dimensions, found := copies.ConveyedRegionDimensions(core.FederationName(req.FederationName), ddm.RegionHandle(req.RegionHandle)); found {
			return &rtiv1.GetDimensionHandleSetResponse{DimensionHandles: dimensionValues(dimensions)}, nil
		}
	}
	state := s.regions.ExportState(core.FederationName(req.FederationName))
	if state != nil {
		for _, region := range state.Regions {
			if uint64(region.Handle) == req.RegionHandle {
				return &rtiv1.GetDimensionHandleSetResponse{DimensionHandles: dimensionValues(region.Dimensions)}, nil
			}
		}
	}
	return nil, errToStatus(ctx, core.ErrRegionNotFound)
}
