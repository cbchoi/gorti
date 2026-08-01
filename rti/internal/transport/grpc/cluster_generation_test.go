package grpc

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/cluster"
	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestP0NotifyAssignmentRejectsStaleGeneration(t *testing.T) {
	mgr := cluster.New("self", "127.0.0.1:1")
	svc := NewClusterService(mgr, nil)
	svc.SetGenerationResolver(func(fed core.FederationName) (uint64, bool) {
		return 7, fed == "fed"
	})
	stale := uint64(6)
	_, err := svc.NotifyAssignment(context.Background(), &rtiv1.NotifyAssignmentRequest{
		WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:               "fed",
		HostNodeId:                   "remote",
		HostAddress:                  "127.0.0.1:2",
		ExpectedFederationGeneration: &stale,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("NotifyAssignment stale status = %v, want FailedPrecondition", status.Code(err))
	}
	if got := mgr.Lookup("fed"); got.Status != cluster.StatusNotFound {
		t.Fatalf("stale assignment mutated table: %+v", got)
	}

	current := uint64(7)
	if _, err := svc.NotifyAssignment(context.Background(), &rtiv1.NotifyAssignmentRequest{
		WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:               "fed",
		HostNodeId:                   "remote",
		HostAddress:                  "127.0.0.1:2",
		ExpectedFederationGeneration: &current,
	}); err != nil {
		t.Fatalf("NotifyAssignment current: %v", err)
	}
	if got := mgr.Lookup("fed"); got.Generation != 7 || got.HostNodeID != "remote" {
		t.Fatalf("current assignment = %+v", got)
	}
}
