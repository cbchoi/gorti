// OwnershipService gRPC handler — translates rti.v1.OwnershipService
// RPCs into calls on rti/internal/ownership.Manager.
//
// Owner: Agent A — M12 W1 (cut-3 gRPC exposure of cut-2 ownership.Manager).
//
// Composition: server.go wires an *ownershipService into the composed
// Server via newOwnershipService(*ownership.Manager).

package grpc

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ownershipService is the concrete OwnershipServiceServer impl.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.2): the handler binds to core.OwnershipCoordinator instead of the
// concrete *ownership.Manager so alternative implementations can be
// wired in at the composition root.
type ownershipService struct {
	rtiv1.UnimplementedOwnershipServiceServer
	mgr core.OwnershipCoordinator
}

func newOwnershipService(mgr core.OwnershipCoordinator) *ownershipService {
	return &ownershipService{mgr: mgr}
}

// UnconditionalAttributeOwnershipDivestiture implements §7.2.
func (s *ownershipService) UnconditionalAttributeOwnershipDivestiture(
	ctx context.Context,
	req *rtiv1.UnconditionalDivestRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnconditionalAttributeOwnershipDivestiture")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.UnconditionalDivest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// NegotiatedAttributeOwnershipDivestiture implements §7.3.
func (s *ownershipService) NegotiatedAttributeOwnershipDivestiture(
	ctx context.Context,
	req *rtiv1.NegotiatedDivestRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("NegotiatedAttributeOwnershipDivestiture")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.NegotiatedDivest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrHandles(req.GetAttributeHandles()),
		req.GetTag(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// AttributeOwnershipAcquisition implements §7.4.
func (s *ownershipService) AttributeOwnershipAcquisition(
	ctx context.Context,
	req *rtiv1.AcquireRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("AttributeOwnershipAcquisition")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.Acquire(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrHandles(req.GetAttributeHandles()),
		req.GetTag(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// CancelNegotiatedAttributeOwnershipDivestiture implements §7.5.
func (s *ownershipService) CancelNegotiatedAttributeOwnershipDivestiture(
	ctx context.Context,
	req *rtiv1.CancelDivestRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("CancelNegotiatedAttributeOwnershipDivestiture")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.CancelDivest(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// CancelAttributeOwnershipAcquisition implements §7.6.
func (s *ownershipService) CancelAttributeOwnershipAcquisition(
	ctx context.Context,
	req *rtiv1.CancelAcquireRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("CancelAttributeOwnershipAcquisition")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.CancelAcquire(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// AttributeOwnershipDivestitureIfWanted implements §7.7.
func (s *ownershipService) AttributeOwnershipDivestitureIfWanted(
	ctx context.Context,
	req *rtiv1.DivestIfWantedRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("AttributeOwnershipDivestitureIfWanted")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.DivestIfWanted(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// QueryAttributeOwnership implements §7.8.
func (s *ownershipService) QueryAttributeOwnership(
	_ context.Context,
	req *rtiv1.QueryOwnershipRequest,
) (*rtiv1.QueryOwnershipResponse, error) {
	if req == nil {
		return nil, nilRequest("QueryAttributeOwnership")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	owner, ok := s.mgr.QueryOwnership(
		core.FederationName(req.GetFederationName()),
		core.ObjectHandle(req.GetObjectHandle()),
		core.AttributeHandle(req.GetAttributeHandle()),
	)
	resp := &rtiv1.QueryOwnershipResponse{Owned: ok}
	if ok {
		resp.OwnerFederateHandle = uint64(owner)
	}
	return resp, nil
}

// IsAttributeOwnedByFederate implements §7.9.
func (s *ownershipService) IsAttributeOwnedByFederate(
	_ context.Context,
	req *rtiv1.IsOwnedRequest,
) (*rtiv1.IsOwnedResponse, error) {
	if req == nil {
		return nil, nilRequest("IsAttributeOwnedByFederate")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	owned := s.mgr.IsOwnedBy(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		core.AttributeHandle(req.GetAttributeHandle()),
	)
	return &rtiv1.IsOwnedResponse{Owned: owned}, nil
}
