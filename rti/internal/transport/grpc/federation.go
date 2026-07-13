package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// federationService translates rti.v1.FederationService RPCs into
// core.FederationStore calls. Errors map per proto/rti/v1/errors.proto
// to gRPC status codes via errToStatus; see the table in errToStatus
// itself for the canonical mapping.
//
// onCreateFederationSuccess is an optional post-success hook invoked
// after CreateFederation returns nil; it lets the composition root
// (rtid main) populate per-federation lookup tables that the manager
// itself does not expose (notably the FOM repository's per-federation
// handle map consumed by FOMRepoOrderLookup — without this hook,
// best-effort interaction order resolution falls back to TSO because
// Repo.Get(fed) returns ErrFederationNotFound). Nil hook is a no-op,
// preserving the prior contract for tests that construct the service
// without a hook.
//
// ddsEnabled + ddsDefaultDomainID + transportLookup are M19 Phase 1a
// (docs/m19-dds-adapter.md §4.4): when a federate requests
// CreateFederation with TRANSPORT_MODE_DDS, the handler refuses unless
// ddsEnabled is true (set when rtid was started with --enable-dds=true,
// implicitly false in the default CGo-free build). transportLookup
// reads back the federation's recorded transport so JoinFederation can
// echo it. nil transportLookup gracefully degrades to GRPC (used by
// older test fixtures that pre-date the M19 wiring).
type federationService struct {
	rtiv1.UnimplementedFederationServiceServer
	fed                        core.FederationStore
	onCreateFederationSuccess  func(ctx context.Context, name core.FederationName, modules []core.FOMModule)
	onDestroyFederationSuccess func(ctx context.Context, name core.FederationName)
	ddsEnabled                 bool
	ddsDefaultDomainID         int32
	transportLookup            func(core.FederationName) (core.TransportMode, int32, bool)
	membership                 core.FederationMembershipValidator
}

type federationSetupCreator interface {
	CreateFederationWithSetup(
		context.Context,
		core.CreateFederationRequest,
		func(generation uint64),
	) error
}

func newFederationService(fed core.FederationStore) *federationService {
	return &federationService{fed: fed}
}

// CreateFederation implements rti.v1.FederationService.CreateFederation.
func (s *federationService) CreateFederation(ctx context.Context, req *rtiv1.CreateFederationRequest) (*rtiv1.CreateFederationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}

	// M19 Phase 1a (docs/m19-dds-adapter.md §4.4): translate the wire
	// transport_mode into the core enum and reject DDS when the rtid
	// build/runtime cannot serve it. UNSPECIFIED collapses to GRPC.
	tm := protoTransportToCore(req.GetTransportMode())
	if tm == core.TransportModeDDS && !s.ddsEnabled {
		return nil, status.Error(codes.FailedPrecondition,
			"transport_mode=DDS requires rtid to be built with -tags=dds "+
				"and started with --enable-dds=true; this rtid was built without DDS support")
	}
	domainID := int32(0)
	if tm == core.TransportModeDDS {
		domainID = s.ddsDefaultDomainID
	}

	coreReq := core.CreateFederationRequest{
		Name:          core.FederationName(req.GetFederationName()),
		FOMModules:    convertFOMModules(req.GetFomModules()),
		Mode:          protoModeToCore(req.GetMode()),
		StallTimeout:  time.Duration(req.GetStallTimeoutSeconds() * float64(time.Second)),
		Seed:          req.GetSeed(),
		TransportMode: tm,
		DDSDomainID:   domainID,
	}
	var generation uint64
	setup := func(current uint64) {
		generation = current
		if s.onCreateFederationSuccess != nil {
			s.onCreateFederationSuccess(ctx, coreReq.Name, coreReq.FOMModules)
		}
	}
	if creator, ok := s.fed.(federationSetupCreator); ok {
		if err := creator.CreateFederationWithSetup(ctx, coreReq, setup); err != nil {
			return nil, errToStatus(ctx, err)
		}
	} else {
		if err := s.fed.CreateFederation(ctx, coreReq); err != nil {
			return nil, errToStatus(ctx, err)
		}
		if s.membership != nil {
			generation, _ = s.membership.GenerationFor(coreReq.Name)
			if guard, ok := s.membership.(core.FederationGenerationGuard); ok {
				release, leaseErr := guard.AcquireGeneration(coreReq.Name, generation)
				if leaseErr != nil {
					return nil, errToStatus(ctx, leaseErr)
				}
				defer release()
			}
		}
		setup(generation)
	}
	return &rtiv1.CreateFederationResponse{
		EffectiveSeed:        req.GetSeed(),
		FederationGeneration: generation,
	}, nil
}

// DestroyFederation implements rti.v1.FederationService.DestroyFederation.
func (s *federationService) DestroyFederation(ctx context.Context, req *rtiv1.DestroyFederationRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	name := core.FederationName(req.GetFederationName())
	expected, err := s.expectedGeneration(name, req.ExpectedFederationGeneration)
	if err != nil {
		return nil, err
	}
	if guard, ok := s.fed.(core.FederationGenerationGuard); ok && expected != nil {
		err = guard.DestroyFederationGeneration(ctx, name, *expected)
	} else {
		err = s.fed.DestroyFederation(ctx, name)
	}
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if s.onDestroyFederationSuccess != nil {
		s.onDestroyFederationSuccess(ctx, name)
	}
	return &rtiv1.Empty{}, nil
}

// JoinFederation implements rti.v1.FederationService.JoinFederation.
func (s *federationService) JoinFederation(ctx context.Context, req *rtiv1.JoinFederationRequest) (*rtiv1.JoinFederationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	fedName := core.FederationName(req.GetFederationName())
	expected, err := s.expectedGeneration(fedName, req.ExpectedFederationGeneration)
	if err != nil {
		return nil, err
	}
	h, err := s.fed.JoinFederation(ctx, core.JoinFederationRequest{
		Federation:         fedName,
		FederateName:       req.GetFederateName(),
		ExpectedGeneration: expected,
		// M13 thread B (docs/srs.md §10.4): forward the optional
		// federate type from the wire. Old clients that omit the
		// field arrive with the empty string, which the federation
		// manager + MOM hook treat as "no type declared".
		FederateType: req.GetFederateType(),
	})
	if err != nil {
		return nil, errToStatus(ctx, err)
	}

	// M19 Phase 1a: echo the federation's recorded transport mode
	// (and DDS domain ID when DDS) so the federate's SDK picks the
	// right wire path. nil transportLookup keeps cut-2 callers
	// working — they get UNSPECIFIED + 0 which the SDK treats as GRPC.
	resp := &rtiv1.JoinFederationResponse{FederateHandle: uint64(h)}
	if s.transportLookup != nil {
		if tm, dom, ok := s.transportLookup(fedName); ok {
			resp.TransportMode = coreTransportToProto(tm)
			resp.DdsDomainId = dom
		}
	}
	return resp, nil
}

func (s *federationService) expectedGeneration(
	fed core.FederationName,
	provided *uint64,
) (*uint64, error) {
	if s.membership == nil {
		return provided, nil
	}
	current, ok := s.membership.GenerationFor(fed)
	if !ok {
		return nil, errToStatus(context.Background(), core.ErrFederationNotFound)
	}
	if provided == nil {
		// Generation zero is retained only for embedded legacy fixtures. Every
		// production server starts from a nonzero, restart-fenced generation.
		if current == 0 {
			value := uint64(0)
			return &value, nil
		}
		return nil, status.Error(codes.FailedPrecondition,
			"expected_federation_generation is required")
	}
	return provided, nil
}

// ResignFederation implements rti.v1.FederationService.ResignFederation.
func (s *federationService) ResignFederation(ctx context.Context, req *rtiv1.ResignFederationRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.fed.ResignFederation(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		protoResignActionToCore(req.GetAction()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// ListFederations implements rti.v1.FederationService.ListFederations.
func (s *federationService) ListFederations(ctx context.Context, req *rtiv1.ListFederationsRequest) (*rtiv1.ListFederationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	summaries, err := s.fed.List(ctx)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	out := make([]*rtiv1.FederationSummary, 0, len(summaries))
	for _, sum := range summaries {
		out = append(out, &rtiv1.FederationSummary{
			Name:                 string(sum.Name),
			Mode:                 coreModeToProto(sum.Mode),
			FederatesJoined:      sum.FederatesJoined,
			FederationGeneration: sum.Generation,
		})
	}
	return &rtiv1.ListFederationsResponse{Federations: out}, nil
}

// ListFederationMembers implements rti.v1.FederationService.ListFederationMembers.
// M24 W3 — IEEE 1516.1-2010 §4.8.
func (s *federationService) ListFederationMembers(_ context.Context, req *rtiv1.ListFederationMembersRequest) (*rtiv1.ListFederationMembersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	members := s.fed.ListMembers(core.FederationName(req.GetFederationName()))
	out := make([]*rtiv1.FederationMember, 0, len(members))
	for _, m := range members {
		out = append(out, &rtiv1.FederationMember{
			FederateHandle: uint64(m.Handle),
			FederateName:   m.Name,
			FederateType:   m.FederateType,
		})
	}
	return &rtiv1.ListFederationMembersResponse{Members: out}, nil
}

// requireWireVersion enforces that the client speaks a supported wire
// version. UNSPECIFIED or anything other than V1 is rejected as
// FailedPrecondition (matching ErrWireVersionMismatch -> ERR_WIRE_VERSION_MISMATCH).
func requireWireVersion(v rtiv1.WireVersion) error {
	if v == rtiv1.WireVersion_WIRE_VERSION_V1 {
		return nil
	}
	return status.Errorf(codes.FailedPrecondition,
		"wire version %s not supported; require WIRE_VERSION_V1", v.String())
}

// convertFOMModules maps proto FOM modules to core FOM modules.
func convertFOMModules(in []*rtiv1.FOMModule) []core.FOMModule {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.FOMModule, 0, len(in))
	for _, m := range in {
		if m == nil {
			continue
		}
		out = append(out, core.FOMModule{Path: m.GetPath(), XML: m.GetXml()})
	}
	return out
}

// protoModeToCore maps the proto Mode enum to the core.Mode enum.
// Unrecognized values fall through to ModeUnspecified.
func protoModeToCore(m rtiv1.Mode) core.Mode {
	switch m {
	case rtiv1.Mode_MODE_VERBOSE:
		return core.ModeVerbose
	case rtiv1.Mode_MODE_BEST_EFFORT:
		return core.ModeBestEffort
	default:
		return core.ModeUnspecified
	}
}

// coreModeToProto is the inverse of protoModeToCore for ListFederations.
func coreModeToProto(m core.Mode) rtiv1.Mode {
	switch m {
	case core.ModeVerbose:
		return rtiv1.Mode_MODE_VERBOSE
	case core.ModeBestEffort:
		return rtiv1.Mode_MODE_BEST_EFFORT
	default:
		return rtiv1.Mode_MODE_UNSPECIFIED
	}
}

// protoTransportToCore maps the proto TransportMode enum to the core
// equivalent. UNSPECIFIED collapses to GRPC for the append-only
// backward-compat contract (docs/m19-dds-adapter.md §4.1).
func protoTransportToCore(t rtiv1.TransportMode) core.TransportMode {
	switch t {
	case rtiv1.TransportMode_TRANSPORT_MODE_GRPC:
		return core.TransportModeGRPC
	case rtiv1.TransportMode_TRANSPORT_MODE_DDS:
		return core.TransportModeDDS
	default:
		return core.TransportModeGRPC
	}
}

// coreTransportToProto is the inverse of protoTransportToCore for
// JoinFederationResponse. The "UNSPECIFIED → GRPC" collapse is
// applied here too so the wire response always carries a definite
// mode for new clients.
func coreTransportToProto(t core.TransportMode) rtiv1.TransportMode {
	switch t {
	case core.TransportModeDDS:
		return rtiv1.TransportMode_TRANSPORT_MODE_DDS
	default:
		return rtiv1.TransportMode_TRANSPORT_MODE_GRPC
	}
}

// protoResignActionToCore maps the proto ResignAction enum to the core
// equivalent. M24 W2: all 6 spec values accepted (was 1). UNSPECIFIED
// at the wire surfaces as core.ResignActionUnspecified — the manager
// rejects with InvalidArgument + ErrResignActionUnsupported.
func protoResignActionToCore(a rtiv1.ResignAction) core.ResignAction {
	switch a {
	case rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES:
		return core.ResignActionUnconditionallyDivestAttributes
	case rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST:
		return core.ResignActionDeleteThenDivest
	case rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE:
		return core.ResignActionCancelThenDelete
	case rtiv1.ResignAction_RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP:
		return core.ResignActionCancelPendingOwnership
	case rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION:
		return core.ResignActionNoAction
	case rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS:
		return core.ResignActionDeleteObjects
	default:
		return core.ResignActionUnspecified
	}
}

// errToStatus moved to errs.go (shared across federation/declaration/
// object/stream service handlers).
