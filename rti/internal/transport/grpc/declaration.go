// DeclarationService gRPC handler — translates the eight declaration
// pub/sub RPCs from the proto layer into calls on
// rti/internal/declaration.Manager.
//
// Owner: Agent A — M2 Wave 3B (TASK-035).
//
// Composition: server.go (W3A) wires a *declarationService into the
// composed Server via newDeclarationService(*declaration.Manager).
//
// All handlers follow the same shape:
//
//	1. Reject nil request (InvalidArgument).
//	2. Validate WireVersion (FailedPrecondition on mismatch).
//	3. Translate proto fields into typed core handles.
//	4. Call the manager method.
//	5. errToStatus the result; return Empty on success.
//
// No business logic lives here. See docs/idd.md §1.1.4 for the
// sentinel-to-gRPC-code table that errToStatus implements.

package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// declarationService is the concrete DeclarationServiceServer impl. It
// embeds UnimplementedDeclarationServiceServer for forward compatibility
// (gRPC v1.65+ requirement; protoc-gen-go-grpc emits a deprecation
// otherwise).
type declarationService struct {
	rtiv1.UnimplementedDeclarationServiceServer
	decl *declaration.Manager
}

// newDeclarationService constructs the service handler bound to the
// given declaration manager. Constructor only — no validation of decl
// (the composer in server.go owns that).
func newDeclarationService(decl *declaration.Manager) *declarationService {
	return &declarationService{decl: decl}
}

// validateWireVersion rejects any request whose WireVersion is not
// WIRE_VERSION_V1. Returns a status.Error suitable for direct return
// from a handler, or nil on a match.
func validateWireVersion(v rtiv1.WireVersion) error {
	if v != rtiv1.WireVersion_WIRE_VERSION_V1 {
		return status.Errorf(codes.FailedPrecondition,
			"wire version mismatch: got %v, want %v",
			v, rtiv1.WireVersion_WIRE_VERSION_V1)
	}
	return nil
}

// nilRequest returns an InvalidArgument status to be returned when
// a handler receives a nil proto request.
func nilRequest(name string) error {
	return status.Errorf(codes.InvalidArgument, "%s: request must be non-nil", name)
}

// attrHandles converts a slice of uint64 attribute handles into the
// typed core.AttributeHandle slice the manager expects. A nil input
// produces a nil slice (manager treats that as "no attributes" — a
// degenerate but legal call that the manager handles idempotently).
func attrHandles(in []uint64) []core.AttributeHandle {
	if in == nil {
		return nil
	}
	out := make([]core.AttributeHandle, len(in))
	for i, h := range in {
		out[i] = core.AttributeHandle(h)
	}
	return out
}

// PublishObjectClassAttributes records a federate's intent to publish
// the listed attributes of an object class.
func (s *declarationService) PublishObjectClassAttributes(
	ctx context.Context,
	req *rtiv1.PubObjAttrsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("PublishObjectClassAttributes")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.PublishObjectClassAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// UnpublishObjectClassAttributes removes a previously declared
// publication. Idempotent: removing a non-existent declaration is a
// successful no-op (the manager handles that).
func (s *declarationService) UnpublishObjectClassAttributes(
	ctx context.Context,
	req *rtiv1.UnpubObjAttrsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnpublishObjectClassAttributes")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.UnpublishObjectClassAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// SubscribeObjectClassAttributes records a federate's interest in
// receiving updates to the listed attributes of an object class.
func (s *declarationService) SubscribeObjectClassAttributes(
	ctx context.Context,
	req *rtiv1.SubObjAttrsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SubscribeObjectClassAttributes")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.SubscribeObjectClassAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// UnsubscribeObjectClassAttributes removes a subscription. Idempotent.
func (s *declarationService) UnsubscribeObjectClassAttributes(
	ctx context.Context,
	req *rtiv1.UnsubObjAttrsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnsubscribeObjectClassAttributes")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.UnsubscribeObjectClassAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// PublishInteractionClass records a federate's intent to send
// interactions of the given class.
func (s *declarationService) PublishInteractionClass(
	ctx context.Context,
	req *rtiv1.PubInterRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("PublishInteractionClass")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.PublishInteractionClass(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// UnpublishInteractionClass removes a previously declared interaction
// publication. Idempotent.
func (s *declarationService) UnpublishInteractionClass(
	ctx context.Context,
	req *rtiv1.UnpubInterRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnpublishInteractionClass")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.UnpublishInteractionClass(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// SubscribeInteractionClass records a federate's interest in receiving
// interactions of the given class.
func (s *declarationService) SubscribeInteractionClass(
	ctx context.Context,
	req *rtiv1.SubInterRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SubscribeInteractionClass")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.SubscribeInteractionClass(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// UnsubscribeInteractionClass removes a subscription. Idempotent.
func (s *declarationService) UnsubscribeInteractionClass(
	ctx context.Context,
	req *rtiv1.UnsubInterRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnsubscribeInteractionClass")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.decl.UnsubscribeInteractionClass(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}
