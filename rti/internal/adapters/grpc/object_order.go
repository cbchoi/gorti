package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *objectServiceOf[T]) ChangeAttributeOrderType(ctx context.Context, req *rtiv1.ChangeAttributeOrderRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.WireVersion) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if req.OrderType != rtiv1.OrderType_ORDER_TYPE_RECEIVE && req.OrderType != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP {
		return nil, status.Error(codes.InvalidArgument, "invalid order type")
	}
	service, ok := s.obj.(core.OrderManagement)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "object registry does not support order changes")
	}
	attrs := make([]core.AttributeHandle, len(req.AttributeHandles))
	for i, attr := range req.AttributeHandles {
		attrs[i] = core.AttributeHandle(attr)
	}
	if err := service.ChangeAttributeOrderType(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.ObjectHandle(req.ObjectHandle), attrs, protoOrderTypeToCore(req.OrderType)); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return emptyResponse, nil
}

func (s *objectServiceOf[T]) ChangeInteractionOrderType(ctx context.Context, req *rtiv1.ChangeInteractionOrderRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.WireVersion) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if req.OrderType != rtiv1.OrderType_ORDER_TYPE_RECEIVE && req.OrderType != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP {
		return nil, status.Error(codes.InvalidArgument, "invalid order type")
	}
	service, ok := s.obj.(core.OrderManagement)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "object registry does not support order changes")
	}
	if err := service.ChangeInteractionOrderType(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.InteractionClassHandle(req.InteractionClassHandle), protoOrderTypeToCore(req.OrderType)); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return emptyResponse, nil
}
func protoOrderTypeToCore(order rtiv1.OrderType) core.OrderType {
	switch order {
	case rtiv1.OrderType_ORDER_TYPE_RECEIVE:
		return core.OrderReceive
	case rtiv1.OrderType_ORDER_TYPE_TIMESTAMP:
		return core.OrderTimestamp
	default:
		return core.OrderUnspecified
	}
}
