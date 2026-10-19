package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type supportRateFOM struct {
	core.FOMHandle
	core.FOMHandleNameLookup
}

func (supportRateFOM) UpdateRateValue(name string) (float64, bool) {
	v, ok := map[string]float64{"Slow": 2.5, "Zero": 0}[name]
	return v, ok
}

func (supportRateFOM) ObjectClassName(cls core.ObjectClassHandle) (string, bool) {
	return "Object", cls == 10
}
func (supportRateFOM) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	return "value", cls == 10 && attr == 2
}
func (supportRateFOM) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	return "Signal", cls == 20
}
func (supportRateFOM) AttributeDimensions(cls core.ObjectClassHandle, attr core.AttributeHandle) ([]core.DDMDimensionHandle, bool) {
	return []core.DDMDimensionHandle{3, 2, 3}, cls == 10 && attr == 2
}
func (supportRateFOM) InteractionDimensions(cls core.InteractionClassHandle) ([]core.DDMDimensionHandle, bool) {
	return nil, cls == 20
}

type supportAttributeRateQuery struct{}

func (supportAttributeRateQuery) GetUpdateRateValueForAttribute(_ context.Context, _ core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle) (float64, error) {
	if caller != 1 || obj != 7 {
		return 0, core.ErrObjectHandleInvalid
	}
	if attr == 3 {
		return 0, core.ErrUpdateRateValueUnavailable
	}
	return 0, nil
}

type supportRateRepo struct{}

func (supportRateRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return supportRateFOM{}, nil
}
func (supportRateRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return supportRateFOM{}, nil
}

func TestSupportNamedUpdateRatesRealWire(t *testing.T) {
	repo := supportRateRepo{}
	listener := bufconn.Listen(1 << 20)
	server := grpcgo.NewServer()
	service := newSupportService(repo)
	service.updateRates = supportAttributeRateQuery{}
	rtiv1.RegisterSupportServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///rates", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewSupportServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for name, want := range map[string]float64{"Slow": 2.5, "Zero": 0} {
		resp, err := client.GetUpdateRateValue(ctx, &rtiv1.GetUpdateRateValueRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, UpdateRateDesignator: name})
		if err != nil || resp.GetUpdateRate() != want {
			t.Fatalf("%s: response=%v error=%v", name, resp, err)
		}
	}
	var trailer metadata.MD
	_, err = client.GetUpdateRateValue(ctx, &rtiv1.GetUpdateRateValueRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, UpdateRateDesignator: "missing"}, grpcgo.Trailer(&trailer))
	if status.Code(err) != codes.InvalidArgument || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != "InvalidUpdateRateDesignator" {
		t.Fatalf("invalid rate=%v trailer=%v", err, trailer)
	}
	zero, err := client.GetUpdateRateValueForAttribute(ctx, &rtiv1.GetUpdateRateValueForAttributeRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, ObjectHandle: 7, AttributeHandle: 2})
	if err != nil || zero.GetUpdateRate() != 0 {
		t.Fatalf("named zero rate=%v %v", zero, err)
	}
	trailer = nil
	_, err = client.GetUpdateRateValueForAttribute(ctx, &rtiv1.GetUpdateRateValueForAttributeRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, ObjectHandle: 7, AttributeHandle: 3}, grpcgo.Trailer(&trailer))
	if status.Code(err) != codes.Internal || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != rtiInternalErrorName {
		t.Fatalf("unavailable numeric rate fabricated result: %v %v", err, trailer)
	}
	dims, err := client.GetAvailableDimensionsForClassAttribute(ctx, &rtiv1.GetAvailableDimensionsForClassAttributeRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, ObjectClassHandle: 10, AttributeHandle: 2})
	if err != nil || len(dims.GetDimensionHandles()) != 2 || dims.DimensionHandles[0] != 2 || dims.DimensionHandles[1] != 3 {
		t.Fatalf("dedup/sort=%v %v", dims, err)
	}
	empty, err := client.GetAvailableDimensionsForInteractionClass(ctx, &rtiv1.GetAvailableDimensionsForInteractionClassRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, InteractionClassHandle: 20})
	if err != nil || len(empty.GetDimensionHandles()) != 0 {
		t.Fatalf("defined empty set=%v %v", empty, err)
	}
	trailer = nil
	_, err = client.GetAvailableDimensionsForInteractionClass(ctx, &rtiv1.GetAvailableDimensionsForInteractionClassRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, InteractionClassHandle: 99}, grpcgo.Trailer(&trailer))
	if status.Code(err) != codes.NotFound || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != "InvalidInteractionClassHandle" {
		t.Fatalf("invalid interaction=%v %v", err, trailer)
	}
}
