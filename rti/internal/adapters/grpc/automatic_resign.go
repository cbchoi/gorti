package grpc

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type automaticResignDirectives interface {
	SetAutomaticResignDirective(core.FederationName, core.FederateHandle, core.ResignAction) error
	GetAutomaticResignDirective(core.FederationName, core.FederateHandle) (core.ResignAction, error)
}

func sessionErrorStatus(ctx context.Context, err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	return errToStatus(ctx, err)
}

func (s *federationService) SetAutomaticResignDirective(ctx context.Context, req *rtiv1.SetAutomaticResignDirectiveRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session authentication is required")
	}
	fed, h := core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle())
	if err := s.sessions.Authenticate(ctx, fed, h); err != nil {
		return nil, err
	}
	target, ok := s.fed.(automaticResignDirectives)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "automatic resign directives unavailable")
	}
	if req.GetAction() == rtiv1.ResignAction_RESIGN_ACTION_UNSPECIFIED {
		return nil, errToStatus(ctx, core.ErrResignActionUnsupported)
	}
	action := protoResignActionToCore(req.GetAction())
	if err := target.SetAutomaticResignDirective(fed, h, action); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *federationService) GetAutomaticResignDirective(ctx context.Context, req *rtiv1.GetAutomaticResignDirectiveRequest) (*rtiv1.GetAutomaticResignDirectiveResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := requireWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session authentication is required")
	}
	fed, h := core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle())
	if err := s.sessions.Authenticate(ctx, fed, h); err != nil {
		return nil, err
	}
	target, ok := s.fed.(automaticResignDirectives)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "automatic resign directives unavailable")
	}
	action, err := target.GetAutomaticResignDirective(fed, h)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.GetAutomaticResignDirectiveResponse{Action: coreResignActionToProto(action)}, nil
}

func coreResignActionToProto(a core.ResignAction) rtiv1.ResignAction {
	switch a {
	case core.ResignActionUnconditionallyDivestAttributes:
		return rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES
	case core.ResignActionDeleteObjects:
		return rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS
	case core.ResignActionCancelPendingOwnership:
		return rtiv1.ResignAction_RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP
	case core.ResignActionDeleteThenDivest:
		return rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST
	case core.ResignActionCancelThenDelete:
		return rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE
	default:
		return rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION
	}
}
