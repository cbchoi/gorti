package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *ownershipService) AttributeOwnershipReleaseDenied(ctx context.Context, req *rtiv1.ReleaseDeniedRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("AttributeOwnershipReleaseDenied")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if _, handled, err := s.executeKnownOwnership(ctx, req, core.OwnershipReleaseDenied, nil); handled {
		if err != nil {
			return nil, err
		}
		return &rtiv1.Empty{}, nil
	}
	denier, ok := s.mgr.(core.OwnershipReleaseDenier)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "ownership coordinator does not support release denial")
	}
	attrs, err := s.mapOwnershipAttributes(ctx, req)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if err := denier.ReleaseDenied(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectHandle(req.GetObjectHandle()), attrs); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}
