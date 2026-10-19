// MomService gRPC handler — translates rti.v1.MomService RPCs into
// calls on rti/internal/mom.Manager (via the core.ManagementObjectModel
// interface).
//
// M12 W3 exposes the cut-2 mom.Manager through gRPC.
//
// Composition: server.go wires a *momService into the composed Server
// via newMomService(core.ManagementObjectModel). The proto + manager
// were both frozen at M11; this handler is the wire bridge.
//
// Read-only contract: every RPC is a snapshot accessor over the
// manager's state. No RPC mutates MOM state. MOM mutations come from
// the runtime composition hooks (federation lifecycle hooks +
// per-federate counter increments off the dispatcher fan-out), not
// from federate calls.

package grpc

import (
	"context"

	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// momClassFederation / momClassFederate mirror mom.ClassHLAfederation /
// mom.ClassHLAfederate without importing the mom package — the handler
// binds to core.ManagementObjectModel so a non-default MOM impl wired
// at the composition root flows through unchanged. Constants kept in
// sync with rti/internal/mom/manager.go.
const (
	momClassFederation = "HLAobjectRoot.HLAmanager.HLAfederation"
	momClassFederate   = "HLAobjectRoot.HLAmanager.HLAfederate"
)

// momService is the concrete MomServiceServer impl. It embeds
// UnimplementedMomServiceServer for forward compatibility (gRPC v1.65+
// requirement; protoc-gen-go-grpc emits a deprecation warning otherwise).
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.3): the handler binds to core.ManagementObjectModel instead of
// the concrete *mom.Manager so alternative implementations can be
// wired at the composition root.
type momService struct {
	rtiv1.UnimplementedMomServiceServer
	mgr core.ManagementObjectModel
}

func newMomService(mgr core.ManagementObjectModel) *momService {
	return &momService{mgr: mgr}
}

// QueryFederationAttributes returns a snapshot of the HLAfederation
// MOM object's attributes for the named federation. When the
// federation is not currently tracked, all repeated fields are
// returned empty and the federation_name is echoed back from the
// request for compatibility with the original introspection RPC.
func (s *momService) QueryFederationAttributes(
	ctx context.Context,
	req *rtiv1.QueryFederationAttributesRequest,
) (*rtiv1.QueryFederationAttributesResponse, error) {
	if req == nil {
		return nil, nilRequest("QueryFederationAttributes")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	attrs, ok := s.mgr.QueryFederation(core.FederationName(req.GetFederationName()))
	if !ok {
		return &rtiv1.QueryFederationAttributesResponse{
			FederationName: req.GetFederationName(),
		}, nil
	}
	resp := &rtiv1.QueryFederationAttributesResponse{
		FederationName: string(attrs.Name),
	}
	if len(attrs.FederateHandles) > 0 {
		resp.FederateHandles = make([]uint64, len(attrs.FederateHandles))
		for i, h := range attrs.FederateHandles {
			resp.FederateHandles[i] = uint64(h)
		}
	}
	if len(attrs.FOMModuleNames) > 0 {
		resp.FomModuleNames = make([]string, len(attrs.FOMModuleNames))
		copy(resp.FomModuleNames, attrs.FOMModuleNames)
	}
	if source, ok := s.mgr.(interface {
		FederationMetricValues(context.Context, core.FederationName) (mom.MetricSnapshot, error)
	}); ok {
		metrics, err := source.FederationMetricValues(ctx, core.FederationName(req.GetFederationName()))
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		resp.AttributeValues, resp.UnavailableAttributes = metrics.Values, metrics.Unavailable
	}
	return resp, nil
}

// QueryFederateAttributes returns one HLAfederate MOM snapshot. The
// response.found field discriminates between "federate is tracked"
// (true; remaining fields populated) and "federate not tracked"
// (false; remaining fields zero-valued). Returning an explicit found
// bool rather than a NotFound status keeps the polling pattern cheap:
// federates can poll without burning per-call status-decode work.
func (s *momService) QueryFederateAttributes(
	ctx context.Context,
	req *rtiv1.QueryFederateAttributesRequest,
) (*rtiv1.QueryFederateAttributesResponse, error) {
	if req == nil {
		return nil, nilRequest("QueryFederateAttributes")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	attrs, ok := s.mgr.QueryFederate(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	)
	if !ok {
		return &rtiv1.QueryFederateAttributesResponse{Found: false}, nil
	}
	logicalTime := &rtiv1.LogicalTime{Value: float64(attrs.LogicalTime)}
	lookahead := &rtiv1.LogicalTime{Value: float64(attrs.Lookahead)}
	if attrs.LogicalTimeValue != nil || attrs.LookaheadValue != nil {
		if attrs.LogicalTimeValue == nil || attrs.LookaheadValue == nil {
			return nil, status.Error(codes.Internal, "MOM snapshot has incomplete exact time values")
		}
		representation := attrs.LogicalTimeValue.Representation()
		legacyTime, exactTime, err := wire.EncodeLogicalTimePair(representation, *attrs.LogicalTimeValue)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "MOM snapshot logical time: %v", err)
		}
		legacyLookahead, exactLookahead, err := wire.EncodeLogicalTimeIntervalPair(representation, *attrs.LookaheadValue)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "MOM snapshot lookahead: %v", err)
		}
		// Integer64 retains zero legacy slots; Float64's legacy slots must agree.
		if float64(attrs.LogicalTime) != legacyTime || float64(attrs.Lookahead) != legacyLookahead {
			return nil, status.Error(codes.Internal, "MOM snapshot legacy and exact time values conflict")
		}
		logicalTime = &rtiv1.LogicalTime{Value: legacyTime, LogicalTimeValue: exactTime}
		lookahead = &rtiv1.LogicalTime{Value: legacyLookahead, LogicalTimeValue: exactLookahead}
	}
	resp := &rtiv1.QueryFederateAttributesResponse{
		Found:                true,
		FederateHandle:       uint64(attrs.Handle),
		FederateName:         attrs.Name,
		FederateType:         attrs.Type,
		TimeRegulating:       attrs.TimeRegulating,
		TimeConstrained:      attrs.TimeConstrained,
		LogicalTime:          logicalTime,
		Lookahead:            lookahead,
		InteractionsSent:     attrs.InteractionsSent,
		InteractionsReceived: attrs.InteractionsReceived,
		UpdatesSent:          attrs.UpdatesSent,
		ReflectionsReceived:  attrs.ReflectionsReceived,
	}
	if source, ok := s.mgr.(interface {
		FederateMetricValues(context.Context, core.FederationName, core.FederateHandle) (mom.MetricSnapshot, error)
	}); ok {
		metrics, err := source.FederateMetricValues(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()))
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		resp.AttributeValues, resp.UnavailableAttributes = metrics.Values, metrics.Unavailable
	}
	return resp, nil
}

// EnumerateMomInstances returns the federation MOM object handle (one
// HLAfederation per federation) plus every per-federate HLAfederate
// instance. The federate ordering follows the mgr's sorted handle
// list so polling clients see a deterministic ordering across calls
// (the federation manager keeps handles dense + monotonic; the MOM
// snapshot keeps the federate-handle list sorted).
//
// Empty federation (no joined federates) returns a single MomInstance
// for the federation. Unknown federation returns an empty instances
// list — same "zero-valued" semantics as QueryFederationAttributes.
func (s *momService) EnumerateMomInstances(
	_ context.Context,
	req *rtiv1.EnumerateMomInstancesRequest,
) (*rtiv1.EnumerateMomInstancesResponse, error) {
	if req == nil {
		return nil, nilRequest("EnumerateMomInstances")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	attrs, ok := s.mgr.QueryFederation(core.FederationName(req.GetFederationName()))
	if !ok {
		return &rtiv1.EnumerateMomInstancesResponse{}, nil
	}
	resp := &rtiv1.EnumerateMomInstancesResponse{
		Instances: make([]*rtiv1.MomInstance, 0, 1+len(attrs.FederateHandles)),
	}
	resp.Instances = append(resp.Instances, &rtiv1.MomInstance{
		ClassName:    momClassFederation,
		InstanceName: string(attrs.Name),
		// FederateHandle = 0 — federation singleton has no federate handle.
	})
	for _, h := range attrs.FederateHandles {
		fed, ok := s.mgr.QueryFederate(core.FederationName(req.GetFederationName()), h)
		if !ok {
			// Federate listed in the federation snapshot but its
			// HLAfederate record was not found (race with a concurrent
			// resign). Skip — the federation snapshot is the
			// authoritative roster, and any straggler will fall off
			// on the next call.
			continue
		}
		resp.Instances = append(resp.Instances, &rtiv1.MomInstance{
			ClassName:      momClassFederate,
			FederateHandle: uint64(fed.Handle),
			InstanceName:   fed.Name,
		})
	}
	return resp, nil
}
