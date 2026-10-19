package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type routedDDMService struct {
	rtiv1.UnimplementedDDMServiceServer
	router   *federationTimeRouter
	floats   rtiv1.DDMServiceServer
	integers rtiv1.DDMServiceServer
}

func (s *routedDDMService) LookupRoutingSpace(ctx context.Context, req *rtiv1.LookupRoutingSpaceRequest) (*rtiv1.LookupRoutingSpaceResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.LookupRoutingSpace(ctx, req)
}

func (s *routedDDMService) LookupDimension(ctx context.Context, req *rtiv1.LookupDimensionRequest) (*rtiv1.LookupDimensionResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.LookupDimension(ctx, req)
}

func (s *routedDDMService) CreateRegion(ctx context.Context, req *rtiv1.CreateRegionRequest) (*rtiv1.CreateRegionResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.CreateRegion(ctx, req)
}

func (s *routedDDMService) SetRangeBounds(ctx context.Context, req *rtiv1.SetRangeBoundsRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.SetRangeBounds(ctx, req)
}

func (s *routedDDMService) CommitRegionModifications(ctx context.Context, req *rtiv1.CommitRegionRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.CommitRegionModifications(ctx, req)
}

func (s *routedDDMService) DeleteRegion(ctx context.Context, req *rtiv1.DeleteRegionRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.DeleteRegion(ctx, req)
}

func (s *routedDDMService) QueryBounds(ctx context.Context, req *rtiv1.QueryBoundsRequest) (*rtiv1.QueryBoundsResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryBounds(ctx, req)
}

func (s *routedDDMService) SubscribeObjectClassAttributesWithRegions(ctx context.Context, req *rtiv1.SubscribeOCAWithRegionsRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.SubscribeObjectClassAttributesWithRegions(ctx, req)
}

func (s *routedDDMService) SubscribeInteractionClassWithRegions(ctx context.Context, req *rtiv1.SubscribeICWithRegionsRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.SubscribeInteractionClassWithRegions(ctx, req)
}

func (s *routedDDMService) RegisterObjectInstanceWithRegions(ctx context.Context, req *rtiv1.RegisterObjectWithRegionsRequest) (*rtiv1.RegisterObjectWithRegionsResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RegisterObjectInstanceWithRegions(ctx, req)
}

func (s *routedDDMService) AssociateRegionsForUpdates(ctx context.Context, req *rtiv1.AssociateRegionsForUpdatesRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.AssociateRegionsForUpdates(ctx, req)
}

func (s *routedDDMService) UnassociateRegionsForUpdates(ctx context.Context, req *rtiv1.UnassociateRegionsForUpdatesRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.UnassociateRegionsForUpdates(ctx, req)
}

func (s *routedDDMService) UnsubscribeObjectClassAttributesWithRegions(ctx context.Context, req *rtiv1.UnsubscribeOCAWithRegionsRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.UnsubscribeObjectClassAttributesWithRegions(ctx, req)
}

func (s *routedDDMService) UnsubscribeInteractionClassWithRegions(ctx context.Context, req *rtiv1.UnsubscribeICWithRegionsRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.UnsubscribeInteractionClassWithRegions(ctx, req)
}

func (s *routedDDMService) SendInteractionWithRegions(ctx context.Context, req *rtiv1.SendInteractionWithRegionsRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.SendInteractionWithRegions(ctx, req)
}

func (s *routedDDMService) RequestAttributeValueUpdateWithRegions(ctx context.Context, req *rtiv1.RequestAttributeValueUpdateWithRegionsRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RequestAttributeValueUpdateWithRegions(ctx, req)
}
