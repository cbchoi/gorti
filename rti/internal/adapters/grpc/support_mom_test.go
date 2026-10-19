package grpc

import (
	"context"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestMOMNormalizationValidatesFullHandleBeforeLocalIndex(t *testing.T) {
	s := newSupportService(nil)
	const handle = uint64(1)<<40 | 7
	s.normalizationMembers = func(core.FederationName) []core.FederateHandle {
		return []core.FederateHandle{1, core.FederateHandle(handle)}
	}
	ctx := context.Background()
	got, err := s.NormalizeFederateHandle(ctx, &rtiv1.NormalizeFederateHandleRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "f", FederateHandle: 1, TargetHandle: handle})
	if err != nil || got.GetValue() != 7 {
		t.Fatalf("normalized=%v err=%v", got, err)
	}
	if _, err := s.NormalizeFederateHandle(ctx, &rtiv1.NormalizeFederateHandleRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "f", FederateHandle: 1, TargetHandle: 9}); err == nil {
		t.Fatal("invalid target normalized")
	}
	if _, err := s.NormalizeFederateHandle(ctx, &rtiv1.NormalizeFederateHandleRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "f", FederateHandle: 1, TargetHandle: 7}); err == nil {
		t.Fatal("foreign generation accepted by truncated index")
	}
	for group := uint32(0); group < 7; group++ {
		got, err := s.NormalizeServiceGroup(ctx, &rtiv1.NormalizeServiceGroupRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "f", FederateHandle: 1, ServiceGroup: group})
		if err != nil || got.GetValue() != uint64(group) {
			t.Fatalf("group=%d value=%v err=%v", group, got, err)
		}
	}
	if _, err := s.NormalizeServiceGroup(ctx, &rtiv1.NormalizeServiceGroupRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "f", FederateHandle: 1, ServiceGroup: 7}); err == nil {
		t.Fatal("invalid group normalized")
	}
}

func TestMOMNormalizationExceptionsRealWire(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpcgo.NewServer()
	service := newSupportService(nil)
	service.normalizationMembers = func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }
	rtiv1.RegisterSupportServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///normalization", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := rtiv1.NewSupportServiceClient(conn)
	var trailer metadata.MD
	_, err = client.NormalizeFederateHandle(ctx, &rtiv1.NormalizeFederateHandleRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, TargetHandle: 9}, grpcgo.Trailer(&trailer))
	if err == nil || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != "InvalidFederateHandle" {
		t.Fatalf("handle exception=%v trailer=%v", err, trailer)
	}
	_, err = client.NormalizeServiceGroup(ctx, &rtiv1.NormalizeServiceGroupRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, ServiceGroup: 7}, grpcgo.Trailer(&trailer))
	if err == nil || len(trailer.Get(specExceptionTrailerKey)) != 1 || trailer.Get(specExceptionTrailerKey)[0] != "InvalidServiceGroup" {
		t.Fatalf("group exception=%v trailer=%v", err, trailer)
	}
}
