// DDMService gRPC handler — translates rti.v1.DDMService RPCs into
// calls on rti/internal/ddm.Manager.
//
// M12 W1 exposes the cut-2 ddm.Manager through gRPC.
//
// Composition: server.go wires a *ddmService into the composed Server
// via newDDMService(*ddm.Manager).

package grpc

import (
	"context"
	"slices"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ddmService is the concrete DDMServiceServer impl.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.4): the handler binds to core.DataDistributionManagement instead
// of the concrete *ddm.Manager so alternative implementations can be
// wired in at the composition root.
type ddmService = ddmServiceOf[core.LogicalTime]

type ddmServiceOf[T core.LogicalTimeScalar] struct {
	rtiv1.UnimplementedDDMServiceServer
	mgr core.DataDistributionManagement
	// M23 W5 — objs is consulted for SendInteractionWithRegions +
	// RequestAttributeValueUpdateWithRegions which forward to the
	// equivalent ObjectService methods (no separate DDM-side fanout
	// path at the manager level; the existing DDM filter applies via
	// object.Registry.opts.DDM).
	objs             core.ObjectRegistryOf[T]
	callerIdentity   func(context.Context) (core.MemberIdentity, error)
	momSubscriptions momSubscriptionGuard
}

func newDDMService(mgr core.DataDistributionManagement, objs core.ObjectRegistry) *ddmService {
	return newDDMServiceOf(mgr, objs)
}

func newDDMServiceOf[T core.LogicalTimeScalar](mgr core.DataDistributionManagement, objs core.ObjectRegistryOf[T]) *ddmServiceOf[T] {
	return &ddmServiceOf[T]{mgr: mgr, objs: objs}
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
func (s *ddmServiceOf[T]) LookupRoutingSpace(
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
func (s *ddmServiceOf[T]) LookupDimension(
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
func (s *ddmServiceOf[T]) CreateRegion(
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
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.CreateRegionResponse{RegionHandle: uint64(rh)}, nil
}

// SetRangeBounds implements §6.5 (pending bounds).
func (s *ddmServiceOf[T]) SetRangeBounds(
	ctx context.Context,
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
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// CommitRegionModifications implements §6.5 (atomic commit across
// the supplied region set).
func (s *ddmServiceOf[T]) CommitRegionModifications(
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
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// DeleteRegion implements §6.6.
func (s *ddmServiceOf[T]) DeleteRegion(
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
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// QueryBounds returns the committed bounds for (region, dim).
func (s *ddmServiceOf[T]) QueryBounds(
	ctx context.Context,
	req *rtiv1.QueryBoundsRequest,
) (*rtiv1.QueryBoundsResponse, error) {
	if req == nil {
		return nil, nilRequest("QueryBounds")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if s.callerIdentity != nil {
		identity, err := s.callerIdentity(ctx)
		if err != nil {
			return nil, sessionErrorStatus(ctx, err)
		}
		if identity.Federation != core.FederationName(req.GetFederationName()) {
			return nil, status.Error(codes.PermissionDenied, "region query differs from authenticated federation")
		}
		queries, ok := s.mgr.(interface {
			QueryBoundsFor(core.FederationName, core.FederateHandle, ddm.RegionHandle, ddm.DimensionHandle) (ddm.Range, error)
		})
		if !ok {
			return nil, status.Error(codes.Unimplemented, "participant-scoped region queries unavailable")
		}
		bounds, err := queries.QueryBoundsFor(identity.Federation, identity.Handle, ddm.RegionHandle(req.GetRegionHandle()), ddm.DimensionHandle(req.GetDimensionHandle()))
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.QueryBoundsResponse{Found: true, Bounds: &rtiv1.Range{Lower: bounds.Lower, Upper: bounds.Upper}}, nil
	}
	if checked, ok := s.mgr.(interface {
		QueryBoundsChecked(core.FederationName, ddm.RegionHandle, ddm.DimensionHandle) (ddm.Range, error)
	}); ok {
		r, err := checked.QueryBoundsChecked(core.FederationName(req.GetFederationName()), ddm.RegionHandle(req.GetRegionHandle()), ddm.DimensionHandle(req.GetDimensionHandle()))
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.QueryBoundsResponse{Found: true, Bounds: &rtiv1.Range{Lower: r.Lower, Upper: r.Upper}}, nil
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
func (s *ddmServiceOf[T]) SubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	req *rtiv1.SubscribeOCAWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SubscribeObjectClassAttributesWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	options := core.SubscriptionOptions{Active: true, UpdateRateDesignator: req.GetUpdateRateDesignator()}
	if req.Active != nil {
		options.Active = req.GetActive()
	}
	if req.AttributeRegionGroups != nil {
		groups, err := attributeRegionGroups(req.AttributeRegionGroups, len(req.AttributeHandles)+len(req.RegionHandles) != 0)
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		mgr, ok := s.mgr.(interface {
			SubscribeObjectClassAttributeRegionGroups(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeRegionGroup, core.SubscriptionOptions) error
		})
		if !ok {
			return nil, status.Error(codes.Unimplemented, "atomic grouped regional subscription is unavailable")
		}
		if err := mgr.SubscribeObjectClassAttributeRegionGroups(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectClassHandle(req.GetObjectClassHandle()), groups, options); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if mgr, ok := s.mgr.(interface {
		SubscribeObjectClassAttributesWithRegionsAndOptions(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle, []core.DDMRegionHandleCore, core.SubscriptionOptions) error
	}); ok {
		if err := mgr.SubscribeObjectClassAttributesWithRegionsAndOptions(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectClassHandle(req.GetObjectClassHandle()), attrHandles(req.GetAttributeHandles()), regionHandlesFromUint64s(req.GetRegionHandles()), options); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if req.Active != nil || options.UpdateRateDesignator != "" {
		return nil, status.Error(codes.Unimplemented, "regional subscription options are unavailable")
	}
	if err := s.mgr.SubscribeObjectClassAttributesWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// SubscribeInteractionClassWithRegions implements §6.13.
func (s *ddmServiceOf[T]) SubscribeInteractionClassWithRegions(
	ctx context.Context,
	req *rtiv1.SubscribeICWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("SubscribeInteractionClassWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if err := s.momSubscriptions.validate(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.InteractionClassHandle(req.InteractionClassHandle)); err != nil {
		return nil, declarationStatus(ctx, err)
	}
	active := true
	if req.Active != nil {
		active = req.GetActive()
	}
	if mgr, ok := s.mgr.(interface {
		SubscribeInteractionClassWithRegionsAndOptions(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, []core.DDMRegionHandleCore, bool) error
	}); ok {
		if err := mgr.SubscribeInteractionClassWithRegionsAndOptions(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.InteractionClassHandle(req.GetInteractionClassHandle()), regionHandlesFromUint64s(req.GetRegionHandles()), active); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if req.Active != nil {
		return nil, status.Error(codes.Unimplemented, "regional subscription options are unavailable")
	}
	if err := s.mgr.SubscribeInteractionClassWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// RegisterObjectInstanceWithRegions registers the object and its complete
// region association set in one domain operation, before discovery.
func (s *ddmServiceOf[T]) RegisterObjectInstanceWithRegions(
	ctx context.Context,
	req *rtiv1.RegisterObjectWithRegionsRequest,
) (*rtiv1.RegisterObjectWithRegionsResponse, error) {
	if req == nil {
		return nil, nilRequest("RegisterObjectInstanceWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.AttributeRegionGroups != nil {
		return s.registerObjectWithRegionGroups(ctx, req)
	}
	registrar, ok := s.objs.(interface {
		RegisterWithRegions(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, string, map[core.AttributeHandle][]core.DDMRegionHandleCore) (core.ObjectHandle, string, error)
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "atomic region registration is unavailable")
	}
	bindings := make(map[core.AttributeHandle][]core.DDMRegionHandleCore)
	for _, ar := range req.GetAttributeRegions() {
		if ar == nil || ar.GetAttributeHandle() == 0 || len(ar.GetRegionHandles()) == 0 {
			return nil, errToStatus(ctx, core.ErrWireMalformedMessage)
		}
		for _, region := range ar.GetRegionHandles() {
			if region == 0 {
				return nil, errToStatus(ctx, core.ErrWireMalformedMessage)
			}
		}
		attr := core.AttributeHandle(ar.GetAttributeHandle())
		bindings[attr] = append(bindings[attr], regionHandlesFromUint64s(ar.GetRegionHandles())...)
	}
	for attr, regions := range bindings {
		slices.Sort(regions)
		bindings[attr] = slices.Compact(regions)
	}
	obj, name, err := registrar.RegisterWithRegions(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectClassHandle(req.GetObjectClassHandle()), req.GetObjectName(), bindings)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.RegisterObjectWithRegionsResponse{ObjectHandle: uint64(obj), ObjectName: name}, nil
}

// --- M23 W5: §9 missing services -----------------------------------

func attrToRegionsFromPb(pairs []*rtiv1.AttributeRegions) (map[core.AttributeHandle][]ddm.RegionHandle, error) {
	out := map[core.AttributeHandle][]ddm.RegionHandle{}
	for _, ar := range pairs {
		if ar == nil || ar.GetAttributeHandle() == 0 {
			return nil, core.ErrWireMalformedMessage
		}
		for _, region := range ar.GetRegionHandles() {
			if region == 0 {
				return nil, core.ErrWireMalformedMessage
			}
		}
		attr := core.AttributeHandle(ar.GetAttributeHandle())
		out[attr] = append(out[attr], regionHandlesFromUint64s(ar.GetRegionHandles())...)
	}
	for attr, regions := range out {
		slices.Sort(regions)
		out[attr] = slices.Compact(regions)
	}
	return out, nil
}

// AssociateRegionsForUpdates — §9.6.
func (s *ddmServiceOf[T]) AssociateRegionsForUpdates(
	ctx context.Context,
	req *rtiv1.AssociateRegionsForUpdatesRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("AssociateRegionsForUpdates")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.AttributeRegionGroups != nil {
		groups, err := attributeRegionGroups(req.AttributeRegionGroups, len(req.AttributeRegions) != 0)
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		mgr, ok := s.mgr.(interface {
			AssociateRegionGroupsForUpdates(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeRegionGroup) error
		})
		if !ok {
			return nil, status.Error(codes.Unimplemented, "grouped regional association is unavailable")
		}
		if err := mgr.AssociateRegionGroupsForUpdates(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectHandle(req.GetObjectHandle()), groups); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	bindings, err := attrToRegionsFromPb(req.GetAttributeRegions())
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if err := s.mgr.AssociateRegionsForUpdates(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		bindings,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// UnassociateRegionsForUpdates — §9.7.
func (s *ddmServiceOf[T]) UnassociateRegionsForUpdates(
	ctx context.Context,
	req *rtiv1.UnassociateRegionsForUpdatesRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnassociateRegionsForUpdates")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.AttributeRegionGroups != nil {
		groups, err := attributeRegionGroups(req.AttributeRegionGroups, len(req.AttributeRegions) != 0)
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		mgr, ok := s.mgr.(interface {
			UnassociateRegionGroupsForUpdates(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeRegionGroup) error
		})
		if !ok {
			return nil, status.Error(codes.Unimplemented, "grouped regional unassociation is unavailable")
		}
		if err := mgr.UnassociateRegionGroupsForUpdates(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectHandle(req.GetObjectHandle()), groups); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	bindings, err := attrToRegionsFromPb(req.GetAttributeRegions())
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	if err := s.mgr.UnassociateRegionsForUpdates(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectHandle(req.GetObjectHandle()),
		bindings,
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// UnsubscribeObjectClassAttributesWithRegions — §9.9.
func (s *ddmServiceOf[T]) UnsubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	req *rtiv1.UnsubscribeOCAWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("UnsubscribeObjectClassAttributesWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.AttributeRegionGroups != nil {
		groups, err := attributeRegionGroups(req.AttributeRegionGroups, len(req.AttributeHandles)+len(req.RegionHandles) != 0)
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		mgr, ok := s.mgr.(interface {
			UnsubscribeObjectClassAttributeRegionGroups(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeRegionGroup) error
		})
		if !ok {
			return nil, status.Error(codes.Unimplemented, "atomic grouped regional unsubscription is unavailable")
		}
		if err := mgr.UnsubscribeObjectClassAttributeRegionGroups(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectClassHandle(req.GetObjectClassHandle()), groups); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	if err := s.mgr.UnsubscribeObjectClassAttributesWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrHandles(req.GetAttributeHandles()),
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// UnsubscribeInteractionClassWithRegions — §9.11.
func (s *ddmServiceOf[T]) UnsubscribeInteractionClassWithRegions(
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
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// SendInteractionWithRegions validates and applies the call's region set.
func (s *ddmServiceOf[T]) SendInteractionWithRegions(
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
	ts, err := decodeOptionalScalarTime[T](req.LogicalTime, req.GetLogicalTimeValue())
	if err != nil {
		return nil, err
	}
	if sender, ok := s.objs.(interface {
		SendInteractionWithRegionsMetadata(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, map[core.ParameterHandle][]byte, *T, []core.DDMRegionHandleCore, core.ObjectMessageOptions) error
	}); ok {
		err := sender.SendInteractionWithRegionsMetadata(ctx, core.FederationName(req.FederationName), core.FederateHandle(req.FederateHandle), core.InteractionClassHandle(req.InteractionClassHandle), params, ts, regionHandlesFromUint64s(req.RegionHandles), core.ObjectMessageOptions{Tag: req.UserSuppliedTag, RetractionHandle: req.MessageRetractionHandle})
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		return emptyResponse, nil
	}
	if len(req.UserSuppliedTag) > 0 || req.MessageRetractionHandle != 0 {
		return nil, status.Error(codes.Unimplemented, "regional interaction metadata is unavailable")
	}
	sender, ok := s.objs.(interface {
		SendInteractionWithRegions(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle, map[core.ParameterHandle][]byte, *T, []core.DDMRegionHandleCore) error
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "region-scoped interaction sending is unavailable")
	}
	if err := sender.SendInteractionWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.InteractionClassHandle(req.GetInteractionClassHandle()),
		params,
		ts,
		regionHandlesFromUint64s(req.GetRegionHandles()),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}

// RequestAttributeValueUpdateWithRegions filters before emitting owner callbacks.
func (s *ddmServiceOf[T]) RequestAttributeValueUpdateWithRegions(
	ctx context.Context,
	req *rtiv1.RequestAttributeValueUpdateWithRegionsRequest,
) (*rtiv1.Empty, error) {
	if req == nil {
		return nil, nilRequest("RequestAttributeValueUpdateWithRegions")
	}
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.AttributeRegionGroups != nil {
		groups, err := attributeRegionGroups(req.AttributeRegionGroups, len(req.AttributeHandles)+len(req.RegionHandles) != 0)
		if err != nil {
			return nil, errToStatus(ctx, err)
		}
		requester, ok := s.objs.(interface {
			RequestAttributeValueUpdateWithRegionGroups(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeRegionGroup, []byte) error
		})
		if !ok {
			return nil, status.Error(codes.Unimplemented, "atomic grouped regional update request is unavailable")
		}
		if err := requester.RequestAttributeValueUpdateWithRegionGroups(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectClassHandle(req.GetObjectClassHandle()), groups, req.GetUserSuppliedTag()); err != nil {
			return nil, errToStatus(ctx, err)
		}
		return &rtiv1.Empty{}, nil
	}
	attrs := make([]core.AttributeHandle, 0, len(req.GetAttributeHandles()))
	for _, h := range req.GetAttributeHandles() {
		attrs = append(attrs, core.AttributeHandle(h))
	}
	requester, ok := s.objs.(interface {
		RequestAttributeValueUpdateWithRegions(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle, []core.DDMRegionHandleCore, []byte) error
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "region-scoped update requests are unavailable")
	}
	if err := requester.RequestAttributeValueUpdateWithRegions(
		ctx,
		core.FederationName(req.GetFederationName()),
		core.FederateHandle(req.GetFederateHandle()),
		core.ObjectClassHandle(req.GetObjectClassHandle()),
		attrs,
		regionHandlesFromUint64s(req.GetRegionHandles()),
		req.GetUserSuppliedTag(),
	); err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.Empty{}, nil
}
func (s *ddmServiceOf[T]) registerObjectWithRegionGroups(ctx context.Context, req *rtiv1.RegisterObjectWithRegionsRequest) (*rtiv1.RegisterObjectWithRegionsResponse, error) {
	groups, err := attributeRegionGroups(req.AttributeRegionGroups, len(req.AttributeRegions) != 0)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	registrar, ok := s.objs.(interface {
		RegisterWithRegionGroups(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, string, []core.AttributeRegionGroup) (core.ObjectHandle, string, error)
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "atomic grouped region registration is unavailable")
	}
	obj, name, err := registrar.RegisterWithRegionGroups(ctx, core.FederationName(req.GetFederationName()), core.FederateHandle(req.GetFederateHandle()), core.ObjectClassHandle(req.GetObjectClassHandle()), req.GetObjectName(), groups)
	if err != nil {
		return nil, errToStatus(ctx, err)
	}
	return &rtiv1.RegisterObjectWithRegionsResponse{ObjectHandle: uint64(obj), ObjectName: name}, nil
}
