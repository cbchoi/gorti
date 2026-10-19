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

type supportNameFOM struct{ supportRateFOM }

func (supportNameFOM) LookupObjectClass(name string) (core.ObjectClassHandle, bool) {
	return 10, name == "Known"
}

func (supportNameFOM) LookupAttribute(class core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	return 2, class == 10 && name == "value"
}

func (supportNameFOM) LookupInteractionClass(name string) (core.InteractionClassHandle, bool) {
	return 20, name == "Signal"
}

func (supportNameFOM) LookupParameter(class core.InteractionClassHandle, name string) (core.ParameterHandle, bool) {
	return 3, class == 20 && name == "payload"
}

func (supportNameFOM) LookupDimension(name string) (core.DimensionHandle, bool) {
	return 4, name == "X"
}

type supportNameRepo struct{ core.FOMRepository }

func (supportNameRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return supportNameFOM{}, nil
}

func supportNameClient(t *testing.T) rtiv1.SupportServiceClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpcgo.NewServer()
	rtiv1.RegisterSupportServiceServer(server, newSupportService(supportNameRepo{}))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///names", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return rtiv1.NewSupportServiceClient(conn)
}

func TestSupportObjectClassNameErrorRealWire(t *testing.T) {
	client := supportNameClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, name := range []string{"Known", "Missing", "Known"} {
		var trailer metadata.MD
		resp, err := client.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{WireVersion: 1, FederationName: "f", ClassName: name}, grpcgo.Trailer(&trailer))
		if name == "Missing" {
			if status.Code(err) != codes.NotFound || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != "NameNotFound" {
				t.Fatalf("missing name: error=%v trailer=%v", err, trailer)
			}
		} else if err != nil || resp.GetClassHandle() != 10 || len(trailer.Get(specExceptionTrailerKey)) != 0 {
			t.Fatalf("known name: response=%v error=%v trailer=%v", resp, err, trailer)
		}
	}
}

func TestSupportNameLookupExceptionsRealWire(t *testing.T) {
	client := supportNameClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cases := []struct {
		service, known, invalidClass string
		class, handle                uint64
		lookup                       func(uint64, string, ...grpcgo.CallOption) (uint64, error)
	}{
		{"object", "Known", "", 0, 10, func(_ uint64, name string, opts ...grpcgo.CallOption) (uint64, error) {
			r, err := client.GetObjectClassHandle(ctx, &rtiv1.GetObjectClassHandleRequest{WireVersion: 1, FederationName: "f", ClassName: name}, opts...)
			return r.GetClassHandle(), err
		}},
		{"attribute", "value", "InvalidObjectClassHandle", 10, 2, func(class uint64, name string, opts ...grpcgo.CallOption) (uint64, error) {
			r, err := client.GetAttributeHandle(ctx, &rtiv1.GetAttributeHandleRequest{WireVersion: 1, FederationName: "f", ClassHandle: class, AttributeName: name}, opts...)
			return r.GetAttributeHandle(), err
		}},
		{"interaction", "Signal", "", 0, 20, func(_ uint64, name string, opts ...grpcgo.CallOption) (uint64, error) {
			r, err := client.GetInteractionClassHandle(ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: "f", ClassName: name}, opts...)
			return r.GetClassHandle(), err
		}},
		{"parameter", "payload", "InvalidInteractionClassHandle", 20, 3, func(class uint64, name string, opts ...grpcgo.CallOption) (uint64, error) {
			r, err := client.GetParameterHandle(ctx, &rtiv1.GetParameterHandleRequest{WireVersion: 1, FederationName: "f", ClassHandle: class, ParameterName: name}, opts...)
			return r.GetParameterHandle(), err
		}},
		{"dimension", "X", "", 0, 4, func(_ uint64, name string, opts ...grpcgo.CallOption) (uint64, error) {
			r, err := client.GetDimensionHandle(ctx, &rtiv1.GetDimensionHandleRequest{WireVersion: 1, FederationName: "f", DimensionName: name}, opts...)
			return r.GetDimensionHandle(), err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.service, func(t *testing.T) {
			for _, step := range []struct{ label, name string }{{"before", tc.known}, {"missing", "Missing"}, {"empty", ""}, {"after", tc.known}} {
				t.Run(step.label, func(t *testing.T) {
					var trailer metadata.MD
					handle, err := tc.lookup(tc.class, step.name, grpcgo.Trailer(&trailer))
					if step.name == tc.known {
						if err != nil || handle != tc.handle || len(trailer.Get(specExceptionTrailerKey)) != 0 {
							t.Fatalf("known name: handle=%d error=%v trailer=%v", handle, err, trailer)
						}
					} else if status.Code(err) != codes.NotFound || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != "NameNotFound" {
						t.Fatalf("missing name: error=%v trailer=%v", err, trailer)
					}
				})
			}
			if tc.invalidClass != "" {
				for _, class := range []uint64{0, 99999} {
					t.Run("invalid-class", func(t *testing.T) {
						var trailer metadata.MD
						_, err := tc.lookup(class, tc.known, grpcgo.Trailer(&trailer))
						if status.Code(err) != codes.NotFound || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != tc.invalidClass {
							t.Fatalf("invalid class: error=%v trailer=%v want=%s", err, trailer, tc.invalidClass)
						}
					})
				}
			}
		})
	}
}
