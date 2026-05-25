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
	// M20.2 — if the client supplied a non-zero retraction handle, take
	// the Retractable path so the TSO buffer carries the handle.
	rh := req.GetMessageRetractionHandle()
	if rh != 0 {
		if err := s.obj.UpdateAttributesRetractable(
			ctx,
			core.FederationName(req.GetFederationName()),
			core.FederateHandle(req.GetFederateHandle()),
			core.ObjectHandle(req.GetObjectHandle()),
			attrs,
			toLogicalTime(req.LogicalTime),
			rh,
		); err != nil {
			return nil, errToStatus(err)
		}
		return &rtiv1.Empty{}, nil
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
	rh := req.GetMessageRetractionHandle()
	if rh != 0 {
		if err := s.obj.SendInteractionRetractable(
			ctx,
			core.FederationName(req.GetFederationName()),
			core.FederateHandle(req.GetFederateHandle()),
			core.InteractionClassHandle(req.GetInteractionClassHandle()),
			params,
			toLogicalTime(req.LogicalTime),
			rh,
		); err != nil {
			return nil, errToStatus(err)
		}
		return &rtiv1.Empty{}, nil
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

// Retract — IEEE 1516.1-2010 §8.21 (M20.2). Removes the buffered TSO
// event identified by (federate, message_retraction_handle). Returns
// OK whether or not a buffered event matched: the message may have
// already been delivered or never sent with a tracking handle.
func (s *objectService) Retract(ctx context.Context, req *rtiv1.RetractRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	_ = s.obj.RetractMessage(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		req.GetMessageRetractionHandle(),
	)
	return &rtiv1.Empty{}, nil
}

// DeleteObjectInstance — IEEE 1516.1-2010 §6.16 (M23 W1).
func (s *objectService) DeleteObjectInstance(ctx context.Context, req *rtiv1.DeleteObjectInstanceRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if err := s.obj.Delete(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		toLogicalTime(req.LogicalTime),
		req.GetUserSuppliedTag(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// LocalDeleteObjectInstance — IEEE 1516.1-2010 §6.18 (M23 W2).
func (s *objectService) LocalDeleteObjectInstance(ctx context.Context, req *rtiv1.LocalDeleteObjectInstanceRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if err := s.obj.LocalDelete(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// RequestAttributeValueUpdate — IEEE 1516.1-2010 §6.24 (M23 W2).
func (s *objectService) RequestAttributeValueUpdate(ctx context.Context, req *rtiv1.RequestAttributeValueUpdateRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	attrs := make([]core.AttributeHandle, 0, len(req.GetAttributeHandles()))
	for _, h := range req.GetAttributeHandles() {
		attrs = append(attrs, core.AttributeHandle(h))
	}
	if err := s.obj.RequestAttributeValueUpdate(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
		req.GetUserSuppliedTag(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// RequestClassAttributeValueUpdate — IEEE 1516.1-2010 §6.25 (M23 W2).
func (s *objectService) RequestClassAttributeValueUpdate(ctx context.Context, req *rtiv1.RequestClassAttributeValueUpdateRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	attrs := make([]core.AttributeHandle, 0, len(req.GetAttributeHandles()))
	for _, h := range req.GetAttributeHandles() {
		attrs = append(attrs, core.AttributeHandle(h))
	}
	if err := s.obj.RequestClassAttributeValueUpdate(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrs,
		req.GetUserSuppliedTag(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// ChangeAttributeTransportationType — IEEE 1516.1-2010 §6.20 (M23 W3).
func (s *objectService) ChangeAttributeTransportationType(ctx context.Context, req *rtiv1.ChangeAttributeTransportRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	attrs := make([]core.AttributeHandle, 0, len(req.GetAttributeHandles()))
	for _, h := range req.GetAttributeHandles() {
		attrs = append(attrs, core.AttributeHandle(h))
	}
	if err := s.obj.ChangeAttributeTransportType(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrs,
		protoTransportTypeToCore(req.GetTransportType()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// ChangeInteractionTransportationType — IEEE 1516.1-2010 §6.22 (M23 W3).
func (s *objectService) ChangeInteractionTransportationType(ctx context.Context, req *rtiv1.ChangeInteractionTransportRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	if err := s.obj.ChangeInteractionTransportType(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
		protoTransportTypeToCore(req.GetTransportType()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// protoTransportTypeToCore maps the wire enum to the core type.
// Unspecified at the wire surfaces as core.TransportTypeUnspecified
// (which the manager rejects with ErrTransportTypeUnspecified).
func protoTransportTypeToCore(t rtiv1.TransportationType) core.TransportType {
	switch t {
	case rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE:
		return core.TransportTypeReliable
	case rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT:
		return core.TransportTypeBestEffort
	default:
		return core.TransportTypeUnspecified
	}
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

// M26 Phase F — object instance name reservation handlers
// (IEEE 1516.1-2010 §6.1-6.5). The handlers type-assert the
// composed ObjectRegistry to core.ObjectInstanceNameReserver;
// composition root in cmd/rtid passes *object.Registry which
// implements both. If the registry doesn't satisfy the reserver
// contract, the handler returns Unimplemented — preserves test
// stubs that don't need reservation.

func (s *objectService) ReserveObjectInstanceName(ctx context.Context, req *rtiv1.ReserveObjectInstanceNameRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	res, ok := s.obj.(core.ObjectInstanceNameReserver)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "object registry does not support name reservation")
	}
	if err := res.ReserveObjectInstanceName(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		req.GetObjectName(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *objectService) ReleaseObjectInstanceName(ctx context.Context, req *rtiv1.ReleaseObjectInstanceNameRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	res, ok := s.obj.(core.ObjectInstanceNameReserver)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "object registry does not support name reservation")
	}
	if err := res.ReleaseObjectInstanceName(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		req.GetObjectName(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

func (s *objectService) ReserveMultipleObjectInstanceNames(ctx context.Context, req *rtiv1.ReserveMultipleObjectInstanceNamesRequest) (*rtiv1.Empty, error) {
	if !validWireVersion(req.GetWireVersion()) {
		return nil, status.Error(codes.FailedPrecondition, "unsupported wire version")
	}
	res, ok := s.obj.(core.ObjectInstanceNameReserver)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "object registry does not support name reservation")
	}
	if err := res.ReserveMultipleObjectInstanceNames(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		req.GetObjectNames(),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}
