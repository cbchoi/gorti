package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

// These methods query the server directly; the corresponding methods without
// the Get prefix use the local authoritative FOM view and refresh on cache miss.
func (f *Federate) GetObjectClassHandle(ctx context.Context, name string) (uint64, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetObjectClassHandle(f.sessionContext(ctx), &rtiv1.GetObjectClassHandleRequest{WireVersion: 1, FederationName: f.federationName, ClassName: name})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	return r.GetClassHandle(), nil
}
func (f *Federate) GetObjectClassName(ctx context.Context, h uint64) (string, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetObjectClassName(f.sessionContext(ctx), &rtiv1.GetObjectClassNameRequest{WireVersion: 1, FederationName: f.federationName, ClassHandle: h})
	if e != nil {
		return "", wrapStatusErr(e)
	}
	return r.GetClassName(), nil
}
func (f *Federate) GetAttributeHandle(ctx context.Context, class uint64, name string) (uint64, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetAttributeHandle(f.sessionContext(ctx), &rtiv1.GetAttributeHandleRequest{WireVersion: 1, FederationName: f.federationName, ClassHandle: class, AttributeName: name})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	return r.GetAttributeHandle(), nil
}
func (f *Federate) GetAttributeName(ctx context.Context, class, attr uint64) (string, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetAttributeName(f.sessionContext(ctx), &rtiv1.GetAttributeNameRequest{WireVersion: 1, FederationName: f.federationName, ClassHandle: class, AttributeHandle: attr})
	if e != nil {
		return "", wrapStatusErr(e)
	}
	return r.GetAttributeName(), nil
}
func (f *Federate) GetInteractionClassHandle(ctx context.Context, name string) (uint64, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetInteractionClassHandle(f.sessionContext(ctx), &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.federationName, ClassName: name})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	return r.GetClassHandle(), nil
}
func (f *Federate) GetInteractionClassName(ctx context.Context, h uint64) (string, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetInteractionClassName(f.sessionContext(ctx), &rtiv1.GetInteractionClassNameRequest{WireVersion: 1, FederationName: f.federationName, ClassHandle: h})
	if e != nil {
		return "", wrapStatusErr(e)
	}
	return r.GetClassName(), nil
}
func (f *Federate) GetParameterHandle(ctx context.Context, class uint64, name string) (uint64, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetParameterHandle(f.sessionContext(ctx), &rtiv1.GetParameterHandleRequest{WireVersion: 1, FederationName: f.federationName, ClassHandle: class, ParameterName: name})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	return r.GetParameterHandle(), nil
}
func (f *Federate) GetParameterName(ctx context.Context, class, param uint64) (string, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetParameterName(f.sessionContext(ctx), &rtiv1.GetParameterNameRequest{WireVersion: 1, FederationName: f.federationName, ClassHandle: class, ParameterHandle: param})
	if e != nil {
		return "", wrapStatusErr(e)
	}
	return r.GetParameterName(), nil
}
func (f *Federate) GetDimensionHandle(ctx context.Context, name string) (uint64, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetDimensionHandle(f.sessionContext(ctx), &rtiv1.GetDimensionHandleRequest{WireVersion: 1, FederationName: f.federationName, DimensionName: name})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	return r.GetDimensionHandle(), nil
}
func (f *Federate) GetDimensionName(ctx context.Context, h uint64) (string, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetDimensionName(f.sessionContext(ctx), &rtiv1.GetDimensionNameRequest{WireVersion: 1, FederationName: f.federationName, DimensionHandle: h})
	if e != nil {
		return "", wrapStatusErr(e)
	}
	return r.GetDimensionName(), nil
}
func (f *Federate) GetDimensionUpperBound(ctx context.Context, h uint64) (uint64, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetDimensionUpperBound(f.sessionContext(ctx), &rtiv1.GetDimensionUpperBoundRequest{WireVersion: 1, FederationName: f.federationName, DimensionHandle: h})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	return r.GetUpperBound(), nil
}
func (f *Federate) GetOrderType(ctx context.Context, name string) (OrderType, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetOrderType(f.sessionContext(ctx), &rtiv1.GetOrderTypeRequest{WireVersion: 1, OrderName: name})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	value := r.GetOrderType()
	if value < 1 || value > 2 {
		return 0, fmt.Errorf("federate: unknown order type %d", r.GetOrderType())
	}
	return OrderType(value), nil
}
func (f *Federate) GetOrderName(ctx context.Context, order OrderType) (string, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetOrderName(f.sessionContext(ctx), &rtiv1.GetOrderNameRequest{WireVersion: 1, OrderType: uint32(order)})
	if e != nil {
		return "", wrapStatusErr(e)
	}
	return r.GetOrderName(), nil
}
func (f *Federate) GetTransportationType(ctx context.Context, name string) (TransportType, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetTransportationType(f.sessionContext(ctx), &rtiv1.GetTransportationTypeRequest{WireVersion: 1, TransportationName: name})
	if e != nil {
		return 0, wrapStatusErr(e)
	}
	value := r.GetTransportationType()
	if value < 1 || value > 2 {
		return 0, fmt.Errorf("federate: unknown transportation type %d", r.GetTransportationType())
	}
	return TransportType(value), nil
}
func (f *Federate) GetTransportationName(ctx context.Context, transport TransportType) (string, error) {
	r, e := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetTransportationName(f.sessionContext(ctx), &rtiv1.GetTransportationNameRequest{WireVersion: 1, TransportationType: uint32(transport)})
	if e != nil {
		return "", wrapStatusErr(e)
	}
	return r.GetTransportationName(), nil
}
