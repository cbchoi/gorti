package server

import (
	"context"
	"fmt"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const momInteractionStreamMethod = "/rti.v1.InteractionStreamService/SendInteractions"

// Replaces ordinary stream membership admission only for the two transports
// whose per-message leases would otherwise surround MOM's lifecycle writer.
func newMOMServiceStreamAdmissionInterceptor(ordinary grpc.StreamServerInterceptor, deps momServiceAdmissionDependencies) (grpc.StreamServerInterceptor, error) {
	if ordinary == nil || deps.Federations == nil || deps.Authenticate == nil || deps.FOMs == nil || deps.Dispatcher == nil || deps.Declarations == nil {
		return nil, fmt.Errorf("MOM stream admission dependencies incomplete")
	}
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if info.FullMethod != momInteractionStreamMethod && info.FullMethod != rtiv1.ConfirmedObjectService_Exchange_FullMethodName {
			return ordinary(srv, stream, info, handler)
		}
		wrapped := &momServiceAdmissionStream{ServerStream: stream, deps: deps, confirmed: info.FullMethod == rtiv1.ConfirmedObjectService_Exchange_FullMethodName, ctx: stream.Context()}
		defer wrapped.releaseCurrent()
		return handler(srv, wrapped)
	}, nil
}

type momStreamScopedRequest interface {
	GetFederationName() string
	GetFederateHandle() uint64
}

type momServiceAdmissionStream struct {
	grpc.ServerStream
	deps                 momServiceAdmissionDependencies
	confirmed, handshake bool
	open                 *core.MemberIdentity
	ctx                  context.Context
	release              func()
}

func (s *momServiceAdmissionStream) Context() context.Context { return s.ctx }
func (s *momServiceAdmissionStream) releaseCurrent() {
	if s.release != nil {
		s.release()
		s.release = nil
	}
	s.ctx = s.ServerStream.Context()
}
func (s *momServiceAdmissionStream) SendMsg(message any) error {
	s.releaseCurrent()
	return s.ServerStream.SendMsg(message)
}
func (s *momServiceAdmissionStream) RecvMsg(message any) error {
	s.releaseCurrent()
	if err := s.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	scoped, interaction, opening, err := s.decodeRequest(message)
	if err != nil || scoped == nil {
		return err
	}
	fed, h := core.FederationName(scoped.GetFederationName()), core.FederateHandle(scoped.GetFederateHandle())
	if s.open != nil && (s.open.Federation != fed || s.open.Handle != h) {
		return status.Error(codes.PermissionDenied, "operation differs from confirmed stream identity")
	}
	if leaf, ok := momRequestServiceLeaf(s.ctx, s.deps, interaction); ok {
		ctx, release, err := acquireMOMRequestAdmission(s.ctx, s.deps, interaction, leaf)
		if err != nil {
			return err
		}
		s.ctx, s.release = ctx, release
	} else {
		if err := s.acquireOrdinary(fed, h, opening != nil); err != nil {
			return err
		}
	}
	return s.validateIdentity(fed, h, opening)
}

func (s *momServiceAdmissionStream) decodeRequest(message any) (momStreamScopedRequest, *rtiv1.SendInteractionRequest, *rtiv1.ConfirmedObjectOpen, error) {
	var scoped momStreamScopedRequest
	var interaction *rtiv1.SendInteractionRequest
	var opening *rtiv1.ConfirmedObjectOpen
	if s.confirmed {
		request, ok := message.(*rtiv1.ConfirmedObjectRequest)
		if !ok || request == nil {
			return nil, nil, nil, status.Error(codes.InvalidArgument, "invalid confirmed request")
		}
		switch {
		case request.GetOpen() != nil:
			opening = request.GetOpen()
			scoped = opening
		case request.GetInteraction() != nil:
			interaction = request.GetInteraction()
			scoped = interaction
		case request.GetAttributeUpdate() != nil:
			scoped = request.GetAttributeUpdate()
		default:
			return nil, nil, nil, status.Error(codes.InvalidArgument, "confirmed request has no operation")
		}
	} else {
		var ok bool
		interaction, ok = message.(*rtiv1.SendInteractionRequest)
		if !ok || interaction == nil {
			return nil, nil, nil, status.Error(codes.InvalidArgument, "invalid interaction request")
		}
		scoped = interaction
		if !s.handshake {
			s.handshake = true
			if proto.Equal(interaction, &rtiv1.SendInteractionRequest{}) {
				return nil, nil, nil, nil
			}
		}
	}
	return scoped, interaction, opening, nil
}

func (s *momServiceAdmissionStream) acquireOrdinary(fed core.FederationName, h core.FederateHandle, opening bool) error {
	releaseCheckpoint := func() {}
	var err error
	if s.deps.Checkpoint != nil {
		if opening {
			releaseCheckpoint = s.deps.Checkpoint.AcquireCheckpoint(fed)
		} else if participant, ok := s.deps.Checkpoint.(core.FederateServiceAdmission); ok {
			releaseCheckpoint, err = participant.AcquireFederateService(fed, h)
		} else {
			releaseCheckpoint, err = s.deps.Checkpoint.AcquireService(fed)
		}
	}
	if err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	releaseMember, err := s.deps.Federations.AcquireMember(fed, h)
	if err != nil {
		releaseCheckpoint()
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	s.release = func() { releaseMember(); releaseCheckpoint() }
	if err := s.deps.Authenticate(s.ctx, fed, h); err != nil {
		s.releaseCurrent()
		return err
	}
	return nil
}

func (s *momServiceAdmissionStream) validateIdentity(fed core.FederationName, h core.FederateHandle, opening *rtiv1.ConfirmedObjectOpen) error {
	id, err := s.deps.Federations.MemberIdentity(fed, h)
	if err != nil {
		s.releaseCurrent()
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	if s.open != nil && (s.open.Generation != id.Generation || s.open.Incarnation != id.Incarnation) {
		s.releaseCurrent()
		return status.Error(codes.FailedPrecondition, "confirmed stream membership changed")
	}
	if opening != nil {
		if s.open != nil || opening.WireVersion != 1 || opening.ExpectedFederationGeneration != id.Generation {
			s.releaseCurrent()
			return status.Error(codes.FailedPrecondition, "invalid confirmed open generation")
		}
		s.open = &id
		s.ctx = grpcadapter.ConfirmedStreamMembershipAdmitted(s.ctx)
	}
	return nil
}
