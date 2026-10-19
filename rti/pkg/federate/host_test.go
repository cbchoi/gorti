package federate

import (
	"context"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type hostJoinClient struct {
	compensatingFederationClient
	inspect func(context.Context)
	stop    error
}

func (c *hostJoinClient) JoinFederation(ctx context.Context, _ *rtiv1.JoinFederationRequest, _ ...grpc.CallOption) (*rtiv1.JoinFederationResponse, error) {
	c.inspect(ctx)
	return nil, c.stop
}

func TestGoJoinSendsOnlyExplicitHost(t *testing.T) {
	for _, host := range []string{"", "sim-host.example", "sim-\uac00.example"} {
		t.Run(host, func(t *testing.T) {
			t.Setenv("GORTI_FEDERATE_HOST", host)
			stop := errors.New("stop after inspecting join")
			calls := 0
			client := &hostJoinClient{stop: stop, inspect: func(ctx context.Context) {
				calls++
				md, _ := metadata.FromOutgoingContext(ctx)
				values := md.Get("gorti-federate-host-bin")
				if host == "" && len(values) != 0 {
					t.Errorf("unexpected host: %q", values)
				}
				if host != "" && (len(values) != 1 || values[0] != host) {
					t.Errorf("host=%q, want %q", values, host)
				}
				if md.Get("caller")[0] != testRetainedMetadata {
					t.Error("caller metadata lost")
				}
			}}
			conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("caller", testRetainedMetadata))
			_, err := conn.JoinFederation(ctx, FederationSpec{Name: testFederationName}, "alice")
			if !errors.Is(err, stop) || calls != 1 {
				t.Fatalf("join=%v calls=%d", err, calls)
			}
			md, _ := metadata.FromOutgoingContext(ctx)
			if len(md.Get("gorti-federate-host-bin")) != 0 {
				t.Fatal("caller context mutated")
			}
		})
	}
}
