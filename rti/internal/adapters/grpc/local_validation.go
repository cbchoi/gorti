package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *localLRCService) GetLocalValidationState(ctx context.Context, req *rtiv1.GetLocalValidationStateRequest) (*rtiv1.LocalValidationState, error) {
	if req == nil || req.GetSnapshotId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "nonzero snapshot ID required")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.server.localValidationState == nil {
		return nil, status.Error(codes.Unimplemented, "authoritative local validation snapshot unavailable")
	}
	state, err := s.server.localValidationState.LocalValidationState(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), req.GetExpectedFederationGeneration(), req.GetExpectedCallbackEpoch(), req.GetSnapshotId())
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	out := &rtiv1.LocalValidationState{FederationGeneration: state.FederationGeneration, FederateHandle: uint64(state.Federate), CallbackEpoch: state.CallbackEpoch, SnapshotId: state.SnapshotID, FomView: encodeFOMView(state.FOM)}
	for _, p := range state.Publications {
		attrs := make([]uint64, len(p.Attributes))
		for i, a := range p.Attributes {
			attrs[i] = uint64(a)
		}
		out.Publications = append(out.Publications, &rtiv1.PublishedObjectClass{ObjectClassHandle: uint64(p.Class), AttributeHandles: attrs})
	}
	for _, c := range state.PublishedInteractions {
		out.PublishedInteractionClasses = append(out.PublishedInteractionClasses, uint64(c))
	}
	for _, obj := range state.Objects {
		attrs := make([]uint64, len(obj.Owned))
		for i, a := range obj.Owned {
			attrs[i] = uint64(a)
		}
		out.Objects = append(out.Objects, &rtiv1.KnownObjectState{ObjectHandle: uint64(obj.Object), ObjectClassHandle: uint64(obj.Class), OwnedAttributeHandles: attrs})
	}
	return out, nil
}
