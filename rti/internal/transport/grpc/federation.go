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
type federationService struct {
	rtiv1.UnimplementedFederationServiceServer
	fed core.FederationStore
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
	coreReq := core.CreateFederationRequest{
		Name:         core.FederationName(req.GetFederationName()),
		FOMModules:   convertFOMModules(req.GetFomModules()),
		Mode:         protoModeToCore(req.GetMode()),
		StallTimeout: time.Duration(req.GetStallTimeoutSeconds() * float64(time.Second)),
		Seed:         req.GetSeed(),
	}
	if err := s.fed.CreateFederation(ctx, coreReq); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.CreateFederationResponse{
		EffectiveSeed: req.GetSeed(),
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
	if err := s.fed.DestroyFederation(ctx, core.FederationName(req.GetFederationName())); err != nil {
		return nil, errToStatus(err)
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
	h, err := s.fed.JoinFederation(ctx, core.JoinFederationRequest{
		Federation:   core.FederationName(req.GetFederationName()),
		FederateName: req.GetFederateName(),
	})
	if err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.JoinFederationResponse{FederateHandle: uint64(h)}, nil
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
		return nil, errToStatus(err)
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
		return nil, errToStatus(err)
	}
	out := make([]*rtiv1.FederationSummary, 0, len(summaries))
	for _, sum := range summaries {
		out = append(out, &rtiv1.FederationSummary{
			Name:            string(sum.Name),
			Mode:            coreModeToProto(sum.Mode),
			FederatesJoined: sum.FederatesJoined,
		})
	}
	return &rtiv1.ListFederationsResponse{Federations: out}, nil
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

// protoResignActionToCore maps the proto ResignAction enum to the core
// equivalent. Cut 1 supports only UnconditionallyDivestAttributes; other
// values surface as ResignActionUnspecified and the manager rejects.
func protoResignActionToCore(a rtiv1.ResignAction) core.ResignAction {
	switch a {
	case rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES:
		return core.ResignActionUnconditionallyDivestAttributes
	default:
		return core.ResignActionUnspecified
	}
}

// errToStatus moved to errs.go (shared across federation/declaration/
// object/stream service handlers).
