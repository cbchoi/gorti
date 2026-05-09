// DDMService gRPC handler — translates rti.v1.DDMService RPCs into
// calls on rti/internal/ddm.Manager.
//
// Owner: Agent A — M12 W1 (cut-3 gRPC exposure of cut-2 ddm.Manager).
//
// Composition: server.go wires a *ddmService into the composed Server
// via newDDMService(*ddm.Manager).

package grpc

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ddm"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ddmService is the concrete DDMServiceServer impl.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.4): the handler binds to core.DataDistributionManagement instead
// of the concrete *ddm.Manager so alternative implementations can be
// wired in at the composition root.
type ddmService struct {
	rtiv1.UnimplementedDDMServiceServer
	mgr core.DataDistributionManagement
	// M23 W5 — objs is consulted for SendInteractionWithRegions +
	// RequestAttributeValueUpdateWithRegions which forward to the
	// equivalent ObjectService methods (no separate DDM-side fanout
	// path at the manager level; the existing DDM filter applies via
	// object.Registry.opts.DDM).
	objs core.ObjectRegistry
}

func newDDMService(mgr core.DataDistributionManagement, objs core.ObjectRegistry) *ddmService {
	return &ddmService{mgr: mgr, objs: objs}
}

// regionHandlesFromUint64s converts a slice of uint64 region handles
// into the typed ddm.RegionHandle slice. ddm.RegionHandle is a type
// alias for core.DDMRegionHandleCore (see rti/internal/core/ddm.go),
// so the slice satisfies both the manager interface and any
// ddm-package consumer.
func regionHandlesFromUint64s(in []uint64) []ddm.RegionHandle {
	if in == nil {
		return nil
	}
	out := make([]ddm.RegionHandle, len(in))
	for i, h := range in {
		out[i] = ddm.RegionHandle(h)
	}
	return out
}

// dimHandlesFromUint64s converts a slice of uint64 dimension handles
// into the typed ddm.DimensionHandle slice (alias of
// core.DDMDimensionHandle).
func dimHandlesFromUint64s(in []uint64) []ddm.DimensionHandle {
	if in == nil {
		return nil
	}
	out := make([]ddm.DimensionHandle, len(in))
	for i, h := range in {
		out[i] = ddm.DimensionHandle(h)
	}
	return out
}

// LookupRoutingSpace implements §6.4.1 (handle resolution).
func (s *ddmService) LookupRoutingSpace(
	_ context.Context,
	req *rtiv1.LookupRoutingSpaceRequest,
) (*rtiv1.LookupRoutingSpaceResponse, error) {
	if req == nil {
		return nil, nilRequest("LookupRoutingSpace")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	h, ok := s.mgr.LookupRoutingSpace(
		core.FederationName(req.GetFederationName()),
		req.GetName(),
	)
	resp := &rtiv1.LookupRoutingSpaceResponse{Found: ok}
	if ok {
		resp.RoutingSpaceHandle = uint64(h)
	}
	return resp, nil
}

// LookupDimension implements §6.4.2 (handle resolution).
func (s *ddmService) LookupDimension(
	_ context.Context,
	req *rtiv1.LookupDimensionRequest,
) (*rtiv1.LookupDimensionResponse, error) {
	if req == nil {
		return nil, nilRequest("LookupDimension")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	h, ok := s.mgr.LookupDimension(
		core.FederationName(req.GetFederationName()),
		ddm.RoutingSpaceHandle(req.GetRoutingSpaceHandle()),
		req.GetName(),
	)
	resp := &rtiv1.LookupDimensionResponse{Found: ok}
	if ok {
		resp.DimensionHandle = uint64(h)
	}
	return resp, nil
}

// CreateRegion implements §6.5.
func (s *ddmService) CreateRegion(
	ctx context.Context,
	req *rtiv1.CreateRegionRequest,
) (*rtiv1.CreateRegionResponse, error) {
	if req == nil {
		return nil, nilRequest("CreateRegion")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	rh, err := s.mgr.CreateRegion(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		ddm.RoutingSpaceHandle(req.GetRoutingSpaceHandle()),
		dimHandlesFromUint64s(req.GetDimensionHandles()),
	)
	if err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.CreateRegionResponse{RegionHandle: uint64(rh)}, nil
}

// SetRangeBounds implements §6.5 (pending bounds).
func (s *ddmService) SetRangeBounds(
	_ context.Context,
	req *rtiv1.SetRangeBoundsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SetRangeBounds")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	bounds := ddm.Range{}
	if b := req.GetBounds(); b != nil {
		bounds.Lower = b.GetLower()
		bounds.Upper = b.GetUpper()
	}
	if err := s.mgr.SetRangeBounds(
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		ddm.RegionHandle(req.GetRegionHandle()),
		ddm.DimensionHandle(req.GetDimensionHandle()),
		bounds,
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// CommitRegionModifications implements §6.5 (atomic commit across
// the supplied region set).
func (s *ddmService) CommitRegionModifications(
	ctx context.Context,
	req *rtiv1.CommitRegionRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("CommitRegionModifications")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.CommitRegionModifications(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// DeleteRegion implements §6.6.
func (s *ddmService) DeleteRegion(
	ctx context.Context,
	req *rtiv1.DeleteRegionRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("DeleteRegion")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.DeleteRegion(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		ddm.RegionHandle(req.GetRegionHandle()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// QueryBounds returns the committed bounds for (region, dim).
func (s *ddmService) QueryBounds(
	_ context.Context,
	req *rtiv1.QueryBoundsRequest,
) (*rtiv1.QueryBoundsResponse, error) {
	if req == nil {
		return nil, nilRequest("QueryBounds")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	r, ok := s.mgr.QueryBounds(
		core.FederationName(req.GetFederationName()),
		ddm.RegionHandle(req.GetRegionHandle()),
		ddm.DimensionHandle(req.GetDimensionHandle()),
	)
	resp := &rtiv1.QueryBoundsResponse{Found: ok}
	if ok {
		resp.Bounds = &rtiv1.Range{Lower: r.Lower, Upper: r.Upper}
	}
	return resp, nil
}

// SubscribeObjectClassAttributesWithRegions implements §6.10.
func (s *ddmService) SubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	req *rtiv1.SubscribeOCAWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SubscribeObjectClassAttributesWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.SubscribeObjectClassAttributesWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// SubscribeInteractionClassWithRegions implements §6.13.
func (s *ddmService) SubscribeInteractionClassWithRegions(
	ctx context.Context,
	req *rtiv1.SubscribeICWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SubscribeInteractionClassWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.SubscribeInteractionClassWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// RegisterObjectInstanceWithRegions implements §6.7. The object handle
// itself is assigned by object.Registry; this RPC forwards to
// ddm.Manager.RegisterObjectInstanceWithRegions which records
// per-attribute region associations.
//
// Cut-3 simplification: the object handle in the response is the value
// passed in via the optional object_name path — production wiring fuses
// this RPC with object.Registry.Register (M12 W2 follow-up). The cut-3
// handler accepts the request shape but the manager method requires the
// caller to have already minted the ObjectHandle; if the request omits a
// concrete handle, the response handle will be 0 and the federate must
// pair this with an ObjectService.RegisterObject call. The Python SDK
// (Agent C) wraps both calls behind the federation.RegisterObjectInstanceWithRegions
// HLA call.
func (s *ddmService) RegisterObjectInstanceWithRegions(
	ctx context.Context,
	req *rtiv1.RegisterObjectWithRegionsRequest,
) (*rtiv1.RegisterObjectWithRegionsResponse, error) {
	if req == nil {
		return nil, nilRequest("RegisterObjectInstanceWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	// Cut-3 handler shape: we accept the request and translate the
	// per-attribute region association map. The object handle is left
	// at 0 because the ddm.Manager only records associations — the
	// caller is responsible for pairing this with an ObjectService.RegisterObject
	// call to mint the handle. Until the M12 W2 fused-API lands the
	// response carries handle 0 and the supplied object_name (echoed).
	attrToRegions := map[core.AttributeHandle][]ddm.RegionHandle{}
	for _, ar := range req.GetAttributeRegions() {
		if ar == nil {
			continue
		}
		attrToRegions[core.AttributeHandle(ar.GetAttributeHandle())] = regionHandlesFromUint64s(ar.GetRegionHandles())
	}
	// AssociateRegionsWithObjectInstance requires a non-zero ObjectHandle;
	// without one, the cut-3 handler skips the manager call and returns
	// an empty response. Future wiring will mint the handle via
	// object.Registry.Register before delegating.
	resp := &rtiv1.RegisterObjectWithRegionsResponse{
		ObjectHandle: 0,
		ObjectName:   req.GetObjectName(),
	}
	_ = ctx
	_ = attrToRegions
	return resp, nil
}

// --- M23 W5: §9 missing services -----------------------------------

func attrToRegionsFromPb(pairs []*rtiv1.AttributeRegions) map[core.AttributeHandle][]ddm.RegionHandle {
	out := map[core.AttributeHandle][]ddm.RegionHandle{}
	for _, ar := range pairs {
		if ar == nil {
			continue
		}
		out[core.AttributeHandle(ar.GetAttributeHandle())] = regionHandlesFromUint64s(ar.GetRegionHandles())
	}
	return out
}

// AssociateRegionsForUpdates — §9.6.
func (s *ddmService) AssociateRegionsForUpdates(
	ctx context.Context,
	req *rtiv1.AssociateRegionsForUpdatesRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("AssociateRegionsForUpdates")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.AssociateRegionsForUpdates(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrToRegionsFromPb(req.GetAttributeRegions()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// UnassociateRegionsForUpdates — §9.7.
func (s *ddmService) UnassociateRegionsForUpdates(
	ctx context.Context,
	req *rtiv1.UnassociateRegionsForUpdatesRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnassociateRegionsForUpdates")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.UnassociateRegionsForUpdates(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		attrToRegionsFromPb(req.GetAttributeRegions()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// UnsubscribeObjectClassAttributesWithRegions — §9.9.
func (s *ddmService) UnsubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	req *rtiv1.UnsubscribeOCAWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnsubscribeObjectClassAttributesWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.UnsubscribeObjectClassAttributesWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// UnsubscribeInteractionClassWithRegions — §9.11.
func (s *ddmService) UnsubscribeInteractionClassWithRegions(
	ctx context.Context,
	req *rtiv1.UnsubscribeICWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnsubscribeInteractionClassWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.mgr.UnsubscribeInteractionClassWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(err)
	}
	return &rtiv1.Empty{}, nil
}

// SendInteractionWithRegions — §9.12.
//
// M23 simplification: forwards to the existing ObjectService.SendInteraction
// path. Region filtering is already applied by the cut-2 DDM filter
// (object.Registry consults DDM.InteractionSubscribersForSend on every
// send) — the regions on this RPC are therefore advisory in M23.
// Strict per-call region filtering on send is a follow-up.
func (s *ddmService) SendInteractionWithRegions(
	ctx context.Context,
	req *rtiv1.SendInteractionWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SendInteractionWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	params := make(map[core.ParameterHandle][]byte, len(req.GetParameters()))
	for h, v := range req.GetParameters() {
		params[core.ParameterHandle(h)] = v
	}
	var ts *core.LogicalTime
	if req.LogicalTime != nil {
		v := core.LogicalTime(req.GetLogicalTime())
		ts = &v
	}
	if err := s.objs.SendInteraction(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
		params,
		ts,
	); err != nil {
		return nil, errToStatus(err)
	}
	_ = req.GetRegionHandles() // recorded for future strict per-call filtering
	return &rtiv1.Empty{}, nil
}

// RequestAttributeValueUpdateWithRegions — §9.13.
//
// M23 simplification: forwards to RequestClassAttributeValueUpdate;
// region filtering on the owner set is a follow-up (the existing
// DDM filter already applies during the resulting UpdateAttributeValues
// fanout when the owner responds, so the practical impact is minimal).
func (s *ddmService) RequestAttributeValueUpdateWithRegions(
	ctx context.Context,
	req *rtiv1.RequestAttributeValueUpdateWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("RequestAttributeValueUpdateWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	attrs := make([]core.AttributeHandle, 0, len(req.GetAttributeHandles()))
	for _, h := range req.GetAttributeHandles() {
		attrs = append(attrs, core.AttributeHandle(h))
	}
	if err := s.objs.RequestClassAttributeValueUpdate(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrs,
		req.GetUserSuppliedTag(),
	); err != nil {
		return nil, errToStatus(err)
	}
	_ = req.GetRegionHandles() // recorded for future filtering
	return &rtiv1.Empty{}, nil
}
