package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type routedObjectService struct {
	rtiv1.UnimplementedObjectServiceServer
	router   *federationTimeRouter
	floats   rtiv1.ObjectServiceServer
	integers rtiv1.ObjectServiceServer
}

func (s *routedObjectService) ReleaseMultipleObjectInstanceNames(ctx context.Context, req *rtiv1.ReleaseMultipleObjectInstanceNamesRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ReleaseMultipleObjectInstanceNames(ctx, req)
}

func (s *routedObjectService) RegisterObjectInstance(ctx context.Context, req *rtiv1.RegisterObjectRequest) (*rtiv1.RegisterObjectResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RegisterObjectInstance(ctx, req)
}

func (s *routedObjectService) UpdateAttributeValues(ctx context.Context, req *rtiv1.UpdateAttributeValuesRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.UpdateAttributeValues(ctx, req)
}

func (s *routedObjectService) SendInteraction(ctx context.Context, req *rtiv1.SendInteractionRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.SendInteraction(ctx, req)
}

func (s *routedObjectService) DeleteObjectInstance(ctx context.Context, req *rtiv1.DeleteObjectInstanceRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.DeleteObjectInstance(ctx, req)
}

func (s *routedObjectService) LocalDeleteObjectInstance(ctx context.Context, req *rtiv1.LocalDeleteObjectInstanceRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.LocalDeleteObjectInstance(ctx, req)
}

func (s *routedObjectService) LocalDeleteObjectInstanceFenced(ctx context.Context, req *rtiv1.LocalDeleteObjectInstanceRequest) (*rtiv1.LocalDeleteObjectInstanceFence, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.LocalDeleteObjectInstanceFenced(ctx, req)
}

func (s *routedObjectService) RequestAttributeValueUpdate(ctx context.Context, req *rtiv1.RequestAttributeValueUpdateRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RequestAttributeValueUpdate(ctx, req)
}

func (s *routedObjectService) RequestClassAttributeValueUpdate(ctx context.Context, req *rtiv1.RequestClassAttributeValueUpdateRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RequestClassAttributeValueUpdate(ctx, req)
}

func (s *routedObjectService) ChangeAttributeTransportationType(ctx context.Context, req *rtiv1.ChangeAttributeTransportRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ChangeAttributeTransportationType(ctx, req)
}

func (s *routedObjectService) ChangeInteractionTransportationType(ctx context.Context, req *rtiv1.ChangeInteractionTransportRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ChangeInteractionTransportationType(ctx, req)
}

func (s *routedObjectService) RequestAttributeTransportationTypeChange(ctx context.Context, req *rtiv1.ChangeAttributeTransportRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RequestAttributeTransportationTypeChange(ctx, req)
}

func (s *routedObjectService) QueryAttributeTransportationType(ctx context.Context, req *rtiv1.QueryAttributeTransportationTypeRequest) (*rtiv1.TransportationTypeResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryAttributeTransportationType(ctx, req)
}

func (s *routedObjectService) RequestInteractionTransportationTypeChange(ctx context.Context, req *rtiv1.ChangeInteractionTransportRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.RequestInteractionTransportationTypeChange(ctx, req)
}

func (s *routedObjectService) QueryInteractionTransportationType(ctx context.Context, req *rtiv1.QueryInteractionTransportationTypeRequest) (*rtiv1.TransportationTypeResponse, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.QueryInteractionTransportationType(ctx, req)
}

func (s *routedObjectService) ChangeAttributeOrderType(ctx context.Context, req *rtiv1.ChangeAttributeOrderRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ChangeAttributeOrderType(ctx, req)
}

func (s *routedObjectService) ChangeInteractionOrderType(ctx context.Context, req *rtiv1.ChangeInteractionOrderRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ChangeInteractionOrderType(ctx, req)
}

func (s *routedObjectService) ReserveObjectInstanceName(ctx context.Context, req *rtiv1.ReserveObjectInstanceNameRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ReserveObjectInstanceName(ctx, req)
}

func (s *routedObjectService) ReleaseObjectInstanceName(ctx context.Context, req *rtiv1.ReleaseObjectInstanceNameRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ReleaseObjectInstanceName(ctx, req)
}

func (s *routedObjectService) ReserveMultipleObjectInstanceNames(ctx context.Context, req *rtiv1.ReserveMultipleObjectInstanceNamesRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ReserveMultipleObjectInstanceNames(ctx, req)
}

func (s *routedObjectService) Retract(ctx context.Context, req *rtiv1.RetractRequest) (*rtiv1.Empty, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.Retract(ctx, req)
}

func (s *routedObjectService) PrepareAttributeTransportationTypeChange(ctx context.Context, req *rtiv1.ChangeAttributeTransportRequest) (*rtiv1.TransportationChange, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.PrepareAttributeTransportationTypeChange(ctx, req)
}

func (s *routedObjectService) PrepareInteractionTransportationTypeChange(ctx context.Context, req *rtiv1.ChangeInteractionTransportRequest) (*rtiv1.TransportationChange, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.PrepareInteractionTransportationTypeChange(ctx, req)
}

func (s *routedObjectService) ActivateTransportationTypeChange(ctx context.Context, req *rtiv1.ActivateTransportationTypeChangeRequest) (*rtiv1.TransportationChange, error) {
	backend, err := routeUnary(ctx, s.router, req, s.floats, s.integers)
	if err != nil {
		return nil, err
	}
	return backend.ActivateTransportationTypeChange(ctx, req)
}
