package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ownershipQueryPort keeps object validation and the ownership snapshot in one
// operation without expanding the ownership coordinator's mutation interface.
type ownershipQueryPort interface {
	QueryAttributeOwnership(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, core.AttributeHandle) (core.FederateHandle, bool, error)
}

// QueryAttributeOwnership implements IEEE 1516.1-2010 section 7.17.
func (s *ownershipService) QueryAttributeOwnership(ctx context.Context, req *rtiv1.QueryOwnershipRequest) (*rtiv1.QueryOwnershipResponse, error) {
	if req == nil {
		return nil, nilRequest("QueryAttributeOwnership")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	fed, obj, attr := core.FederationName(req.GetFederationName()), core.ObjectHandle(req.GetObjectHandle()), core.AttributeHandle(req.GetAttributeHandle())
	var owner core.FederateHandle
	var owned bool
	if queries, ok := s.objects.(ownershipQueryPort); ok {
		var err error
		owner, owned, err = queries.QueryAttributeOwnership(ctx, fed, core.FederateHandle(req.GetFederateHandle()), obj, attr)
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
	} else {
		owner, owned = s.mgr.QueryOwnership(fed, obj, attr)
	}
	resp := &rtiv1.QueryOwnershipResponse{Owned: owned}
	if owned {
		resp.OwnerFederateHandle = uint64(owner)
	}
	return resp, nil
}

// IsAttributeOwnedByFederate implements IEEE 1516.1-2010 section 7.19.
func (s *ownershipService) IsAttributeOwnedByFederate(ctx context.Context, req *rtiv1.IsOwnedRequest) (*rtiv1.IsOwnedResponse, error) {
	if req == nil {
		return nil, nilRequest("IsAttributeOwnedByFederate")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	fed, caller := core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle())
	obj, attr := core.ObjectHandle(req.GetObjectHandle()), core.AttributeHandle(req.GetAttributeHandle())
	var owned bool
	if queries, ok := s.objects.(ownershipQueryPort); ok {
		owner, found, err := queries.QueryAttributeOwnership(ctx, fed, caller, obj, attr)
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		owned = found && owner == caller
	} else {
		owned = s.mgr.IsOwnedBy(fed, caller, obj, attr)
	}
	return &rtiv1.IsOwnedResponse{Owned: owned}, nil
}
