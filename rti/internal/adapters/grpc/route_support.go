package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type routedSupportService struct {
	rtiv1.UnimplementedSupportServiceServer
	router   *federationTimeRouter
	floats   rtiv1.SupportServiceServer
	integers rtiv1.SupportServiceServer
	shared   rtiv1.SupportServiceServer
}

// Enum/name services have no federation argument and are valid without joining.
// Authenticated calls use the existing live-caller resolver; anonymous/stale
// callers retain the shared, scalar-independent lookup, not a default factory.
func (s *routedSupportService) globalBackend(ctx context.Context) (rtiv1.SupportServiceServer, error) {
	if s.router.resolveCaller == nil {
		return s.shared, nil
	}
	fed, handle, release, err := s.router.resolveCaller(ctx)
	if release != nil {
		defer release()
	}
	if err != nil || fed == "" || handle == 0 {
		return s.shared, nil
	}
	integer, err := s.router.integerFederation(ctx, fed)
	if err != nil {
		return nil, err
	}
	if integer {
		return s.integers, nil
	}
	return s.floats, nil
}

func (s *routedSupportService) ReportCallbackInvocation(ctx context.Context, req *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ReportCallbackInvocation(ctx, req)
}

func (s *routedSupportService) NormalizeFederateHandle(ctx context.Context, req *rtiv1.NormalizeFederateHandleRequest) (*rtiv1.NormalizeFederateHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.NormalizeFederateHandle(ctx, req)
}

func (s *routedSupportService) NormalizeServiceGroup(ctx context.Context, req *rtiv1.NormalizeServiceGroupRequest) (*rtiv1.NormalizeServiceGroupResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.NormalizeServiceGroup(ctx, req)
}

func (s *routedSupportService) GetUpdateRateValue(ctx context.Context, req *rtiv1.GetUpdateRateValueRequest) (*rtiv1.GetUpdateRateValueResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetUpdateRateValue(ctx, req)
}

func (s *routedSupportService) GetUpdateRateValueForAttribute(ctx context.Context, req *rtiv1.GetUpdateRateValueForAttributeRequest) (*rtiv1.GetUpdateRateValueForAttributeResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetUpdateRateValueForAttribute(ctx, req)
}

func (s *routedSupportService) GetAvailableDimensionsForClassAttribute(ctx context.Context, req *rtiv1.GetAvailableDimensionsForClassAttributeRequest) (*rtiv1.GetAvailableDimensionsForClassAttributeResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetAvailableDimensionsForClassAttribute(ctx, req)
}

func (s *routedSupportService) GetAvailableDimensionsForInteractionClass(ctx context.Context, req *rtiv1.GetAvailableDimensionsForInteractionClassRequest) (*rtiv1.GetAvailableDimensionsForInteractionClassResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetAvailableDimensionsForInteractionClass(ctx, req)
}

func (s *routedSupportService) GetDimensionHandleSet(ctx context.Context, req *rtiv1.GetDimensionHandleSetRequest) (*rtiv1.GetDimensionHandleSetResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetDimensionHandleSet(ctx, req)
}

func (s *routedSupportService) GetObjectClassHandle(ctx context.Context, req *rtiv1.GetObjectClassHandleRequest) (*rtiv1.GetObjectClassHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetObjectClassHandle(ctx, req)
}

func (s *routedSupportService) GetObjectClassName(ctx context.Context, req *rtiv1.GetObjectClassNameRequest) (*rtiv1.GetObjectClassNameResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetObjectClassName(ctx, req)
}

func (s *routedSupportService) GetAttributeHandle(ctx context.Context, req *rtiv1.GetAttributeHandleRequest) (*rtiv1.GetAttributeHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetAttributeHandle(ctx, req)
}

func (s *routedSupportService) GetAttributeName(ctx context.Context, req *rtiv1.GetAttributeNameRequest) (*rtiv1.GetAttributeNameResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetAttributeName(ctx, req)
}

func (s *routedSupportService) GetInteractionClassHandle(ctx context.Context, req *rtiv1.GetInteractionClassHandleRequest) (*rtiv1.GetInteractionClassHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetInteractionClassHandle(ctx, req)
}

func (s *routedSupportService) GetInteractionClassName(ctx context.Context, req *rtiv1.GetInteractionClassNameRequest) (*rtiv1.GetInteractionClassNameResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetInteractionClassName(ctx, req)
}

func (s *routedSupportService) GetParameterHandle(ctx context.Context, req *rtiv1.GetParameterHandleRequest) (*rtiv1.GetParameterHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetParameterHandle(ctx, req)
}

func (s *routedSupportService) GetParameterName(ctx context.Context, req *rtiv1.GetParameterNameRequest) (*rtiv1.GetParameterNameResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetParameterName(ctx, req)
}

func (s *routedSupportService) GetDimensionHandle(ctx context.Context, req *rtiv1.GetDimensionHandleRequest) (*rtiv1.GetDimensionHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetDimensionHandle(ctx, req)
}

func (s *routedSupportService) GetDimensionName(ctx context.Context, req *rtiv1.GetDimensionNameRequest) (*rtiv1.GetDimensionNameResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetDimensionName(ctx, req)
}

func (s *routedSupportService) GetDimensionUpperBound(ctx context.Context, req *rtiv1.GetDimensionUpperBoundRequest) (*rtiv1.GetDimensionUpperBoundResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetDimensionUpperBound(ctx, req)
}

func (s *routedSupportService) GetOrderType(ctx context.Context, req *rtiv1.GetOrderTypeRequest) (*rtiv1.GetOrderTypeResponse, error) {
	backend, err := s.globalBackend(ctx)
	if err != nil {
		return nil, err
	}
	return backend.GetOrderType(ctx, req)
}

func (s *routedSupportService) GetOrderName(ctx context.Context, req *rtiv1.GetOrderNameRequest) (*rtiv1.GetOrderNameResponse, error) {
	backend, err := s.globalBackend(ctx)
	if err != nil {
		return nil, err
	}
	return backend.GetOrderName(ctx, req)
}

func (s *routedSupportService) GetTransportationType(ctx context.Context, req *rtiv1.GetTransportationTypeRequest) (*rtiv1.GetTransportationTypeResponse, error) {
	backend, err := s.globalBackend(ctx)
	if err != nil {
		return nil, err
	}
	return backend.GetTransportationType(ctx, req)
}

func (s *routedSupportService) GetTransportationName(ctx context.Context, req *rtiv1.GetTransportationNameRequest) (*rtiv1.GetTransportationNameResponse, error) {
	backend, err := s.globalBackend(ctx)
	if err != nil {
		return nil, err
	}
	return backend.GetTransportationName(ctx, req)
}

func (s *routedSupportService) GetObjectInstanceHandle(ctx context.Context, req *rtiv1.GetObjectInstanceHandleRequest) (*rtiv1.GetObjectInstanceHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetObjectInstanceHandle(ctx, req)
}

func (s *routedSupportService) GetObjectInstanceName(ctx context.Context, req *rtiv1.GetObjectInstanceNameRequest) (*rtiv1.GetObjectInstanceNameResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetObjectInstanceName(ctx, req)
}

func (s *routedSupportService) GetKnownObjectClassHandle(ctx context.Context, req *rtiv1.GetKnownObjectClassHandleRequest) (*rtiv1.GetKnownObjectClassHandleResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.GetKnownObjectClassHandle(ctx, req)
}
