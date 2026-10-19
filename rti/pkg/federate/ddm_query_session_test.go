package federate

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func ddmQuerySessionFederate(t *testing.T, handler grpc.UnaryServerInterceptor) *Federate {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(handler))
	rtiv1.RegisterDDMServiceServer(server, &rtiv1.UnimplementedDDMServiceServer{})
	rtiv1.RegisterSupportServiceServer(server, &rtiv1.UnimplementedSupportServiceServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///ddm-query-session", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &Federate{conn: &Connection{cc: conn, ddm: rtiv1.NewDDMServiceClient(conn)}, federationName: testFederationName, federateHandle: 42}
}

func callDDMRegionQuery(ctx context.Context, t *testing.T, f *Federate, method string) error {
	t.Helper()
	if method == "bounds" {
		lower, upper, found, err := f.QueryBounds(ctx, 7, 9)
		if err == nil && (!found || lower != 1<<53+1 || upper != ^uint64(0)) {
			t.Fatalf("exact bounds changed: %d %d %t", lower, upper, found)
		}
		return err
	}
	dims, err := f.GetDimensionHandleSet(ctx, 7)
	if err == nil && !reflect.DeepEqual(dims, []uint64{9, 10}) {
		t.Fatalf("dimension set changed: %v", dims)
	}
	return err
}

func TestGoDDMRegionQueriesSessionMetadata(t *testing.T) {
	for _, method := range []string{"bounds", "dimensions"} {
		for _, signed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/signed=%t", method, signed), func(t *testing.T) {
				token := []byte{0, 1, 2, 255}
				f := ddmQuerySessionFederate(t, func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
					md, _ := metadata.FromIncomingContext(ctx)
					if values := md.Get("x-query-test"); len(values) != 1 || values[0] != testRetainedMetadata {
						t.Errorf("caller metadata lost: %v", md)
					}
					got := md.Get("gorti-session-token-bin")
					if !signed {
						if len(got) != 0 {
							t.Errorf("fabricated legacy token: %q", got)
						}
						return nil, status.Error(codes.Unauthenticated, "session required")
					}
					if len(got) != 1 || got[0] != string(token) {
						return nil, status.Error(codes.Unauthenticated, "exact current session required")
					}
					switch r := req.(type) {
					case *rtiv1.QueryBoundsRequest:
						if r.WireVersion != 1 || r.FederationName != testFederationName || r.RegionHandle != 7 || r.DimensionHandle != 9 {
							t.Errorf("bounds identity changed: %v", r)
						}
						return &rtiv1.QueryBoundsResponse{Found: true, Bounds: &rtiv1.Range{Lower: 1<<53 + 1, Upper: ^uint64(0)}}, nil
					case *rtiv1.GetDimensionHandleSetRequest:
						if r.WireVersion != 1 || r.FederationName != testFederationName || r.FederateHandle != 42 || r.RegionHandle != 7 {
							t.Errorf("dimension identity changed: %v", r)
						}
						return &rtiv1.GetDimensionHandleSetResponse{DimensionHandles: []uint64{9, 10}}, nil
					default:
						t.Fatalf("unexpected query %T", req)
						return nil, nil
					}
				})
				caller := metadata.Pairs("x-query-test", testRetainedMetadata)
				if signed {
					f.sessionToken = token
					caller.Set("gorti-session-token-bin", "stale-caller-token")
				}
				original := caller.Copy()
				ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(context.Background(), caller), 5*time.Second)
				defer cancel()
				err := callDDMRegionQuery(ctx, t, f, method)
				if signed && err != nil || !signed && status.Code(err) != codes.Unauthenticated {
					t.Fatalf("query signed=%t: %v", signed, err)
				}
				if !reflect.DeepEqual(caller, original) {
					t.Fatal("query mutated caller metadata")
				}
			})
		}
	}
}

func TestGoDDMRegionQueriesPreserveCancellationAndRejection(t *testing.T) {
	for _, method := range []string{"bounds", "dimensions"} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			f := ddmQuerySessionFederate(t, func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
				calls.Add(1)
				return nil, status.Error(codes.PermissionDenied, "foreign region")
			})
			f.sessionToken = []byte("session")
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := callDDMRegionQuery(ctx, t, f, method); status.Code(err) != codes.Canceled || calls.Load() != 0 {
				t.Fatalf("cancelled query sent or changed error: %v calls=%d", err, calls.Load())
			}
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := callDDMRegionQuery(ctx, t, f, method); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("foreign-owner rejection changed: %v", err)
			}
		})
	}
}
