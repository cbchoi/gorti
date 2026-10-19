package grpc

import (
	"context"
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
)

func TestIEEE2010CancellationExceptionTrailersTCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mgr, err := ownership.New(ownership.Options{Outbox: stubOutboxForServerTest{}})
	if err != nil {
		t.Fatal(err)
	}
	mgr.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpcgo.NewServer()
	rtiv1.RegisterOwnershipServiceServer(server, newOwnershipService(mgr))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient(listener.Addr().String(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewOwnershipServiceClient(conn)
	check := func(acquire bool, caller uint64, attrs []uint64, want string, code codes.Code) {
		t.Helper()
		var trailer metadata.MD
		var err error
		if acquire {
			_, err = client.CancelAttributeOwnershipAcquisition(ctx, &rtiv1.CancelAcquireRequest{WireVersion: 1, FederationName: "fed", FederateHandle: caller, ObjectHandle: 100, AttributeHandles: attrs}, grpcgo.Trailer(&trailer))
		} else {
			_, err = client.CancelNegotiatedAttributeOwnershipDivestiture(ctx, &rtiv1.CancelDivestRequest{WireVersion: 1, FederationName: "fed", FederateHandle: caller, ObjectHandle: 100, AttributeHandles: attrs}, grpcgo.Trailer(&trailer))
		}
		var names []string
		if want != "" {
			names = []string{want}
		}
		if status.Code(err) != code || !reflect.DeepEqual(trailer.Get(specExceptionTrailerKey), names) {
			t.Errorf("acquire=%v caller=%d attrs=%v: error=%v trailer=%v; want %s/%s", acquire, caller, attrs, err, trailer, code, want)
		}
	}
	check(false, 1, []uint64{1}, "AttributeDivestitureWasNotRequested", codes.FailedPrecondition)
	check(false, 2, []uint64{1}, "AttributeNotOwned", codes.PermissionDenied)
	check(true, 2, []uint64{1}, "AttributeAcquisitionWasNotRequested", codes.FailedPrecondition)
	check(true, 1, []uint64{1}, "AttributeAlreadyOwned", codes.FailedPrecondition)
	if err := mgr.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	check(false, 1, []uint64{1, 2}, "AttributeDivestitureWasNotRequested", codes.FailedPrecondition)
	check(true, 2, []uint64{1, 2}, "AttributeAcquisitionWasNotRequested", codes.FailedPrecondition)
	check(false, 1, []uint64{1}, "", codes.OK)
	check(true, 2, []uint64{1}, "", codes.OK)
	check(false, 1, []uint64{1}, "AttributeDivestitureWasNotRequested", codes.FailedPrecondition)
	check(true, 2, []uint64{1}, "AttributeAcquisitionWasNotRequested", codes.FailedPrecondition)
}
