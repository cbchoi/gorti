// MomService gRPC handler — translates rti.v1.MomService RPCs into
// calls on rti/internal/mom.Manager (via the core.ManagementObjectModel
// interface).
//
// Owner: Agent A — M12 W3 (cut-3 gRPC exposure of cut-2 mom.Manager).
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

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
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
// request — this matches the §10 standard's "federation does not
// exist" semantics (zero-valued attributes, not an error).
func (s *momService) QueryFederationAttributes(
	_ context.Context,
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
	return resp, nil
}

// QueryFederateAttributes returns one HLAfederate MOM snapshot. The
// response.found field discriminates between "federate is tracked"
// (true; remaining fields populated) and "federate not tracked"
// (false; remaining fields zero-valued). Returning an explicit found
// bool rather than a NotFound status keeps the polling pattern cheap:
// federates can poll without burning per-call status-decode work.
func (s *momService) QueryFederateAttributes(
	_ context.Context,
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
	return &rtiv1.QueryFederateAttributesResponse{
		Found:                true,
		FederateHandle:       uint64(attrs.Handle),
		FederateName:         attrs.Name,
		FederateType:         attrs.Type,
		TimeRegulating:       attrs.TimeRegulating,
		TimeConstrained:      attrs.TimeConstrained,
		LogicalTime:          &rtiv1.LogicalTime{Value: float64(attrs.LogicalTime)},
		Lookahead:            &rtiv1.LogicalTime{Value: float64(attrs.Lookahead)},
		InteractionsSent:     attrs.InteractionsSent,
		InteractionsReceived: attrs.InteractionsReceived,
		UpdatesSent:          attrs.UpdatesSent,
		ReflectionsReceived:  attrs.ReflectionsReceived,
	}, nil
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
