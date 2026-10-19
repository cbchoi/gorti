package grpc

import (
	"context"
	"errors"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type OwnershipCallbackReceiptOptions struct {
	Coordinator  core.OwnershipCallbackCoordinator
	Authenticate func(context.Context, core.FederationName, core.FederateHandle) error
	AcquireScope func(context.Context, core.OwnershipCallbackScope) (func(), error)
	// RecoveryScope resolves current authority only after coordinated restore.
	RecoveryScope func(context.Context, core.FederationName, core.FederateHandle) (core.OwnershipCallbackScope, error)
}

// Mandatory invocation receipts are independent of optional MOM measurements.
type OwnershipCallbackReceipts struct {
	opts OwnershipCallbackReceiptOptions
}

func NewOwnershipCallbackReceipts(opts OwnershipCallbackReceiptOptions) (*OwnershipCallbackReceipts, error) {
	if opts.Coordinator == nil || opts.Authenticate == nil || opts.AcquireScope == nil {
		return nil, fmt.Errorf("ownership receipts require coordinator, authentication and live scope admission")
	}
	return &OwnershipCallbackReceipts{opts: opts}, nil
}

func (r *OwnershipCallbackReceipts) acquire(ctx context.Context, scope core.OwnershipCallbackScope) (func(), error) {
	if scope.Federation == "" || scope.Recipient == 0 || scope.FederationGeneration == 0 {
		return nil, core.ErrOwnershipCallbackScope
	}
	if err := r.opts.Authenticate(ctx, scope.Federation, scope.Recipient); err != nil {
		return nil, err
	}
	release, err := r.opts.AcquireScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, core.ErrOwnershipCallbackScope
	}
	return release, nil
}

func (r *OwnershipCallbackReceipts) Prepare(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event *rtiv1.FederateEvent) (func(bool), error) {
	recovery := event.GetCallbackRecovery()
	if err := validateOwnershipRecoveryEvent(event); err != nil {
		return nil, err
	}
	identity := event.GetOwnershipReceipt()
	if identity == nil {
		return nil, nil
	}
	// Only terminal outcomes may carry mandatory invocation identities.
	if event.GetOwnershipAcquired() == nil && event.GetOwnershipUnavailable() == nil && event.GetOwnershipAcquisitionCancelled() == nil {
		return nil, core.ErrOwnershipCallbackReceipt
	}
	scope, err := r.prepareOwnershipScope(ctx, fed, recipient, event)
	if err != nil {
		return nil, err
	}
	release, err := r.acquire(ctx, scope)
	if err != nil {
		return nil, err
	}
	if err := r.validateRecoveredOwnership(fed, recipient, event); err != nil {
		release()
		return nil, err
	}
	receipt, finish, err := r.opts.Coordinator.BeginOwnershipCallbackDelivery(ctx, scope, identity.GetCallbackId())
	release()
	return finishOwnershipPreparation(event, recovery, receipt, finish, err)
}

type ownershipCallbackService struct {
	rtiv1.UnimplementedOwnershipCallbackServiceServer
	receipts *OwnershipCallbackReceipts
}

func NewOwnershipCallbackService(receipts *OwnershipCallbackReceipts) rtiv1.OwnershipCallbackServiceServer {
	return &ownershipCallbackService{receipts: receipts}
}

func ownershipReceiptStatus(ctx context.Context, err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, core.ErrOwnershipCallbackScope), errors.Is(err, core.ErrOwnershipCallbackReceipt), errors.Is(err, core.ErrOwnershipAcquisitionPending):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, core.ErrOwnershipCallbackCapacity):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, core.ErrOwnershipCallbackBusy):
		return status.Error(codes.Aborted, err.Error())
	default:
		return errToStatus(ctx, err)
	}
}

func scopeFromRequest(req *rtiv1.OwnershipCallbackScopeRequest) core.OwnershipCallbackScope {
	return core.OwnershipCallbackScope{Federation: core.FederationName(req.GetFederationName()), Recipient: core.FederateHandle(req.GetFederateHandle()), FederationGeneration: req.GetFederationGeneration(), CallbackEpoch: req.GetCallbackEpoch()}
}

func (s *ownershipCallbackService) EnableOwnershipCallbackReceipts(ctx context.Context, req *rtiv1.OwnershipCallbackScopeRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("EnableOwnershipCallbackReceipts")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.receipts == nil {
		return nil, status.Error(codes.Unimplemented, "ownership invocation receipts are not configured")
	}
	scope := scopeFromRequest(req)
	release, err := s.receipts.acquire(ctx, scope)
	if err != nil {
		return nil, ownershipReceiptStatus(ctx, err)
	}
	defer release()
	if err := s.receipts.opts.Coordinator.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
		return nil, ownershipReceiptStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *ownershipCallbackService) ReportOwnershipCallbackInvocation(ctx context.Context, req *rtiv1.ReportOwnershipCallbackInvocationRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("ReportOwnershipCallbackInvocation")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.receipts == nil {
		return nil, status.Error(codes.Unimplemented, "ownership invocation receipts are not configured")
	}
	wire := req.GetReceipt()
	if wire == nil {
		return nil, status.Error(codes.InvalidArgument, "ownership receipt required")
	}
	scope := core.OwnershipCallbackScope{Federation: core.FederationName(req.GetFederationName()), Recipient: core.FederateHandle(req.GetFederateHandle()), FederationGeneration: wire.GetFederationGeneration(), CallbackEpoch: wire.GetCallbackEpoch()}
	release, err := s.receipts.acquire(ctx, scope)
	if err != nil {
		return nil, ownershipReceiptStatus(ctx, err)
	}
	defer release()
	receipt := core.OwnershipCallbackReceipt{FederationGeneration: wire.GetFederationGeneration(), CallbackEpoch: wire.GetCallbackEpoch(), CallbackID: wire.GetCallbackId(), DeliveryAttempt: wire.GetDeliveryAttempt()}
	if err := s.receipts.opts.Coordinator.ReportOwnershipCallbackInvocation(ctx, scope, receipt); err != nil {
		return nil, ownershipReceiptStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *ownershipCallbackService) RetryOwnershipCallbacks(ctx context.Context, req *rtiv1.OwnershipCallbackScopeRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("RetryOwnershipCallbacks")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.receipts == nil {
		return nil, status.Error(codes.Unimplemented, "ownership invocation receipts are not configured")
	}
	scope := scopeFromRequest(req)
	release, err := s.receipts.acquire(ctx, scope)
	if err != nil {
		return nil, ownershipReceiptStatus(ctx, err)
	}
	// Queue admission/projection must not retain checkpoint or membership locks.
	release()
	if err := s.receipts.opts.Coordinator.RetryOwnershipCallbacks(ctx, scope); err != nil {
		return nil, ownershipReceiptStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}
func validateOwnershipRecoveryEvent(event *rtiv1.FederateEvent) error {
	recovery := event.GetCallbackRecovery()
	if recovery != nil {
		if len(recovery.GetInvocationIdentity()) != 32 {
			return core.ErrOwnershipCallbackReceipt
		}
		if recovery.GetCompleted() {
			if !recovery.GetOutcomeOnly() || event.Event != nil || len(event.GetCallbackReceipt()) != 0 || event.GetOwnershipReceipt() != nil {
				return core.ErrOwnershipCallbackReceipt
			}
			return nil
		}
		if event.Event == nil || len(event.GetCallbackReceipt()) != 32 {
			return core.ErrOwnershipCallbackReceipt
		}
	}
	return nil
}

func (r *OwnershipCallbackReceipts) prepareOwnershipScope(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event *rtiv1.FederateEvent) (core.OwnershipCallbackScope, error) {
	identity, recovery := event.GetOwnershipReceipt(), event.GetCallbackRecovery()
	scope := core.OwnershipCallbackScope{Federation: fed, Recipient: recipient, FederationGeneration: identity.GetFederationGeneration(), CallbackEpoch: identity.GetCallbackEpoch()}
	if recovery != nil {
		if r.opts.RecoveryScope == nil {
			return core.OwnershipCallbackScope{}, core.ErrOwnershipCallbackScope
		}
		var err error
		scope, err = r.opts.RecoveryScope(ctx, fed, recipient)
		if err != nil {
			return core.OwnershipCallbackScope{}, err
		}
		if scope.Federation != fed || scope.Recipient != recipient {
			return core.OwnershipCallbackScope{}, core.ErrOwnershipCallbackScope
		}
	}
	return scope, nil
}
func (r *OwnershipCallbackReceipts) validateRecoveredOwnership(fed core.FederationName, recipient core.FederateHandle, event *rtiv1.FederateEvent) error {
	recovery, identity := event.GetCallbackRecovery(), event.GetOwnershipReceipt()
	if recovery != nil {
		stateSource, ok := r.opts.Coordinator.(ownershipCheckpointStateSource)
		if !ok {
			return core.ErrOwnershipCallbackReceipt
		}
		if _, err := validateOwnershipCheckpointBinding(stateSource.ExportState(fed), recipient, event, &OwnershipCheckpointBinding{CallbackID: identity.GetCallbackId()}, recovery.GetOutcomeOnly()); err != nil {
			return err
		}
	}
	return nil
}
func finishOwnershipPreparation(event *rtiv1.FederateEvent, recovery *rtiv1.CallbackRecovery, receipt core.OwnershipCallbackReceipt, finish func(bool), err error) (func(bool), error) {
	if errors.Is(err, core.ErrOwnershipCallbackComplete) && recovery != nil && recovery.GetOutcomeOnly() {
		// An ownership acknowledgement may have succeeded while the general
		// outcome report failed. Do not drop that independent outcome carrier.
		event.OwnershipReceipt = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if finish == nil || receipt.CallbackID == 0 || receipt.DeliveryAttempt == 0 {
		return nil, core.ErrOwnershipCallbackReceipt
	}
	event.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{FederationGeneration: receipt.FederationGeneration, CallbackEpoch: receipt.CallbackEpoch, CallbackId: receipt.CallbackID, DeliveryAttempt: receipt.DeliveryAttempt}
	return finish, nil
}
