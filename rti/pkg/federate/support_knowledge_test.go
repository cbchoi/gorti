package federate

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestSupportSDKCallerScopedQueries(t *testing.T) {
	var calls atomic.Int32
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(_ context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		calls.Add(1)
		identity := req.(interface {
			GetFederateHandle() uint64
			GetFederationName() string
			GetWireVersion() rtiv1.WireVersion
		})
		if identity.GetFederateHandle() != 19 || identity.GetFederationName() != "known" || identity.GetWireVersion() != 1 {
			t.Errorf("caller identity was not forwarded: %v", req)
		}
		switch r := req.(type) {
		case *rtiv1.GetObjectInstanceHandleRequest:
			if r.ObjectName != "shared" {
				t.Errorf("name=%q", r.ObjectName)
			}
			return &rtiv1.GetObjectInstanceHandleResponse{ObjectHandle: 42}, nil
		case *rtiv1.GetObjectInstanceNameRequest:
			if r.ObjectHandle != 42 {
				t.Errorf("handle=%d", r.ObjectHandle)
			}
			return &rtiv1.GetObjectInstanceNameResponse{ObjectName: "shared"}, nil
		case *rtiv1.GetKnownObjectClassHandleRequest:
			if r.ObjectHandle == 99 {
				return nil, status.Error(codes.InvalidArgument, "unknown object")
			}
			if r.ObjectHandle != 42 {
				t.Errorf("handle=%d", r.ObjectHandle)
			}
			return &rtiv1.GetKnownObjectClassHandleResponse{ClassHandle: 7}, nil
		default:
			return nil, status.Error(codes.Unimplemented, "unexpected call")
		}
	}))
	rtiv1.RegisterSupportServiceServer(server, &rtiv1.UnimplementedSupportServiceServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Cleanup(func() { _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///support-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	f := &Federate{conn: &Connection{cc: conn}, federationName: "known", federateHandle: 19}
	ctx := context.Background()
	if h, err := f.GetObjectInstanceHandle(ctx, "shared"); err != nil || h != 42 {
		t.Fatalf("handle=%d error=%v", h, err)
	}
	if name, err := f.GetObjectInstanceName(ctx, 42); err != nil || name != "shared" {
		t.Fatalf("name=%q error=%v", name, err)
	}
	if class, err := f.GetKnownObjectClassHandle(ctx, 42); err != nil || class != 7 {
		t.Fatalf("class=%d error=%v", class, err)
	}
	if _, err := f.GetKnownObjectClassHandle(ctx, 99); err == nil {
		t.Fatal("remote rejection ignored")
	}
	if calls.Load() != 4 {
		t.Fatalf("RPC calls=%d", calls.Load())
	}
}
