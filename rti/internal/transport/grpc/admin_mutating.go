// MutatingService gRPC handler — Phase 5 of docs/rtid-tui.md.
//
// Owner: Agent A — rtid-TUI Phase 5.
//
// AdminService remains read-only. The mutating ops (ForceResign,
// DestroyFederation) live on this separate proto service so the
// composition root can register / withhold them based on the
// --admin-mutating flag.
//
// Both RPCs MUST reuse the federation.Manager's existing primitives
// (ResignFederation, DestroyFederation) so the event-log entries and
// MOM hooks fire identically to the federate-initiated path. This is
// critical for replay determinism + observability symmetry — bypassing
// to a lower layer would silently break those guarantees.

package grpc

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// MutatingOptions bundles the MutatingService handler's dependencies.
// Federations is REQUIRED — without it the handler has no federation
// roster to mutate. Version + StartedAt are surfaced through the
// Probe RPC (forward-compatible parity with AdminService.Status).
type MutatingOptions struct {
	// Federations is the federation lifecycle source. The handler
	// calls ResignFederation / DestroyFederation directly on it so
	// every mutation flows through the same code path the federate
	// services use — preserving event-log + MOM hook symmetry.
	Federations core.FederationStore

	// Version is the rtid build version returned in Probe.
	Version string
}

// mutatingService is the concrete MutatingServiceServer impl.
type mutatingService struct {
	rtiv1.UnimplementedMutatingServiceServer
	opts MutatingOptions
}

// newMutatingService constructs the handler. Federations may be nil
// in tests that exercise only Probe (the mutating RPCs return
// FailedPrecondition when nil).
func newMutatingService(opts MutatingOptions) *mutatingService {
	return &mutatingService{opts: opts}
}

// RegisterMutatingService attaches a MutatingService handler to the
// given gRPC server. ONLY called by the composition root when the
// safety gate (--admin-mutating=true + loopback bind) has been
// satisfied; AdminService callers do NOT register this.
//
// grpcServer is typed as `any` for symmetry with RegisterAdminService.
func RegisterMutatingService(grpcServer any, opts MutatingOptions) error {
	gs, ok := grpcServer.(grpc.ServiceRegistrar)
	if !ok {
		return fmt.Errorf("transport/grpc: RegisterMutatingService: want grpc.ServiceRegistrar, got %T", grpcServer)
	}
	rtiv1.RegisterMutatingServiceServer(gs, newMutatingService(opts))
	return nil
}

// --- Probe -----------------------------------------------------------------

// Probe returns rtid_version + mutating_enabled=true so rti-top can
// detect the service's presence and decide whether to render X / D
// keybindings.
func (s *mutatingService) Probe(_ context.Context, req *rtiv1.MutatingProbeRequest) (*rtiv1.MutatingProbeResponse, error) {
	if req == nil {
		return nil, nilRequest("Probe")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	return &rtiv1.MutatingProbeResponse{
		RtidVersion:     versionOrDefault(s.opts.Version),
		MutatingEnabled: true,
	}, nil
}

// --- ForceResign -----------------------------------------------------------

// ForceResign reuses Federations.ResignFederation so the eventlog +
// MOM hooks fire identically to the federate-initiated path. The
// already_resigned reply field is true when the federate had already
// left the federation between the operator's confirmation and the
// RPC arriving — surfaced as success-equivalent so the rti-top status
// line stays uniform.
func (s *mutatingService) ForceResign(ctx context.Context, req *rtiv1.ForceResignRequest) (*rtiv1.ForceResignResponse, error) {
	if req == nil {
		return nil, nilRequest("ForceResign")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.opts.Federations == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"MutatingService.ForceResign: Federations source is not wired")
	}
	fed := req.GetFederationName()
	if fed == "" {
		return nil, status.Error(codes.InvalidArgument,
			"MutatingService.ForceResign: federation_name is required")
	}
	if req.GetFederateHandle() == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"MutatingService.ForceResign: federate_handle is required")
	}
	err := s.opts.Federations.ResignFederation(
		ctx,
		core.FederationName(fed),
		core.FederateHandle(req.GetFederateHandle()),
		core.ResignActionUnconditionallyDivestAttributes,
	)
	if errors.Is(err, core.ErrFederateNotJoined) {
		// Idempotent success — the operator's intent is satisfied.
		return &rtiv1.ForceResignResponse{AlreadyResigned: true}, nil
	}
	if err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.ForceResignResponse{}, nil
}

// --- DestroyFederation -----------------------------------------------------

// DestroyFederation reuses Federations.DestroyFederation so the
// eventlog + MOM hooks fire identically to the federate-initiated
// path. When evict_joined_federates is true the handler first walks
// the roster and force-resigns every joined federate; the resulting
// evicted_handles slice tells the operator which federates were
// kicked. When false, the handler refuses with the federate-facing
// FR-FM-5 contract (ErrFederationHasFederatesJoined).
func (s *mutatingService) DestroyFederation(ctx context.Context, req *rtiv1.AdminDestroyFederationRequest) (*rtiv1.AdminDestroyFederationResponse, error) {
	if req == nil {
		return nil, nilRequest("DestroyFederation")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.opts.Federations == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"MutatingService.DestroyFederation: Federations source is not wired")
	}
	fed := req.GetFederationName()
	if fed == "" {
		return nil, status.Error(codes.InvalidArgument,
			"MutatingService.DestroyFederation: federation_name is required")
	}

	resp := &rtiv1.AdminDestroyFederationResponse{}

	if req.GetEvictJoinedFederates() {
		// Walk the roster, ResignFederation each joined federate. We
		// reuse the same primitive ForceResign uses so every eviction
		// emits a federate-resigned event-log entry + MOM hook.
		rosters := s.opts.Federations.Snapshot()
		for _, r := range rosters {
			if string(r.Name) != fed {
				continue
			}
			for _, info := range r.Federates {
				rerr := s.opts.Federations.ResignFederation(
					ctx,
					core.FederationName(fed),
					info.Handle,
					core.ResignActionUnconditionallyDivestAttributes,
				)
				if rerr != nil && !errors.Is(rerr, core.ErrFederateNotJoined) {
					return nil, errToStatus(rerr)
				}
				resp.EvictedHandles = append(resp.EvictedHandles, uint64(info.Handle))
			}
			break
		}
	}

	if err := s.opts.Federations.DestroyFederation(ctx, core.FederationName(fed)); err != nil {
		return nil, errToStatus(err)
	}
	return resp, nil
}
