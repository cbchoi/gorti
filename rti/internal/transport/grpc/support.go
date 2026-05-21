// SupportService gRPC handler — translates rti.v1.SupportService RPCs
// into FOM-handle lookups. M25 Phase B (IEEE 1516.1-2010 §10.2).
//
// All methods are read-only against the per-federation FOM held by
// core.FOMRepository. The repository's Get returns a core.FOMHandle;
// the handler type-asserts that to core.FOMHandleNameLookup to access
// reverse + dimension lookups. Composition fault (production handle
// does not satisfy the lookup contract) returns codes.Internal.

package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Order-type / transport-type encodings per IEEE 1516.1-2010 §10.2
// (these are MIM-standard enum values; the SDK and the SupportService
// agree on the integer codes so the wire is symmetric).
const (
	OrderTypeReceive    uint32 = 1
	OrderTypeTimeStamp  uint32 = 2
	TransportReliable   uint32 = 1
	TransportBestEffort uint32 = 2
)

// supportService is the SupportServiceServer impl. Embeds the
// Unimplemented* shim per generated-code requirement.
type supportService struct {
	rtiv1.UnimplementedSupportServiceServer
	foms core.FOMRepository
}

func newSupportService(foms core.FOMRepository) *supportService {
	return &supportService{foms: foms}
}

// resolveLookup gets the FOM for a federation and asserts the reverse-
// lookup interface. Centralises the boilerplate that every handler
// needs.
func (s *supportService) resolveLookup(ctx context.Context, fed string) (core.FOMHandleNameLookup, error) {
	if strings.TrimSpace(fed) == "" {
		return nil, status.Error(codes.InvalidArgument, "federation_name is required")
	}
	h, err := s.foms.Get(ctx, core.FederationName(fed))
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "federation %q: %v", fed, err)
	}
	nl, ok := h.(core.FOMHandleNameLookup)
	if !ok {
		return nil, status.Error(codes.Internal,
			"FOM handle does not implement reverse-lookup (composition fault)")
	}
	return nl, nil
}

func (s *supportService) GetObjectClassHandle(ctx context.Context, req *rtiv1.GetObjectClassHandleRequest) (*rtiv1.GetObjectClassHandleResponse, error) {
	if req == nil {
		return nil, nilRequest("GetObjectClassHandle")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	fh, ok := nl.(core.FOMHandle)
	if !ok {
		return nil, status.Error(codes.Internal, "FOM lookup missing forward-lookup contract")
	}
	h, ok := fh.LookupObjectClass(req.GetClassName())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "object class %q not found", req.GetClassName())
	}
	return &rtiv1.GetObjectClassHandleResponse{ClassHandle: uint64(h)}, nil
}

func (s *supportService) GetObjectClassName(ctx context.Context, req *rtiv1.GetObjectClassNameRequest) (*rtiv1.GetObjectClassNameResponse, error) {
	if req == nil {
		return nil, nilRequest("GetObjectClassName")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	name, ok := nl.ObjectClassName(core.ObjectClassHandle(req.GetClassHandle()))
	if !ok {
		return nil, status.Errorf(codes.NotFound, "object class handle %d not found", req.GetClassHandle())
	}
	return &rtiv1.GetObjectClassNameResponse{ClassName: name}, nil
}

func (s *supportService) GetAttributeHandle(ctx context.Context, req *rtiv1.GetAttributeHandleRequest) (*rtiv1.GetAttributeHandleResponse, error) {
	if req == nil {
		return nil, nilRequest("GetAttributeHandle")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	fh, ok := nl.(core.FOMHandle)
	if !ok {
		return nil, status.Error(codes.Internal, "FOM lookup missing forward-lookup contract")
	}
	h, ok := fh.LookupAttribute(core.ObjectClassHandle(req.GetClassHandle()), req.GetAttributeName())
	if !ok {
		return nil, status.Errorf(codes.NotFound,
			"attribute %q on class handle %d not found", req.GetAttributeName(), req.GetClassHandle())
	}
	return &rtiv1.GetAttributeHandleResponse{AttributeHandle: uint64(h)}, nil
}

func (s *supportService) GetAttributeName(ctx context.Context, req *rtiv1.GetAttributeNameRequest) (*rtiv1.GetAttributeNameResponse, error) {
	if req == nil {
		return nil, nilRequest("GetAttributeName")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	name, ok := nl.AttributeName(
		core.ObjectClassHandle(req.GetClassHandle()),
		core.AttributeHandle(req.GetAttributeHandle()),
	)
	if !ok {
		return nil, status.Errorf(codes.NotFound,
			"attribute handle %d on class %d not found", req.GetAttributeHandle(), req.GetClassHandle())
	}
	return &rtiv1.GetAttributeNameResponse{AttributeName: name}, nil
}

func (s *supportService) GetInteractionClassHandle(ctx context.Context, req *rtiv1.GetInteractionClassHandleRequest) (*rtiv1.GetInteractionClassHandleResponse, error) {
	if req == nil {
		return nil, nilRequest("GetInteractionClassHandle")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	fh, ok := nl.(core.FOMHandle)
	if !ok {
		return nil, status.Error(codes.Internal, "FOM lookup missing forward-lookup contract")
	}
	h, ok := fh.LookupInteractionClass(req.GetClassName())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "interaction class %q not found", req.GetClassName())
	}
	return &rtiv1.GetInteractionClassHandleResponse{ClassHandle: uint64(h)}, nil
}

func (s *supportService) GetInteractionClassName(ctx context.Context, req *rtiv1.GetInteractionClassNameRequest) (*rtiv1.GetInteractionClassNameResponse, error) {
	if req == nil {
		return nil, nilRequest("GetInteractionClassName")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	name, ok := nl.InteractionClassName(core.InteractionClassHandle(req.GetClassHandle()))
	if !ok {
		return nil, status.Errorf(codes.NotFound, "interaction class handle %d not found", req.GetClassHandle())
	}
	return &rtiv1.GetInteractionClassNameResponse{ClassName: name}, nil
}

func (s *supportService) GetParameterHandle(ctx context.Context, req *rtiv1.GetParameterHandleRequest) (*rtiv1.GetParameterHandleResponse, error) {
	if req == nil {
		return nil, nilRequest("GetParameterHandle")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	fh, ok := nl.(core.FOMHandle)
	if !ok {
		return nil, status.Error(codes.Internal, "FOM lookup missing forward-lookup contract")
	}
	h, ok := fh.LookupParameter(core.InteractionClassHandle(req.GetClassHandle()), req.GetParameterName())
	if !ok {
		return nil, status.Errorf(codes.NotFound,
			"parameter %q on interaction class %d not found", req.GetParameterName(), req.GetClassHandle())
	}
	return &rtiv1.GetParameterHandleResponse{ParameterHandle: uint64(h)}, nil
}

func (s *supportService) GetParameterName(ctx context.Context, req *rtiv1.GetParameterNameRequest) (*rtiv1.GetParameterNameResponse, error) {
	if req == nil {
		return nil, nilRequest("GetParameterName")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	name, ok := nl.ParameterName(
		core.InteractionClassHandle(req.GetClassHandle()),
		core.ParameterHandle(req.GetParameterHandle()),
	)
	if !ok {
		return nil, status.Errorf(codes.NotFound,
			"parameter handle %d on interaction class %d not found",
			req.GetParameterHandle(), req.GetClassHandle())
	}
	return &rtiv1.GetParameterNameResponse{ParameterName: name}, nil
}

func (s *supportService) GetDimensionHandle(ctx context.Context, req *rtiv1.GetDimensionHandleRequest) (*rtiv1.GetDimensionHandleResponse, error) {
	if req == nil {
		return nil, nilRequest("GetDimensionHandle")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	h, ok := nl.LookupDimension(req.GetDimensionName())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "dimension %q not found", req.GetDimensionName())
	}
	return &rtiv1.GetDimensionHandleResponse{DimensionHandle: uint64(h)}, nil
}

func (s *supportService) GetDimensionName(ctx context.Context, req *rtiv1.GetDimensionNameRequest) (*rtiv1.GetDimensionNameResponse, error) {
	if req == nil {
		return nil, nilRequest("GetDimensionName")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	name, ok := nl.DimensionName(core.DimensionHandle(req.GetDimensionHandle()))
	if !ok {
		return nil, status.Errorf(codes.NotFound, "dimension handle %d not found", req.GetDimensionHandle())
	}
	return &rtiv1.GetDimensionNameResponse{DimensionName: name}, nil
}

func (s *supportService) GetDimensionUpperBound(ctx context.Context, req *rtiv1.GetDimensionUpperBoundRequest) (*rtiv1.GetDimensionUpperBoundResponse, error) {
	if req == nil {
		return nil, nilRequest("GetDimensionUpperBound")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nl, err := s.resolveLookup(ctx, req.GetFederationName())
	if err != nil {
		return nil, err
	}
	ub, ok := nl.DimensionUpperBound(core.DimensionHandle(req.GetDimensionHandle()))
	if !ok {
		return nil, status.Errorf(codes.NotFound, "dimension handle %d not found", req.GetDimensionHandle())
	}
	return &rtiv1.GetDimensionUpperBoundResponse{UpperBound: ub}, nil
}

// Order / transportation lookups — pure functions over the MIM enum,
// no FOM resolution required. Names are case-insensitive.
func (s *supportService) GetOrderType(_ context.Context, req *rtiv1.GetOrderTypeRequest) (*rtiv1.GetOrderTypeResponse, error) {
	if req == nil {
		return nil, nilRequest("GetOrderType")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	switch strings.ToLower(req.GetOrderName()) {
	case "receive":
		return &rtiv1.GetOrderTypeResponse{OrderType: OrderTypeReceive}, nil
	case "timestamp":
		return &rtiv1.GetOrderTypeResponse{OrderType: OrderTypeTimeStamp}, nil
	default:
		return nil, status.Errorf(codes.NotFound, "order name %q not recognised", req.GetOrderName())
	}
}

func (s *supportService) GetOrderName(_ context.Context, req *rtiv1.GetOrderNameRequest) (*rtiv1.GetOrderNameResponse, error) {
	if req == nil {
		return nil, nilRequest("GetOrderName")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	switch req.GetOrderType() {
	case OrderTypeReceive:
		return &rtiv1.GetOrderNameResponse{OrderName: "Receive"}, nil
	case OrderTypeTimeStamp:
		return &rtiv1.GetOrderNameResponse{OrderName: "TimeStamp"}, nil
	default:
		return nil, status.Errorf(codes.NotFound, "order type %d not recognised", req.GetOrderType())
	}
}

func (s *supportService) GetTransportationType(_ context.Context, req *rtiv1.GetTransportationTypeRequest) (*rtiv1.GetTransportationTypeResponse, error) {
	if req == nil {
		return nil, nilRequest("GetTransportationType")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	switch strings.ToLower(req.GetTransportationName()) {
	case "hlareliable", "reliable":
		return &rtiv1.GetTransportationTypeResponse{TransportationType: TransportReliable}, nil
	case "hlabesteffort", "besteffort", "best-effort":
		return &rtiv1.GetTransportationTypeResponse{TransportationType: TransportBestEffort}, nil
	default:
		return nil, status.Errorf(codes.NotFound, "transportation name %q not recognised", req.GetTransportationName())
	}
}

func (s *supportService) GetTransportationName(_ context.Context, req *rtiv1.GetTransportationNameRequest) (*rtiv1.GetTransportationNameResponse, error) {
	if req == nil {
		return nil, nilRequest("GetTransportationName")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	switch req.GetTransportationType() {
	case TransportReliable:
		return &rtiv1.GetTransportationNameResponse{TransportationName: "HLAreliable"}, nil
	case TransportBestEffort:
		return &rtiv1.GetTransportationNameResponse{TransportationName: "HLAbestEffort"}, nil
	default:
		return nil, status.Errorf(codes.NotFound, "transportation type %d not recognised", req.GetTransportationType())
	}
}
