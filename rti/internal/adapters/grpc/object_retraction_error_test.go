package grpc

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type retractionErrorRegistry struct {
	core.ObjectRegistry
	err error
}

func (r *retractionErrorRegistry) RetractWithError(context.Context, core.FederationName, core.FederateHandle, uint64) error {
	return r.err
}

func TestRetractTooLatePreservesTypedExceptionTrailer(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"sentinel", core.ErrRetractionTooLate},
		{"wrapped", fmt.Errorf("retract deletion: %w", core.ErrRetractionTooLate)},
		{"success", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := bufconn.Listen(1 << 20)
			server := stdgrpc.NewServer()
			rtiv1.RegisterObjectServiceServer(server, newObjectService(&retractionErrorRegistry{err: tc.err}))
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			t.Cleanup(func() { _ = listener.Close() })
			conn, err := stdgrpc.NewClient("passthrough:///retraction",
				stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					return listener.DialContext(ctx)
				}),
				stdgrpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var trailer metadata.MD
			_, err = rtiv1.NewObjectServiceClient(conn).Retract(ctx, &rtiv1.RetractRequest{
				WireVersion: wireV1(), FederationName: "f", FederateHandle: 1, MessageRetractionHandle: 7,
			}, stdgrpc.Trailer(&trailer))
			got := trailer.Get(specExceptionTrailerKey)
			if tc.err == nil {
				if err != nil || len(got) != 0 {
					t.Fatalf("successful retraction: err=%v trailer=%v", err, got)
				}
				return
			}
			if status.Code(err) != codes.FailedPrecondition || len(got) != 1 || got[0] != "MessageCanNoLongerBeRetracted" {
				t.Fatalf("late retraction: err=%v trailer=%v", err, got)
			}
		})
	}
}
