package m24spec

import (
	"context"
	"net"
	"testing"
	"time"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/tests/testkit/grpctest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestResignWirePreconditionsAndRetry(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "owned"
		if pending {
			name = "pending"
		}
		t.Run(name, func(t *testing.T) {
			f := newResignFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			want := "FederateOwnsAttributes"
			if pending {
				obj := f.register(t, f.b)
				if err := f.own.Acquire(ctx, resignFed, f.a, obj, []core.AttributeHandle{2}, nil); err != nil {
					t.Fatal(err)
				}
				want = "OwnershipAcquisitionPending"
			} else {
				f.register(t, f.a)
			}
			adapter, err := grpcsvc.NewServer(grpcsvc.Options{Federations: f.fm, Declarations: f.dm, Objects: f.om, Outbox: f.out})
			if err != nil {
				t.Fatal(err)
			}
			server := grpc.NewServer(grpc.UnaryInterceptor(adapter.UnaryMembershipInterceptor()))
			if err := adapter.Register(server); err != nil {
				t.Fatal(err)
			}
			listener := bufconn.Listen(1024 * 1024)
			grpctest.Serve(t, server, listener)
			conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			client := rtiv1.NewFederationServiceClient(conn)
			req := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: string(resignFed), FederateHandle: uint64(f.a), Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}
			var trailer metadata.MD
			_, err = client.ResignFederation(ctx, req, grpc.Trailer(&trailer))
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("RPC error = %v", err)
			}
			if got := trailer.Get("rti-spec-exception"); len(got) != 1 || got[0] != want {
				t.Fatalf("exception trailer = %v, want %s", got, want)
			}
			if err := f.fm.ValidateMember(resignFed, f.a); err != nil {
				t.Fatal("rejected RPC resigned member", err)
			}
			req.Action = rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE
			if _, err = client.ResignFederation(ctx, req); err != nil {
				t.Fatal("retry RPC", err)
			}
		})
	}
}
