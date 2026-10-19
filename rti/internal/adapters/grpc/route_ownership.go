package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type routedOwnershipService struct {
	rtiv1.UnimplementedOwnershipServiceServer
	router   *federationTimeRouter
	floats   rtiv1.OwnershipServiceServer
	integers rtiv1.OwnershipServiceServer
}

func (s *routedOwnershipService) UnconditionalAttributeOwnershipDivestiture(ctx context.Context, req *rtiv1.UnconditionalDivestRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.UnconditionalAttributeOwnershipDivestiture(ctx, req)
}

func (s *routedOwnershipService) NegotiatedAttributeOwnershipDivestiture(ctx context.Context, req *rtiv1.NegotiatedDivestRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.NegotiatedAttributeOwnershipDivestiture(ctx, req)
}

func (s *routedOwnershipService) AttributeOwnershipAcquisition(ctx context.Context, req *rtiv1.AcquireRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.AttributeOwnershipAcquisition(ctx, req)
}

func (s *routedOwnershipService) CancelNegotiatedAttributeOwnershipDivestiture(ctx context.Context, req *rtiv1.CancelDivestRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.CancelNegotiatedAttributeOwnershipDivestiture(ctx, req)
}

func (s *routedOwnershipService) CancelAttributeOwnershipAcquisition(ctx context.Context, req *rtiv1.CancelAcquireRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.CancelAttributeOwnershipAcquisition(ctx, req)
}

func (s *routedOwnershipService) AttributeOwnershipDivestitureIfWanted(ctx context.Context, req *rtiv1.DivestIfWantedRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.AttributeOwnershipDivestitureIfWanted(ctx, req)
}

func (s *routedOwnershipService) AttributeOwnershipDivestitureIfWantedWithResult(ctx context.Context, req *rtiv1.DivestIfWantedRequest) (*rtiv1.DivestIfWantedResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.AttributeOwnershipDivestitureIfWantedWithResult(ctx, req)
}

func (s *routedOwnershipService) ConfirmDivestiture(ctx context.Context, req *rtiv1.ConfirmDivestitureRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ConfirmDivestiture(ctx, req)
}

func (s *routedOwnershipService) QueryAttributeOwnership(ctx context.Context, req *rtiv1.QueryOwnershipRequest) (*rtiv1.QueryOwnershipResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryAttributeOwnership(ctx, req)
}

func (s *routedOwnershipService) IsAttributeOwnedByFederate(ctx context.Context, req *rtiv1.IsOwnedRequest) (*rtiv1.IsOwnedResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.IsAttributeOwnedByFederate(ctx, req)
}

func (s *routedOwnershipService) AttributeOwnershipReleaseDenied(ctx context.Context, req *rtiv1.ReleaseDeniedRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.AttributeOwnershipReleaseDenied(ctx, req)
}
