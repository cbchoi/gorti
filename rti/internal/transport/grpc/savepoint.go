// SavepointService gRPC handler — translates rti.v1.SavepointService
// RPCs into calls on rti/internal/savepoint.Manager.
//
// Owner: Agent A — M12 W1 (cut-3 gRPC exposure of cut-2 savepoint.Manager).
//
// Composition: server.go wires a *savepointService into the composed
// Server via newSavepointService(*savepoint.Manager).

package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/savepoint"
)

// savepointService is the concrete SavepointServiceServer impl.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.5): the handler binds to core.SavepointCoordinator instead of the
// concrete *savepoint.Manager so alternative implementations can be
// wired in at the composition root.
type savepointService struct {
	rtiv1.UnimplementedSavepointServiceServer
	mgr core.SavepointCoordinator
}

func newSavepointService(mgr core.SavepointCoordinator) *savepointService {
	return &savepointService{mgr: mgr}
}

// savepointErrToStatus maps savepoint-package storage sentinels onto
// gRPC codes. Storage-layer errors (ErrSaveBundleExists /
// ErrSaveBundleNotFound) are not in core.* — they live in the savepoint
// package — so the shared errToStatus does not handle them; this helper
// fills the gap.
func savepointErrToStatus(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, savepoint.ErrSaveBundleNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, savepoint.ErrSaveBundleExists):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return errToStatus(err)
	}
}

// RequestFederationSave implements §4.8.
func (s *savepointService) RequestFederationSave(
	ctx context.Context,
	req *rtiv1.RequestFederationSaveRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("RequestFederationSave")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	var saveTime *core.LogicalTime
	if req.SaveTime != nil {
		t := core.LogicalTime(req.GetSaveTime())
		saveTime = &t
	}
	if err := s.mgr.RequestFederationSave(
		ctx,
		core.FederationName(req.GetFederationName()),
		req.GetLabel(),
		saveTime,
	); err != nil {
		return nil, savepointErrToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// FederateSaveComplete implements §4.10 (per-federate success).
func (s *savepointService) FederateSaveComplete(
	ctx context.Context,
	req *rtiv1.FederateSaveResponseRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("FederateSaveComplete")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.FederateSaveComplete(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, savepointErrToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// FederateSaveNotComplete implements §4.10 (per-federate failure).
func (s *savepointService) FederateSaveNotComplete(
	ctx context.Context,
	req *rtiv1.FederateSaveResponseRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("FederateSaveNotComplete")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.FederateSaveNotComplete(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, savepointErrToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// QuerySaveState implements §4.11.
func (s *savepointService) QuerySaveState(
	_ context.Context,
	req *rtiv1.QuerySaveStateRequest,
) (*rtiv1.QuerySaveStateResponse, error) {
	if req == nil {
		return nil, nilRequest("QuerySaveState")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	st := s.mgr.QuerySaveState(
		core.FederationName(req.GetFederationName()),
		req.GetLabel(),
	)
	return &rtiv1.QuerySaveStateResponse{State: saveStateToProto(st)}, nil
}

// RequestFederationRestore implements §4.12.
func (s *savepointService) RequestFederationRestore(
	ctx context.Context,
	req *rtiv1.RequestFederationRestoreRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("RequestFederationRestore")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.RequestFederationRestore(
		ctx,
		core.FederationName(req.GetFederationName()),
		req.GetLabel(),
	); err != nil {
		return nil, savepointErrToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// FederateRestoreComplete implements §4.14 (per-federate success).
func (s *savepointService) FederateRestoreComplete(
	ctx context.Context,
	req *rtiv1.FederateRestoreResponseRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("FederateRestoreComplete")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.FederateRestoreComplete(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
	); err != nil {
		return nil, savepointErrToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// QueryRestoreState implements §4.15.
func (s *savepointService) QueryRestoreState(
	_ context.Context,
	req *rtiv1.QueryRestoreStateRequest,
) (*rtiv1.QueryRestoreStateResponse, error) {
	if req == nil {
		return nil, nilRequest("QueryRestoreState")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	st := s.mgr.QueryRestoreState(
		core.FederationName(req.GetFederationName()),
		req.GetLabel(),
	)
	return &rtiv1.QueryRestoreStateResponse{State: restoreStateToProto(st)}, nil
}

// saveStateToProto maps savepoint.SaveState into the proto enum.
func saveStateToProto(s savepoint.SaveState) rtiv1.SaveState {
	switch s {
	case savepoint.StateIdle:
		return rtiv1.SaveState_SAVE_STATE_IDLE
	case savepoint.StateInitiated:
		return rtiv1.SaveState_SAVE_STATE_INITIATED
	case savepoint.StateSaved:
		return rtiv1.SaveState_SAVE_STATE_SAVED
	case savepoint.StateNotSaved:
		return rtiv1.SaveState_SAVE_STATE_NOT_SAVED
	default:
		return rtiv1.SaveState_SAVE_STATE_UNSPECIFIED
	}
}

// restoreStateToProto maps savepoint.RestoreState into the proto enum.
func restoreStateToProto(s savepoint.RestoreState) rtiv1.RestoreState {
	switch s {
	case savepoint.RestoreIdle:
		return rtiv1.RestoreState_RESTORE_STATE_IDLE
	case savepoint.RestoreLoading:
		return rtiv1.RestoreState_RESTORE_STATE_LOADING
	case savepoint.RestoreInitiated:
		return rtiv1.RestoreState_RESTORE_STATE_INITIATED
	case savepoint.RestoreCompleted:
		return rtiv1.RestoreState_RESTORE_STATE_COMPLETED
	case savepoint.RestoreFailed:
		return rtiv1.RestoreState_RESTORE_STATE_FAILED
	default:
		return rtiv1.RestoreState_RESTORE_STATE_UNSPECIFIED
	}
}
