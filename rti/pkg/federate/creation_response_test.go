package federate

import (
	"context"
	"errors"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
)

type creationEvidenceClient struct {
	timeSelectionClient
	destroyCalls   int
	destroyRequest *rtiv1.DestroyFederationRequest
}

func (c *creationEvidenceClient) DestroyFederation(_ context.Context, req *rtiv1.DestroyFederationRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.destroyCalls++
	c.destroyRequest = req
	return &rtiv1.Empty{}, nil
}

func TestContradictoryCreateRetainsExecutionEvidenceWithoutAutoDestroy(t *testing.T) {
	for _, join := range []bool{false, true} {
		client := &creationEvidenceClient{timeSelectionClient: timeSelectionClient{createName: "unsupportedTime"}}
		conn := &Connection{cc: &grpc.ClientConn{}, fed: client}
		var created FederationCreation
		var err error
		if join {
			var member *Federate
			member, err = conn.JoinFederation(context.Background(), FederationSpec{Name: "created-execution"}, "alice")
			if member != nil {
				t.Fatal("contradictory creation returned usable membership")
			}
		} else {
			created, err = conn.CreateFederation(context.Background(), FederationSpec{Name: "created-execution"})
		}
		if err == nil || errors.Is(err, ErrCouldNotCreateLogicalTimeFactory) {
			t.Fatalf("reported a clean factory failure after creation: %v", err)
		}
		var responseErr *FederationCreationResponseError
		if !errors.As(err, &responseErr) || !errors.Is(err, ErrRTIInternal) {
			t.Fatalf("missing post-create protocol error: %v", err)
		}
		if responseErr.FederationName != "created-execution" || responseErr.Creation.FederationGeneration != 4 || responseErr.Creation.EffectiveSeed != 7 || responseErr.Creation.LogicalTimeImplementationName != "unsupportedTime" {
			t.Fatalf("lost successful response evidence: %+v", responseErr)
		}
		if !join && created != responseErr.Creation {
			t.Fatalf("result lost creation evidence: %+v", created)
		}
		if !strings.Contains(err.Error(), "server already created") || !strings.Contains(err.Error(), "created-execution") || !strings.Contains(err.Error(), "unsupportedTime") {
			t.Fatalf("message hides committed creation: %v", err)
		}
		if client.destroyCalls != 0 || client.joinCalls != 0 || client.resignCalls != 0 {
			t.Fatal("automatic mutation after contradictory creation")
		}
		if conn.generations["created-execution"] != 4 {
			t.Fatal("manual cleanup generation lost")
		}
		if err := conn.DestroyFederation(context.Background(), "created-execution"); err != nil {
			t.Fatal(err)
		}
		if client.destroyCalls != 1 || client.destroyRequest.ExpectedFederationGeneration == nil || client.destroyRequest.GetExpectedFederationGeneration() != 4 {
			t.Fatal("explicit cleanup lost generation fencing")
		}
	}
}

func TestUnjoinedFederateDoesNotAdvertiseFloat64(t *testing.T) {
	for _, member := range []*Federate{nil, {}} {
		if member.LogicalTimeImplementationName() != "" {
			t.Fatal("unjoined member advertised usable Float64")
		}
	}
	var cleanup any = &JoinCleanupError{}
	if _, usable := cleanup.(interface{ LogicalTimeImplementationName() string }); usable {
		t.Fatal("cleanup recovery exposes logical-time factory")
	}
}
