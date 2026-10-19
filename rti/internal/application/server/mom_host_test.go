package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestMOMHostUsesExplicitJoinMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv, err := newRTID(rtidConfig{OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(lis) }()
	t.Cleanup(func() {
		srv.sessions.Close()
		_ = srv.momMgr.FederationDestroyed(context.Background(), "hosts")
		srv.grpcS.Stop()
		_ = lis.Close()
		<-done
		_ = srv.plugins.Close()
	})
	conn, err := stdgrpc.NewClient("passthrough:///mom-host", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	feds := rtiv1.NewFederationServiceClient(conn)
	if _, err := feds.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "hosts", FomModules: []*rtiv1.FOMModule{{Path: "empty.xml", Xml: []byte(`<objectModel/>`)}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		values  []string
		want    string
		invalid bool
	}{
		{"explicit", []string{"simulation-host.example"}, "simulation-host.example", false},
		{"unicode", []string{"sim-\uac00.example"}, "sim-\uac00.example", false},
		{"omitted", nil, "", false},
		{"empty", []string{""}, "", true},
		{"duplicate", []string{"a", "b"}, "", true},
		{"invalid-utf8", []string{string([]byte{0xff})}, "", true},
		{"control", []string{"a\nb"}, "", true},
		{"oversized", []string{strings.Repeat("a", 1025)}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			joinCtx := ctx
			if tc.values != nil {
				joinCtx = metadata.NewOutgoingContext(ctx, metadata.MD{"gorti-federate-host-bin": tc.values})
			}
			req := &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: "hosts", FederateName: tc.name}
			joined, err := feds.JoinFederation(joinCtx, req)
			if tc.invalid {
				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf("invalid host admitted: %v", err)
				}
				// Validation must precede roster or capability mutation.
				joined, err = feds.JoinFederation(ctx, req)
			}
			if err != nil {
				t.Fatal(err)
			}
			attrs, err := rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: "hosts", FederateHandle: joined.FederateHandle})
			if err != nil {
				t.Fatal(err)
			}
			value, ok := attrs.AttributeValues["HLAfederateHost"]
			if tc.want == "" {
				if ok || attrs.UnavailableAttributes["HLAfederateHost"] == "" {
					t.Fatalf("unreported host fabricated: %v", attrs)
				}
			} else if !ok || catalogRPCString(t, value) != tc.want || attrs.UnavailableAttributes["HLAfederateHost"] != "" {
				t.Fatalf("host not retained: values=%v missing=%v", attrs.AttributeValues, attrs.UnavailableAttributes)
			}
		})
	}
}
