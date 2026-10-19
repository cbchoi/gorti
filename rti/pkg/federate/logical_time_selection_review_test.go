package federate

import (
	"bytes"
	"context"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type reviewAuthenticatedTimeSelectionClient struct {
	timeSelectionClient
	token                []byte
	authenticatedCleanup bool
}

func (c *reviewAuthenticatedTimeSelectionClient) JoinFederation(context.Context, *rtiv1.JoinFederationRequest, ...grpc.CallOption) (*rtiv1.JoinFederationResponse, error) {
	return &rtiv1.JoinFederationResponse{
		FederateHandle:                9,
		SessionToken:                  c.token,
		LogicalTimeImplementationName: "unsupportedTime",
	}, nil
}

func (c *reviewAuthenticatedTimeSelectionClient) ResignFederation(ctx context.Context, req *rtiv1.ResignFederationRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.resignCalls++
	md, _ := metadata.FromOutgoingContext(ctx)
	tokens := md.Get("gorti-session-token-bin")
	c.authenticatedCleanup = req.GetFederateHandle() == 9 && len(tokens) == 1 && bytes.Equal([]byte(tokens[0]), c.token)
	if !c.authenticatedCleanup {
		return nil, status.Error(codes.Unauthenticated, "cleanup requires the returned membership capability")
	}
	return &rtiv1.Empty{}, nil
}

func TestReviewUnsupportedTimeJoinCleanupUsesReturnedCapability(t *testing.T) {
	client := &reviewAuthenticatedTimeSelectionClient{token: bytes.Repeat([]byte{0x31}, 32)}
	conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
	joined, err := conn.JoinFederation(context.Background(), FederationSpec{Name: testFederationName}, "alice")
	if joined != nil || !errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
		t.Fatalf("unsupported time was not rejected: joined=%v err=%v", joined, err)
	}
	if client.resignCalls != 1 || !client.authenticatedCleanup {
		t.Fatalf("failed join abandoned its authenticated identity: cleanup calls=%d authenticated=%t err=%v", client.resignCalls, client.authenticatedCleanup, err)
	}
}
