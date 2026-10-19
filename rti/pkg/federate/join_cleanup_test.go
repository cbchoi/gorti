package federate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type recoverableJoinClient struct {
	timeSelectionClient
	response *rtiv1.JoinFederationResponse
	token    []byte
	failures []error
	t        *testing.T
}

func (c *recoverableJoinClient) JoinFederation(context.Context, *rtiv1.JoinFederationRequest, ...grpc.CallOption) (*rtiv1.JoinFederationResponse, error) {
	return c.response, nil
}

func (c *recoverableJoinClient) ResignFederation(ctx context.Context, req *rtiv1.ResignFederationRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.resignCalls++
	md, _ := metadata.FromOutgoingContext(ctx)
	got := md.Get("gorti-session-token-bin")
	if len(got) != 1 || !bytes.Equal([]byte(got[0]), c.token) {
		c.t.Errorf("cleanup must use exactly the returned capability")
		return nil, status.Error(codes.Unauthenticated, "wrong capability")
	}
	if req.GetFederationName() != testFederationName || req.GetFederateHandle() != 9 || ctx.Err() != nil {
		c.t.Errorf("incorrect cleanup identity/context: %v %v", req, ctx.Err())
	}
	if len(md.Get("caller")) != 1 || md.Get("caller")[0] != testRetainedMetadata {
		c.t.Error("caller metadata lost")
	}
	if len(c.failures) != 0 {
		err := c.failures[0]
		c.failures = c.failures[1:]
		return nil, err
	}
	return &rtiv1.Empty{}, nil
}

func TestFailedJoinCleanupRetainsAuthenticatedRetry(t *testing.T) {
	for _, failure := range []string{"unsupported-time", "additional-fom", "callback-open"} {
		t.Run(failure, func(t *testing.T) {
			token := bytes.Repeat([]byte{'q'}, 32)
			cleanupErr := errors.New("temporary cleanup failure")
			retryErr := status.Error(codes.Unavailable, "retry unavailable")
			client := &recoverableJoinClient{t: t, token: token, failures: []error{cleanupErr, retryErr}, response: &rtiv1.JoinFederationResponse{
				FederateHandle: 9, SessionToken: append([]byte(nil), token...),
			}}
			spec := FederationSpec{Name: testFederationName}
			conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
			switch failure {
			case "unsupported-time":
				client.response.LogicalTimeImplementationName = "unsupportedTime"
			case "additional-fom":
				spec.AdditionalFOMModules = []FOMModule{{Path: "additional.xml"}}
			case "callback-open":
				conn.stream = &callbackOpenFailureClient{batchErr: status.Error(codes.Unavailable, "stream unavailable")}
			}
			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("gorti-session-token-bin", "stale-caller-token", "caller", testRetainedMetadata))
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			joined, err := conn.JoinFederation(cancelled, spec, "alice")
			if joined != nil || !errors.Is(err, cleanupErr) {
				t.Fatalf("join=%v err=%v", joined, err)
			}
			if failure == "unsupported-time" && !errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
				t.Fatal(err)
			}
			var recovery interface {
				error
				RetryResign(context.Context) error
				FederationName() string
				Handle() uint64
			}
			if !errors.As(err, &recovery) {
				t.Fatalf("failed cleanup identity was lost: %v", err)
			}
			if recovery.FederationName() != testFederationName || recovery.Handle() != 9 {
				t.Fatal("recovery identity lost")
			}
			if text := fmt.Sprintf("%v %+v %#v", err, err, err); strings.Contains(text, string(token)) || strings.Contains(text, "sessionToken") {
				t.Fatal("error formatting leaked capability")
			}
			client.response.SessionToken[0] = 'x'
			if err := recovery.RetryResign(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled retry=%v", err)
			}
			if err := recovery.RetryResign(ctx); !errors.Is(err, retryErr) {
				t.Fatalf("failed retry=%v", err)
			}
			if err := recovery.RetryResign(ctx); err != nil {
				t.Fatal(err)
			}
			if err := recovery.RetryResign(ctx); err != nil {
				t.Fatal(err)
			}
			if client.resignCalls != 3 {
				t.Fatalf("cleanup calls=%d", client.resignCalls)
			}
			md, _ := metadata.FromOutgoingContext(ctx)
			if md.Get("gorti-session-token-bin")[0] != "stale-caller-token" {
				t.Fatal("caller metadata mutated")
			}
		})
	}
}
