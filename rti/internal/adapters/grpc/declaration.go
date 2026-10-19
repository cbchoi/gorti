// DeclarationService gRPC handler — translates the eight declaration
// pub/sub RPCs from the proto layer into calls on
// rti/internal/declaration.Manager.
//
// M2 Wave 3B (TASK-035).
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
	"errors"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// declarationService is the concrete DeclarationServiceServer impl. It
// embeds UnimplementedDeclarationServiceServer for forward compatibility
// (gRPC v1.65+ requirement; protoc-gen-go-grpc emits a deprecation
// otherwise).
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.6): the handler binds to core.DeclarationManagement instead of
// the concrete *declaration.Manager so alternative implementations can
// be wired in at the composition root.
type declarationService struct {
	rtiv1.UnimplementedDeclarationServiceServer
	decl             core.DeclarationManagement
	momSubscriptions momSubscriptionGuard
}

// newDeclarationService constructs the service handler bound to the
// given declaration manager. Constructor only — no validation of decl
// (the composer in server.go owns that).
func newDeclarationService(decl core.DeclarationManagement) *declarationService {
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
		return nil, errToStatus(ctx, err)
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
	if req.AttributeSetPresent != nil && !req.GetAttributeSetPresent() && len(req.AttributeHandles) != 0 {
		return nil, status.Error(codes.InvalidArgument, "omitted attribute set cannot contain attribute handles")
	}
	if explicit, ok := s.decl.(core.DeclarationObjectUnpublication); ok {
		var err error
		if req.GetAttributeSetPresent() || len(req.AttributeHandles) != 0 {
			err = explicit.UnpublishObjectClassAttributeSet(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.ObjectClassHandle(req.ObjectClassHandle), attrHandles(req.AttributeHandles))
		} else {
			err = explicit.UnpublishObjectClass(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.ObjectClassHandle(req.ObjectClassHandle))
		}
		if err != nil {
			return nil, declarationStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if req.GetAttributeSetPresent() && len(req.AttributeHandles) == 0 {
		return nil, status.Error(codes.Unimplemented, "explicit empty unpublication set unavailable")
	}
	if err := s.decl.UnpublishObjectClassAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(ctx, err)
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
	if options, ok := s.decl.(core.DeclarationSubscriptionOptions); ok {
		active := req.Active == nil || req.GetActive()
		err := options.SubscribeObjectClassAttributesWithOptions(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.ObjectClassHandle(req.ObjectClassHandle), attrHandles(req.AttributeHandles), core.SubscriptionOptions{Active: active, UpdateRateDesignator: req.UpdateRateDesignator})
		if err != nil {
			return nil, declarationStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if req.Active != nil || req.UpdateRateDesignator != "" {
		return nil, status.Error(codes.Unimplemented, "declaration subscription options unavailable")
	}
	if err := s.decl.SubscribeObjectClassAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(ctx, err)
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
	if req.AttributeSetPresent != nil && !req.GetAttributeSetPresent() && len(req.AttributeHandles) != 0 {
		return nil, status.Error(codes.InvalidArgument, "omitted attribute set cannot contain attribute handles")
	}
	if explicit, ok := s.decl.(core.DeclarationObjectUnsubscription); ok {
		var err error
		if req.GetAttributeSetPresent() || len(req.AttributeHandles) != 0 {
			err = explicit.UnsubscribeObjectClassAttributeSet(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.ObjectClassHandle(req.ObjectClassHandle), attrHandles(req.AttributeHandles))
		} else {
			err = explicit.UnsubscribeObjectClass(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.ObjectClassHandle(req.ObjectClassHandle))
		}
		if err != nil {
			return nil, declarationStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if req.GetAttributeSetPresent() && len(req.AttributeHandles) == 0 {
		return nil, status.Error(codes.Unimplemented, "explicit empty unsubscription set unavailable")
	}
	if err := s.decl.UnsubscribeObjectClassAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
	); err != nil {
		return nil, errToStatus(ctx, err)
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
		return nil, errToStatus(ctx, err)
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
		return nil, errToStatus(ctx, err)
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
	if err := s.momSubscriptions.validate(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.InteractionClassHandle(req.InteractionClassHandle)); err != nil {
		return nil, declarationStatus(ctx, err)
	}
	if options, ok := s.decl.(core.DeclarationSubscriptionOptions); ok {
		err := options.SubscribeInteractionClassWithOptions(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.InteractionClassHandle(req.InteractionClassHandle), req.Active == nil || req.GetActive())
		if err != nil {
			return nil, declarationStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if req.Active != nil {
		return nil, status.Error(codes.Unimplemented, "declaration subscription options unavailable")
	}
	if err := s.decl.SubscribeInteractionClass(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *declarationService) SetAdvisorySwitch(ctx context.Context, req *rtiv1.SetAdvisorySwitchRequest) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SetAdvisorySwitch")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.SwitchKind < rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_OBJECT_CLASS_RELEVANCE || req.SwitchKind > rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_INTERACTION_RELEVANCE {
		return nil, status.Error(codes.InvalidArgument, "invalid advisory switch kind")
	}
	api, ok := s.decl.(core.AdvisorySwitchManagement)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "advisory switches unavailable")
	}
	if err := api.SetAdvisorySwitch(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), protoAdvisorySwitchToCore(req.SwitchKind), req.Enabled); err != nil {
		return nil, declarationStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

func declarationStatus(ctx context.Context, err error) error {
	name := ""
	code := codes.InvalidArgument
	if errors.Is(err, core.ErrServiceInvocationsReportedViaMOM) {
		name = "FederateServiceInvocationsAreBeingReportedViaMOM"
		code = codes.FailedPrecondition
	}
	var duplicate *core.AdvisorySwitchAlreadySetError
	if errors.Is(err, core.ErrInvalidUpdateRateDesignator) {
		name = "InvalidUpdateRateDesignator"
	}
	if errors.As(err, &duplicate) {
		names := map[core.AdvisorySwitchKind]string{core.AdvisoryObjectClassRelevance: "ObjectClassRelevance", core.AdvisoryAttributeRelevance: "AttributeRelevance", core.AdvisoryAttributeScope: "AttributeScope", core.AdvisoryInteractionRelevance: "InteractionRelevance"}
		name = names[duplicate.Kind] + "AdvisorySwitchIsOff"
		if duplicate.Enabled {
			name = names[duplicate.Kind] + "AdvisorySwitchIsOn"
		}
		code = codes.FailedPrecondition
	}
	if name != "" {
		_ = grpcgo.SetTrailer(ctx, metadata.Pairs(specExceptionTrailerKey, name))
		return status.Error(code, err.Error())
	}
	if errors.Is(err, core.ErrInvalidAdvisorySwitch) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return errToStatus(ctx, err)
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
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}
func protoAdvisorySwitchToCore(kind rtiv1.AdvisorySwitchKind) core.AdvisorySwitchKind {
	switch kind {
	case rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_OBJECT_CLASS_RELEVANCE:
		return core.AdvisoryObjectClassRelevance
	case rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_ATTRIBUTE_RELEVANCE:
		return core.AdvisoryAttributeRelevance
	case rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_ATTRIBUTE_SCOPE:
		return core.AdvisoryAttributeScope
	case rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_INTERACTION_RELEVANCE:
		return core.AdvisoryInteractionRelevance
	default:
		return 0
	}
}
