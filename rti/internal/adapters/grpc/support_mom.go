package grpc

import (
	"context"
	"math"
	"slices"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *supportService) ReportCallbackInvocation(ctx context.Context, req *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("ReportCallbackInvocation")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.GetInvocationEntry() && (!req.GetSuccess() || req.GetException() != "") {
		return nil, status.Error(codes.InvalidArgument, "callback entry requires success=true and no exception")
	}
	if s.callbackReceipts == nil {
		return nil, status.Error(codes.Unimplemented, "callback receipt receiver unavailable")
	}
	if req.GetInvocationEntry() {
		if err := s.callbackReceipts.ReportEntry(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), req.GetCallbackReceipt()); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if err := s.callbackReceipts.Report(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), req.GetCallbackReceipt(), req.GetSuccess(), req.GetException()); err != nil {
		return nil, err
	}
	return &rtiv1.Empty{}, nil
}

func (s *supportService) normalizationRoster(fed string, caller uint64) ([]core.FederateHandle, error) {
	if fed == "" || caller == 0 {
		return nil, status.Error(codes.InvalidArgument, "federation and caller required")
	}
	if s.normalizationMembers == nil {
		return nil, status.Error(codes.Unimplemented, "normalization membership source unavailable")
	}
	roster := s.normalizationMembers(core.FederationName(fed))
	if !slices.Contains(roster, core.FederateHandle(caller)) {
		return nil, status.Error(codes.FailedPrecondition, "federate not joined")
	}
	return roster, nil
}

// Normalization is the per-execution local index after exact live-handle
// validation. This remains representable by the C++ LLP64 unsigned-long API.
func (s *supportService) NormalizeFederateHandle(ctx context.Context, req *rtiv1.NormalizeFederateHandleRequest) (*rtiv1.NormalizeFederateHandleResponse, error) {
	if req == nil {
		return nil, nilRequest("NormalizeFederateHandle")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	roster, err := s.normalizationRoster(req.GetFederationName(), req.GetFederateHandle())
	if err != nil {
		return nil, err
	}
	if req.GetTargetHandle() == 0 || !slices.Contains(roster, core.FederateHandle(req.GetTargetHandle())) {
		return nil, supportException(ctx, codes.InvalidArgument, "InvalidFederateHandle", "invalid federate handle")
	}
	return &rtiv1.NormalizeFederateHandleResponse{Value: req.GetTargetHandle() & math.MaxUint32}, nil
}

func (s *supportService) NormalizeServiceGroup(ctx context.Context, req *rtiv1.NormalizeServiceGroupRequest) (*rtiv1.NormalizeServiceGroupResponse, error) {
	if req == nil {
		return nil, nilRequest("NormalizeServiceGroup")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if _, err := s.normalizationRoster(req.GetFederationName(), req.GetFederateHandle()); err != nil {
		return nil, err
	}
	if req.GetServiceGroup() > 6 {
		return nil, supportException(ctx, codes.InvalidArgument, "InvalidServiceGroup", "invalid service group")
	}
	return &rtiv1.NormalizeServiceGroupResponse{Value: uint64(req.GetServiceGroup())}, nil
}
