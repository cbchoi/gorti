package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type routedSavepointService struct {
	rtiv1.UnimplementedSavepointServiceServer
	router   *federationTimeRouter
	floats   rtiv1.SavepointServiceServer
	integers rtiv1.SavepointServiceServer
}

func (s *routedSavepointService) RequestFederationSave(ctx context.Context, req *rtiv1.RequestFederationSaveRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RequestFederationSave(ctx, req)
}

func (s *routedSavepointService) FederateSaveComplete(ctx context.Context, req *rtiv1.FederateSaveResponseRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.FederateSaveComplete(ctx, req)
}

func (s *routedSavepointService) FederateSaveNotComplete(ctx context.Context, req *rtiv1.FederateSaveResponseRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.FederateSaveNotComplete(ctx, req)
}

func (s *routedSavepointService) QuerySaveState(ctx context.Context, req *rtiv1.QuerySaveStateRequest) (*rtiv1.QuerySaveStateResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QuerySaveState(ctx, req)
}

func (s *routedSavepointService) RequestFederationRestore(ctx context.Context, req *rtiv1.RequestFederationRestoreRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RequestFederationRestore(ctx, req)
}

func (s *routedSavepointService) FederateRestoreComplete(ctx context.Context, req *rtiv1.FederateRestoreResponseRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.FederateRestoreComplete(ctx, req)
}

func (s *routedSavepointService) QueryRestoreState(ctx context.Context, req *rtiv1.QueryRestoreStateRequest) (*rtiv1.QueryRestoreStateResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryRestoreState(ctx, req)
}

func (s *routedSavepointService) AbortFederationSave(ctx context.Context, req *rtiv1.AbortFederationSaveRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.AbortFederationSave(ctx, req)
}

func (s *routedSavepointService) AbortFederationRestore(ctx context.Context, req *rtiv1.AbortFederationRestoreRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.AbortFederationRestore(ctx, req)
}

func (s *routedSavepointService) FederateSaveBegun(ctx context.Context, req *rtiv1.FederateSaveResponseRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.FederateSaveBegun(ctx, req)
}

func (s *routedSavepointService) FederateRestoreNotComplete(ctx context.Context, req *rtiv1.FederateRestoreResponseRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.FederateRestoreNotComplete(ctx, req)
}
