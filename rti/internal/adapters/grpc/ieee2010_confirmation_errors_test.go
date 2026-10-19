package grpc

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestIEEE2010ConfirmationSentinelMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		name string
	}{
		{core.ErrOwnershipConfirmationNotRequested, "AttributeDivestitureWasNotRequested"},
		{core.ErrOwnershipNoAcquisitionPending, "NoAcquisitionPending"},
	} {
		for _, err := range []error{tc.err, fmt.Errorf("confirm: %w", tc.err)} {
			if name, ok := specExceptionName(err); !ok || name != tc.name {
				t.Errorf("mapping(%v) = %q, %v; want %s", err, name, ok, tc.name)
			}
			if got := status.Code(errToStatus(context.Background(), err)); got != codes.FailedPrecondition {
				t.Errorf("status(%v) = %v", err, got)
			}
		}
	}
	if name, ok := specExceptionName(core.ErrOwnershipNotInTransfer); ok {
		t.Fatalf("ambiguous cancellation sentinel acquired a mapping: %s", name)
	}
}

func TestIEEE2010ConfirmationExceptionTrailers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mgr, err := ownership.New(ownership.Options{Outbox: stubOutboxForServerTest{}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	listener := bufconn.Listen(1 << 20)
	server := grpcgo.NewServer()
	rtiv1.RegisterOwnershipServiceServer(server, newOwnershipService(mgr))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///confirmation", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewOwnershipServiceClient(conn)
	check := func(want string) {
		t.Helper()
		var trailer metadata.MD
		_, err := client.ConfirmDivestiture(ctx, &rtiv1.ConfirmDivestitureRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: 100, AttributeHandles: []uint64{1}}, grpcgo.Trailer(&trailer))
		if want == "" {
			if err != nil || len(trailer.Get(specExceptionTrailerKey)) != 0 {
				t.Fatalf("success: %v, trailer=%v", err, trailer)
			}
			return
		}
		if status.Code(err) != codes.FailedPrecondition || !reflect.DeepEqual(trailer.Get(specExceptionTrailerKey), []string{want}) {
			t.Fatalf("confirmation: %v, trailer=%v; want %s", err, trailer, want)
		}
	}
	if err := mgr.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	check("AttributeDivestitureWasNotRequested")
	if err := mgr.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.CancelAcquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	check("NoAcquisitionPending")
	if err := mgr.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	check("")
}
