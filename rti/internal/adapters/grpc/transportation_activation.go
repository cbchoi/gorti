package grpc

import (
	"context"
	"errors"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (s *objectServiceOf[T]) configureTransportation(opts Options) {
	epochs, ok := opts.Outbox.(interface {
		CallbackEpoch(core.FederationName, core.FederateHandle) (uint64, error)
	})
	if opts.Sessions == nil || !ok {
		return
	}
	s.transportationScope = func(ctx context.Context, fed core.FederationName, h core.FederateHandle) (core.TransportationScope, error) {
		// The unary interceptor retains checkpoint and membership admission for
		// this complete call, including epoch lookup and the domain commit.
		if err := opts.Sessions.Authenticate(ctx, fed, h); err != nil {
			return core.TransportationScope{}, err
		}
		id, err := opts.Sessions.CurrentCallerIdentity(ctx)
		if err != nil {
			return core.TransportationScope{}, err
		}
		if id.Federation != fed || id.Handle != h {
			return core.TransportationScope{}, status.Error(codes.PermissionDenied, "transportation caller does not match authenticated membership")
		}
		epoch, err := epochs.CallbackEpoch(fed, h)
		if err != nil {
			return core.TransportationScope{}, errToStatus(ctx, err)
		}
		return core.TransportationScope{Member: id, CallbackEpoch: epoch}, nil
	}
}

func (s *objectServiceOf[T]) transportationActivation(ctx context.Context, version rtiv1.WireVersion, fed string, h uint64) (core.TransportationActivation, core.TransportationScope, error) {
	if !validWireVersion(version) {
		return nil, core.TransportationScope{}, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	service, ok := s.obj.(core.TransportationActivation)
	if !ok || s.transportationScope == nil {
		return nil, core.TransportationScope{}, status.Error(codes.Unimplemented, "authenticated transportation activation is unavailable")
	}
	scope, err := s.transportationScope(ctx, core.FederationName(fed), core.FederateHandle(h))
	return service, scope, err
}

func (s *objectServiceOf[T]) PrepareAttributeTransportationTypeChange(ctx context.Context, req *rtiv1.ChangeAttributeTransportRequest) (*rtiv1.TransportationChange, error) {
	service, scope, err := s.transportationActivation(ctx, req.GetWireVersion(), req.GetFederationName(), req.GetFederateHandle())
	if err != nil {
		return nil, err
	}
	attrs := make([]core.AttributeHandle, len(req.GetAttributeHandles()))
	for i, attr := range req.GetAttributeHandles() {
		attrs[i] = core.AttributeHandle(attr)
	}
	change, err := service.PrepareAttributeTransportationChange(ctx, scope, core.ObjectHandle(req.GetObjectHandle()), attrs, protoTransportTypeToCore(req.GetTransportType()))
	if err != nil {
		return nil, transportationActivationError(ctx, err)
	}
	return encodeTransportationChange(change), nil
}

func (s *objectServiceOf[T]) PrepareInteractionTransportationTypeChange(ctx context.Context, req *rtiv1.ChangeInteractionTransportRequest) (*rtiv1.TransportationChange, error) {
	service, scope, err := s.transportationActivation(ctx, req.GetWireVersion(), req.GetFederationName(), req.GetFederateHandle())
	if err != nil {
		return nil, err
	}
	change, err := service.PrepareInteractionTransportationChange(ctx, scope, core.InteractionClassHandle(req.GetInteractionClassHandle()), protoTransportTypeToCore(req.GetTransportType()))
	if err != nil {
		return nil, transportationActivationError(ctx, err)
	}
	return encodeTransportationChange(change), nil
}

func (s *objectServiceOf[T]) ActivateTransportationTypeChange(ctx context.Context, req *rtiv1.ActivateTransportationTypeChangeRequest) (*rtiv1.TransportationChange, error) {
	service, scope, err := s.transportationActivation(ctx, req.GetWireVersion(), req.GetFederationName(), req.GetFederateHandle())
	if err != nil {
		return nil, err
	}
	if req.GetExpectedFederationGeneration() != scope.Member.Generation || req.GetExpectedCallbackEpoch() != scope.CallbackEpoch {
		return nil, transportationActivationError(ctx, core.ErrTransportationTicketInvalid)
	}
	change, err := service.ActivateTransportationChange(ctx, scope, req.GetTicket())
	if err != nil {
		return nil, transportationActivationError(ctx, err)
	}
	return encodeTransportationChange(change), nil
}

func encodeTransportationChange(change core.TransportationChange) *rtiv1.TransportationChange {
	attrs := make([]uint64, len(change.Attributes))
	for i, attr := range change.Attributes {
		attrs[i] = uint64(attr)
	}
	return &rtiv1.TransportationChange{Ticket: change.Ticket, FederationGeneration: change.Scope.Member.Generation, CallbackEpoch: change.Scope.CallbackEpoch, ObjectHandle: uint64(change.Object), AttributeHandles: attrs, InteractionClassHandle: uint64(change.Interaction), TransportType: coreTransportTypeToProto(change.Transportation)}
}

func transportationActivationError(ctx context.Context, err error) error {
	name, code := "", codes.FailedPrecondition
	switch {
	case errors.Is(err, core.ErrAttributeAlreadyBeingChanged):
		name = "AttributeAlreadyBeingChanged"
	case errors.Is(err, core.ErrInteractionClassAlreadyBeingChanged):
		name = "InteractionClassAlreadyBeingChanged"
	case errors.Is(err, core.ErrTransportationTicketInvalid):
		name = rtiInternalErrorName
	case errors.Is(err, core.ErrTransportationPendingLimit):
		name, code = rtiInternalErrorName, codes.ResourceExhausted
	default:
		return errToStatus(ctx, err)
	}
	_ = grpcgo.SetTrailer(ctx, metadata.Pairs(specExceptionTrailerKey, name))
	return status.Error(code, err.Error())
}
