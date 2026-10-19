package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type routedTimeService struct {
	rtiv1.UnimplementedTimeServiceServer
	router   *federationTimeRouter
	floats   rtiv1.TimeServiceServer
	integers rtiv1.TimeServiceServer
}

func (s *routedTimeService) EnableTimeRegulation(ctx context.Context, req *rtiv1.EnableRegulationRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.EnableTimeRegulation(ctx, req)
}

func (s *routedTimeService) DisableTimeRegulation(ctx context.Context, req *rtiv1.DisableRegulationRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.DisableTimeRegulation(ctx, req)
}

func (s *routedTimeService) EnableTimeConstrained(ctx context.Context, req *rtiv1.EnableConstrainedRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.EnableTimeConstrained(ctx, req)
}

func (s *routedTimeService) DisableTimeConstrained(ctx context.Context, req *rtiv1.DisableConstrainedRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.DisableTimeConstrained(ctx, req)
}

func (s *routedTimeService) NextMessageRequest(ctx context.Context, req *rtiv1.NERRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.NextMessageRequest(ctx, req)
}

func (s *routedTimeService) NextMessageRequestAvailable(ctx context.Context, req *rtiv1.NMRARequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.NextMessageRequestAvailable(ctx, req)
}

func (s *routedTimeService) TimeAdvanceRequest(ctx context.Context, req *rtiv1.TARRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.TimeAdvanceRequest(ctx, req)
}

func (s *routedTimeService) TimeAdvanceRequestAvailable(ctx context.Context, req *rtiv1.TARARequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.TimeAdvanceRequestAvailable(ctx, req)
}

func (s *routedTimeService) FlushQueueRequest(ctx context.Context, req *rtiv1.FQRRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.FlushQueueRequest(ctx, req)
}

func (s *routedTimeService) ModifyLookahead(ctx context.Context, req *rtiv1.ModifyLookaheadRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ModifyLookahead(ctx, req)
}

func (s *routedTimeService) QueryLogicalTime(ctx context.Context, req *rtiv1.QueryFederateTimeRequest) (*rtiv1.QueryFederateTimeResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryLogicalTime(ctx, req)
}

func (s *routedTimeService) QueryLookahead(ctx context.Context, req *rtiv1.QueryFederateTimeRequest) (*rtiv1.QueryLookaheadResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryLookahead(ctx, req)
}

func (s *routedTimeService) QueryLBTS(ctx context.Context, req *rtiv1.QueryLBTSRequest) (*rtiv1.QueryLBTSResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryLBTS(ctx, req)
}

func (s *routedTimeService) QueryGALT(ctx context.Context, req *rtiv1.QueryFederateTimeRequest) (*rtiv1.QueryGALTResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryGALT(ctx, req)
}

func (s *routedTimeService) QueryLITS(ctx context.Context, req *rtiv1.QueryFederateTimeRequest) (*rtiv1.QueryLITSResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryLITS(ctx, req)
}

func (s *routedTimeService) EnableAsynchronousDelivery(ctx context.Context, req *rtiv1.EnableAsynchronousDeliveryRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.EnableAsynchronousDelivery(ctx, req)
}

func (s *routedTimeService) DisableAsynchronousDelivery(ctx context.Context, req *rtiv1.DisableAsynchronousDeliveryRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.DisableAsynchronousDelivery(ctx, req)
}
