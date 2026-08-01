package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type confirmedObjectService struct {
	rtiv1.UnimplementedConfirmedObjectServiceServer
	server *Server
}

func newConfirmedObjectService(server *Server) *confirmedObjectService {
	return &confirmedObjectService{server: server}
}

func (s *confirmedObjectService) Exchange(
	stream rtiv1.ConfirmedObjectService_ExchangeServer,
) error {
	// Let the client establish capability before sending a mutating request.
	// An older server returns Unimplemented at this boundary, so unary fallback
	// cannot duplicate an operation.
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	open, err := stream.Recv()
	if err != nil {
		return err
	}
	openRequest := open.GetOpen()
	if open.GetSequence() != 0 || openRequest == nil {
		return status.Error(codes.FailedPrecondition,
			"confirmed object stream must start with sequence-zero open")
	}
	if !validWireVersion(openRequest.GetWireVersion()) {
		return status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if err := s.validateOpen(stream.Context(), openRequest); err != nil {
		return err
	}
	if err := stream.Send(&rtiv1.ConfirmedObjectResult{}); err != nil {
		return err
	}

	var committed uint64
	var previousACKSendNS uint64
	for {
		request, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		sequence := request.GetSequence()
		if sequence != committed+1 {
			return status.Errorf(codes.FailedPrecondition,
				"confirmed object sequence %d, expected %d", sequence, committed+1)
		}

		var operationErr error
		var serviceStarted time.Time
		if openRequest.GetTransportDiagnostics() {
			serviceStarted = time.Now()
		}
		switch operation := request.GetOperation().(type) {
		case *rtiv1.ConfirmedObjectRequest_Open:
			return status.Error(codes.FailedPrecondition, "confirmed object stream is already open")
		case *rtiv1.ConfirmedObjectRequest_AttributeUpdate:
			if operation.AttributeUpdate == nil {
				return status.Error(codes.InvalidArgument, "confirmed attribute update is nil")
			}
			if err := validateConfirmedObjectIdentity(openRequest, operation.AttributeUpdate); err != nil {
				return err
			}
			_, operationErr = s.server.objService.UpdateAttributeValues(stream.Context(), operation.AttributeUpdate)
		case *rtiv1.ConfirmedObjectRequest_Interaction:
			if operation.Interaction == nil {
				return status.Error(codes.InvalidArgument, "confirmed interaction is nil")
			}
			if err := validateConfirmedObjectIdentity(openRequest, operation.Interaction); err != nil {
				return err
			}
			_, operationErr = s.server.objService.SendInteraction(stream.Context(), operation.Interaction)
		default:
			return status.Error(codes.InvalidArgument, "confirmed object request has no operation")
		}

		result := &rtiv1.ConfirmedObjectResult{
			Sequence:          sequence,
			PreviousAckSendNs: previousACKSendNS,
		}
		if !serviceStarted.IsZero() {
			result.ServerServiceNs = uint64(time.Since(serviceStarted))
		}
		if operationErr != nil {
			encoded, marshalErr := proto.Marshal(status.Convert(operationErr).Proto())
			if marshalErr != nil {
				return status.Error(codes.Internal,
					fmt.Sprintf("encode confirmed object result: %v", marshalErr))
			}
			result.ErrorStatus = encoded
		}
		var ackSendStarted time.Time
		if openRequest.GetTransportDiagnostics() {
			ackSendStarted = time.Now()
		}
		if err := stream.Send(result); err != nil {
			return err
		}
		if !ackSendStarted.IsZero() {
			previousACKSendNS = uint64(time.Since(ackSendStarted))
		}
		committed = sequence
	}
}

func (s *confirmedObjectService) validateOpen(
	ctx context.Context,
	open *rtiv1.ConfirmedObjectOpen,
) error {
	if s.server.membership == nil {
		return nil
	}
	federation := core.FederationName(open.GetFederationName())
	generation, ok := s.server.membership.GenerationFor(federation)
	if !ok {
		return errToStatus(ctx, core.ErrFederationNotFound)
	}
	if generation != open.GetExpectedFederationGeneration() {
		return errToStatus(ctx, core.ErrFederationGenerationMismatch)
	}
	if validated, _ := ctx.Value(confirmedObjectMembershipValidatedKey{}).(bool); validated {
		return nil
	}
	return errToStatus(ctx, s.server.membership.ValidateMember(
		federation, core.FederateHandle(open.GetFederateHandle())))
}

func validateConfirmedObjectIdentity(
	open *rtiv1.ConfirmedObjectOpen,
	operation federateScopedRequest,
) error {
	if operation.GetFederationName() != open.GetFederationName() ||
		operation.GetFederateHandle() != open.GetFederateHandle() {
		return status.Error(codes.PermissionDenied,
			"confirmed object operation identity differs from stream identity")
	}
	return nil
}
