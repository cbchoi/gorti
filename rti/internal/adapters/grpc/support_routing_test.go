package grpc

import (
	"context"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type routingSupportRecorder struct {
	rtiv1.UnimplementedSupportServiceServer
	label string
	calls int
}

func (s *routingSupportRecorder) GetOrderType(context.Context, *rtiv1.GetOrderTypeRequest) (*rtiv1.GetOrderTypeResponse, error) {
	s.calls++
	return &rtiv1.GetOrderTypeResponse{OrderType: 1}, nil
}

func (s *routingSupportRecorder) GetObjectInstanceName(context.Context, *rtiv1.GetObjectInstanceNameRequest) (*rtiv1.GetObjectInstanceNameResponse, error) {
	s.calls++
	return &rtiv1.GetObjectInstanceNameResponse{ObjectName: s.label}, nil
}

func TestSupportRoutingHandlelessAndScopedCalls(t *testing.T) {
	floats, ints, shared := &routingSupportRecorder{label: "float"}, &routingSupportRecorder{label: "integer"}, &routingSupportRecorder{label: "shared"}
	router := &federationTimeRouter{factories: routingFactories{"float": float64TimeFactory, "int": integer64TimeFactory, "invalid": "unsupported"}}
	service := &routedSupportService{router: router, floats: floats, integers: ints, shared: shared}
	ctx := context.Background()
	if _, err := service.GetOrderType(ctx, &rtiv1.GetOrderTypeRequest{WireVersion: 1, OrderName: "Receive"}); err != nil || shared.calls != 1 || floats.calls+ints.calls != 0 {
		t.Fatal("global lookup invented a factory")
	}
	released := 0
	router.resolveCaller = func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
		return "int", 7, func() { released++ }, nil
	}
	if _, err := service.GetOrderType(ctx, &rtiv1.GetOrderTypeRequest{WireVersion: 1}); err != nil || ints.calls != 1 || released != 1 {
		t.Fatal("live caller factory not used")
	}
	router.resolveCaller = func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
		return "", 0, nil, errors.New("stale caller")
	}
	if _, err := service.GetOrderType(ctx, &rtiv1.GetOrderTypeRequest{WireVersion: 1}); err != nil || shared.calls != 2 {
		t.Fatal("public enum lookup changed for stale caller")
	}
	response, err := service.GetObjectInstanceName(ctx, &rtiv1.GetObjectInstanceNameRequest{WireVersion: 1, FederationName: "int", FederateHandle: 7})
	if err != nil || response.ObjectName != "integer" {
		t.Fatal("runtime object lookup used wrong backend")
	}
	if _, err := service.GetObjectInstanceName(ctx, &rtiv1.GetObjectInstanceNameRequest{WireVersion: 1}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unscoped object lookup invented a backend: %v", err)
	}
	router.resolveCaller = func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
		return "invalid", 7, nil, nil
	}
	if _, err := service.GetOrderType(ctx, &rtiv1.GetOrderTypeRequest{WireVersion: 1}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unknown live factory accepted: %v", err)
	}
}
