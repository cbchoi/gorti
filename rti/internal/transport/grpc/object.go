package grpc

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// objectService binds rtiv1.ObjectServiceServer to a core.ObjectRegistry.
// Handlers are thin: validate wire version, translate proto -> core call,
// translate core error -> gRPC status. No business logic lives here.
type objectService struct {
	rtiv1.UnimplementedObjectServiceServer
	obj core.ObjectRegistry
}

func newObjectService(obj core.ObjectRegistry) *objectService {
	return &objectService{obj: obj}
}

// RegisterObjectInstance translates and forwards to ObjectRegistry.Register.
func (s *objectService) RegisterObjectInstance(ctx context.Context, req *rtiv1.RegisterObjectRequest) (*rtiv1.RegisterObjectResponse, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	handle, name, err := s.obj.Register(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		req.GetObjectName(),
	)
	if err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.RegisterObjectResponse{
		ObjectHandle: uint64(handle),
		ObjectName:   name,
	}, nil
}

// UpdateAttributeValues translates and forwards to ObjectRegistry.UpdateAttributes.
// The optional logical_time field maps nil->RO, non-nil->TSO.
func (s *objectService) UpdateAttributeValues(ctx context.Context, req *rtiv1.UpdateAttributeValuesRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	attrs := make(map[core.AttributeHandle][]byte, len(req.GetAttributes()))
	for h, v := range req.GetAttributes() {
		attrs[core.AttributeHandle(h)] = v
	}
	if err := s.obj.UpdateAttributes(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
		toLogicalTime(req.LogicalTime),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// SendInteraction translates and forwards to ObjectRegistry.SendInteraction.
func (s *objectService) SendInteraction(ctx context.Context, req *rtiv1.SendInteractionRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	params := make(map[core.ParameterHandle][]byte, len(req.GetParameters()))
	for h, v := range req.GetParameters() {
		params[core.ParameterHandle(h)] = v
	}
	if err := s.obj.SendInteraction(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
		params,
		toLogicalTime(req.LogicalTime),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// validWireVersion returns true when the wire version on a request is one
// the server understands. WIRE_VERSION_UNSPECIFIED is rejected: clients
// must opt into a versioned dialect.
func validWireVersion(v rtiv1.WireVersion) bool {
	return v == rtiv1.WireVersion_WIRE_VERSION_V1
}

// toLogicalTime converts the optional double on the wire into a
// *core.LogicalTime. nil pointer means RO; non-nil means TSO.
func toLogicalTime(p *float64) *core.LogicalTime {
	if p == nil {
		return nil
	}
	t := core.LogicalTime(*p)
	return &t
}

// Compile-time assertion that objectService implements the generated
// server interface.
var _ rtiv1.ObjectServiceServer = (*objectService)(nil)
