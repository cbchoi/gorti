package federate

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"testing"
)

func TestSupportSDKRateAndDimensions(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		identity := req.(interface {
			GetFederateHandle() uint64
			GetFederationName() string
			GetWireVersion() rtiv1.WireVersion
		})
		if identity.GetFederateHandle() != 42 || identity.GetFederationName() != "f" || identity.GetWireVersion() != 1 {
			t.Errorf("identity=%v", req)
		}
		switch r := req.(type) {
		case *rtiv1.GetUpdateRateValueRequest:
			if r.UpdateRateDesignator != "Slow" {
				t.Errorf("name=%q", r.UpdateRateDesignator)
			}
			return &rtiv1.GetUpdateRateValueResponse{UpdateRate: 2.5}, nil
		case *rtiv1.GetUpdateRateValueForAttributeRequest:
			if r.ObjectHandle != 9 || r.AttributeHandle != 4 {
				t.Errorf("attribute target=%v", r)
			}
			return &rtiv1.GetUpdateRateValueForAttributeResponse{UpdateRate: 0}, nil
		case *rtiv1.GetAvailableDimensionsForClassAttributeRequest:
			if r.ObjectClassHandle != 3 || r.AttributeHandle != 4 {
				t.Errorf("class target=%v", r)
			}
			return &rtiv1.GetAvailableDimensionsForClassAttributeResponse{DimensionHandles: []uint64{1, 2}}, nil
		case *rtiv1.GetAvailableDimensionsForInteractionClassRequest:
			if r.InteractionClassHandle != 5 {
				t.Errorf("interaction=%v", r)
			}
			return &rtiv1.GetAvailableDimensionsForInteractionClassResponse{}, nil
		case *rtiv1.GetDimensionHandleSetRequest:
			if r.RegionHandle != 7 {
				t.Errorf("region=%v", r)
			}
			return &rtiv1.GetDimensionHandleSetResponse{DimensionHandles: []uint64{2}}, nil
		default:
			t.Errorf("unexpected %T", req)
			return nil, nil
		}
	}))
	rtiv1.RegisterSupportServiceServer(server, &rtiv1.UnimplementedSupportServiceServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///support-rates", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	f := &Federate{conn: &Connection{cc: conn}, federationName: "f", federateHandle: 42}
	ctx := context.Background()
	if v, err := f.GetUpdateRateValue(ctx, "Slow"); err != nil || v != 2.5 {
		t.Fatalf("named rate=%v %v", v, err)
	}
	if v, err := f.GetUpdateRateValueForAttribute(ctx, 9, 4); err != nil || v != 0 {
		t.Fatalf("zero rate=%v %v", v, err)
	}
	if v, err := f.GetAvailableDimensionsForClassAttribute(ctx, 3, 4); err != nil || len(v) != 2 || v[0] != 1 || v[1] != 2 {
		t.Fatalf("attribute dimensions=%v %v", v, err)
	}
	if v, err := f.GetAvailableDimensionsForInteractionClass(ctx, 5); err != nil || len(v) != 0 {
		t.Fatalf("empty dimensions=%v %v", v, err)
	}
	if v, err := f.GetDimensionHandleSet(ctx, 7); err != nil || len(v) != 1 || v[0] != 2 {
		t.Fatalf("region dimensions=%v %v", v, err)
	}
}
