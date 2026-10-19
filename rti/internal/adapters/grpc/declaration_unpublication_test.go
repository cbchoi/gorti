package grpc

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestUnpublicationAttributeSetPresenceRealWire(t *testing.T) {
	present, omitted := true, false
	for _, tc := range []struct {
		name     string
		presence *bool
		attrs    []uint64
		want     []core.AttributeHandle
		code     codes.Code
	}{
		{"legacy whole", nil, nil, nil, codes.OK},
		{"legacy scoped", nil, []uint64{2}, []core.AttributeHandle{3}, codes.OK},
		{"explicit empty", &present, nil, []core.AttributeHandle{2, 3}, codes.OK},
		{"explicit scoped", &present, []uint64{2}, []core.AttributeHandle{3}, codes.OK},
		{"explicit omitted", &omitted, nil, nil, codes.OK},
		{"contradictory omitted", &omitted, []uint64{2}, []core.AttributeHandle{2, 3}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, mgr := declarationFixture()
			listener := bufconn.Listen(1 << 20)
			server := grpcgo.NewServer()
			rtiv1.RegisterDeclarationServiceServer(server, svc)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); _ = listener.Close() })
			conn, err := grpcgo.NewClient("passthrough:///unpublication-presence", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := mgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2, 3}); err != nil {
				t.Fatal(err)
			}
			_, err = rtiv1.NewDeclarationServiceClient(conn).UnpublishObjectClassAttributes(ctx, &rtiv1.UnpubObjAttrsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectClassHandle: 7, AttributeHandles: tc.attrs, AttributeSetPresent: tc.presence})
			if status.Code(err) != tc.code {
				t.Fatalf("error=%v want=%v", err, tc.code)
			}
			if got := mgr.PublishedObjectAttributes("fed", 1, 7); !slices.Equal(got, tc.want) {
				t.Fatalf("published=%v want=%v", got, tc.want)
			}
		})
	}
}
