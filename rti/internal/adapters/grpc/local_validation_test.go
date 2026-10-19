package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type validationWireSource struct {
	t     *testing.T
	calls int
}

func (s *validationWireSource) LocalValidationState(_ context.Context, fed core.FederationName, h core.FederateHandle, generation, epoch, id uint64) (core.LocalValidationState, error) {
	s.calls++
	if fed != "fed" || h != 7 || generation != 9 || epoch != 2 || id != 12 {
		s.t.Errorf("snapshot identity changed: %s %d %d %d %d", fed, h, generation, epoch, id)
	}
	return core.LocalValidationState{FederationGeneration: 9, Federate: 7, CallbackEpoch: 2, SnapshotID: 12, FOM: core.FOMView{Revision: 3, ObjectClasses: []core.FOMObjectClassBinding{{Handle: 19, Name: "Thing", Attributes: []core.FOMAttributeBinding{{Handle: 7, Name: "value"}}}}}, Publications: []core.LocalObjectPublication{{Class: 19, Attributes: []core.AttributeHandle{7}}}, Objects: []core.LocalKnownObject{{Object: 80, Class: 19, Owned: []core.AttributeHandle{7}}}}, nil
}

func TestLocalValidationWireRequiresSourceAndPreservesBarrierIdentity(t *testing.T) {
	s := &localLRCService{server: &Server{}}
	req := &rtiv1.GetLocalValidationStateRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 7, ExpectedFederationGeneration: 9, ExpectedCallbackEpoch: 2, SnapshotId: 12}
	if _, err := s.GetLocalValidationState(context.Background(), req); status.Code(err) != codes.Unimplemented {
		t.Fatalf("absent mandatory source: %v", err)
	}
	source := &validationWireSource{t: t}
	s.server.localValidationState = source
	r, err := s.GetLocalValidationState(context.Background(), req)
	if err != nil || r.FederateHandle != 7 || r.CallbackEpoch != 2 || r.SnapshotId != 12 || r.FomView.ObjectClasses[0].Members[0].Handle != 7 || r.Objects[0].OwnedAttributeHandles[0] != 7 {
		t.Fatalf("wire state %v %v", r, err)
	}
	req.SnapshotId = 0
	if _, err := s.GetLocalValidationState(context.Background(), req); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("zero snapshot ID: %v", err)
	}
	if source.calls != 1 {
		t.Fatal("invalid request reached source")
	}
	e, err := toFederateEvent(core.LocalValidationBarrierEvent{SnapshotID: 12, Federate: 7, CallbackEpoch: 2})
	if err != nil || e.GetLocalValidationBarrier().SnapshotId != 12 || e.GetLocalValidationBarrier().FederateHandle != 7 || e.GetLocalValidationBarrier().CallbackEpoch != 2 || len(e.CallbackReceipt) != 0 {
		t.Fatalf("internal barrier changed: %v %v", e, err)
	}
}
