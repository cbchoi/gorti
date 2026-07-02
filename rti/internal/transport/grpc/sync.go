// SyncService gRPC handler — translates rti.v1.SyncService RPCs into
// calls on rti/internal/sync.Manager.
//
// Owner: Agent A — M12 W1 (cut-3 gRPC exposure of cut-2 sync.Manager).
//
// Composition: server.go wires a *syncService into the composed Server
// via newSyncService(*sync.Manager). The proto + manager were both
// frozen at M0 / M8; this handler is the wire bridge.

package grpc

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// syncService is the concrete SyncServiceServer impl. It embeds
// UnimplementedSyncServiceServer for forward compatibility (gRPC v1.65+
// requirement; protoc-gen-go-grpc emits a deprecation warning otherwise).
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.1): the handler binds to core.SyncCoordinator instead of the
// concrete *sync.Manager so alternative implementations can be wired in
// at the composition root.
type syncService struct {
	rtiv1.UnimplementedSyncServiceServer
	mgr core.SyncCoordinator
}

func newSyncService(mgr core.SyncCoordinator) *syncService {
	return &syncService{mgr: mgr}
}

// federateHandlesFromUint64s converts a slice of uint64 federate handles
// into the typed core.FederateHandle slice the manager expects. A nil
// input produces a nil slice (the manager treats nil as "all currently
// joined federates" per sync.MembersResolver semantics).
func federateHandlesFromUint64s(in []uint64) []core.FederateHandle {
	if in == nil {
		return nil
	}
	out := make([]core.FederateHandle, len(in))
	for i, h := range in {
		out[i] = core.FederateHandle(h)
	}
	return out
}

// syncRegistrantAware is the optional richer register entrypoint the
// production *sync.Manager exposes (M37 Agent EA): it carries the
// REGISTERING federate handle so the §4.12
// synchronizationPointRegistrationSucceeded / Failed ack events can
// target it. Duck-typed so core.SyncCoordinator keeps its frozen shape.
type syncRegistrantAware interface {
	RegisterBy(
		ctx context.Context,
		fed core.FederationName,
		registrant core.FederateHandle,
		label string,
		tag []byte,
		requiredFederates []core.FederateHandle,
	) error
}

// RegisterFederationSynchronizationPoint implements §4.6.
func (s *syncService) RegisterFederationSynchronizationPoint(
	ctx context.Context,
	req *rtiv1.RegisterSyncPointRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("RegisterFederationSynchronizationPoint")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	var err error
	if ra, ok := s.mgr.(syncRegistrantAware); ok {
		err = ra.RegisterBy(
			ctx,
			core.FederationName(req.GetFederationName()),
			core.FederateHandle(req.GetFederateHandle()),
			req.GetLabel(),
			req.GetTag(),
			federateHandlesFromUint64s(req.GetRequiredFederates()),
		)
	} else {
		err = s.mgr.Register(
			ctx,
			core.FederationName(req.GetFederationName()),
			req.GetLabel(),
			req.GetTag(),
			federateHandlesFromUint64s(req.GetRequiredFederates()),
		)
	}
	if err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// SynchronizationPointAchieved implements §4.7.
func (s *syncService) SynchronizationPointAchieved(
	ctx context.Context,
	req *rtiv1.AchieveSyncPointRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SynchronizationPointAchieved")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.Achieve(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		req.GetLabel(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}
