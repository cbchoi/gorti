// TASK-261..263 (M23 W4) — Go federate SDK §9 Data Distribution Management.
//
// Mirrors pysdk/rti1516e/ddm.py 1:1 onto rti.v1.DDMService. Pre-M23
// the Go SDK had zero DDM coverage — Python federates could use the
// M10 DDM wire surface end-to-end but Go federates couldn't.
//
// Region handles, dimension handles, routing-space handles all
// surface as uint64 — same minimal contract pysdk uses, no opaque
// struct.

package federate

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// AttributeRegions binds an attribute handle to a set of region
// handles for region-scoped pub/sub (IEEE 1516.1 §9.5 / §9.6).
type AttributeRegions struct {
	AttributeHandle uint64
	RegionHandles   []uint64
}

// LookupRoutingSpace returns the routing-space handle for name, or 0
// if the FOM does not declare a routing space with this name.
func (f *Federate) LookupRoutingSpace(ctx context.Context, name string) (uint64, error) {
	resp, err := f.conn.ddm.LookupRoutingSpace(ctx, &rtiv1.LookupRoutingSpaceRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		Name:           name,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	if !resp.GetFound() {
		return 0, nil
	}
	return resp.GetRoutingSpaceHandle(), nil
}

// LookupDimension returns the dimension handle within space for name,
// or 0 if not found.
func (f *Federate) LookupDimension(ctx context.Context, space uint64, name string) (uint64, error) {
	resp, err := f.conn.ddm.LookupDimension(ctx, &rtiv1.LookupDimensionRequest{
		WireVersion:        rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:     f.federationName,
		RoutingSpaceHandle: space,
		Name:               name,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	if !resp.GetFound() {
		return 0, nil
	}
	return resp.GetDimensionHandle(), nil
}

// CreateRegion creates a region in space spanning the given dimension
// set; returns the new region handle. IEEE 1516.1 §9.2.
func (f *Federate) CreateRegion(ctx context.Context, space uint64, dimensions []uint64) (uint64, error) {
	resp, err := f.conn.ddm.CreateRegion(ctx, &rtiv1.CreateRegionRequest{
		WireVersion:        rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:     f.federationName,
		FederateHandle:     f.federateHandle,
		RoutingSpaceHandle: space,
		DimensionHandles:   dimensions,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return resp.GetRegionHandle(), nil
}

// SetRangeBounds stages a pending range update on the (region, dim)
// pair. Bounds are not committed until CommitRegionModifications.
func (f *Federate) SetRangeBounds(ctx context.Context, region, dimension uint64, lower, upper uint64) error {
	_, err := f.conn.ddm.SetRangeBounds(ctx, &rtiv1.SetRangeBoundsRequest{
		WireVersion:     rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:  f.federationName,
		FederateHandle:  f.federateHandle,
		RegionHandle:    region,
		DimensionHandle: dimension,
		Bounds:          &rtiv1.Range{Lower: lower, Upper: upper},
	})
	return wrapStatusErr(err)
}

// CommitRegionModifications atomically commits pending bounds across
// all the supplied regions. IEEE 1516.1 §9.3.
func (f *Federate) CommitRegionModifications(ctx context.Context, regions []uint64) error {
	_, err := f.conn.ddm.CommitRegionModifications(ctx, &rtiv1.CommitRegionRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		RegionHandles:  regions,
	})
	return wrapStatusErr(err)
}

// DeleteRegion deletes a region. Pending publishers/subscribers detach.
// IEEE 1516.1 §9.4.
func (f *Federate) DeleteRegion(ctx context.Context, region uint64) error {
	_, err := f.conn.ddm.DeleteRegion(ctx, &rtiv1.DeleteRegionRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		RegionHandle:   region,
	})
	return wrapStatusErr(err)
}

// QueryBounds returns the committed (lower, upper) bounds for the
// (region, dimension) pair. found=false when the pair has no bounds.
// IEEE 1516.1 §9.14 (getRangeBounds).
func (f *Federate) QueryBounds(ctx context.Context, region, dimension uint64) (lower, upper uint64, found bool, err error) {
	resp, rerr := f.conn.ddm.QueryBounds(ctx, &rtiv1.QueryBoundsRequest{
		WireVersion:     rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:  f.federationName,
		RegionHandle:    region,
		DimensionHandle: dimension,
	})
	if rerr != nil {
		return 0, 0, false, wrapStatusErr(rerr)
	}
	if !resp.GetFound() {
		return 0, 0, false, nil
	}
	b := resp.GetBounds()
	return b.GetLower(), b.GetUpper(), true, nil
}

// SubscribeObjectClassAttributesWithRegions — IEEE 1516.1 §9.8.
// Region-scoped subscription to object-class attributes.
func (f *Federate) SubscribeObjectClassAttributesWithRegions(
	ctx context.Context,
	cls uint64,
	attributes, regions []uint64,
) error {
	_, err := f.conn.ddm.SubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.SubscribeOCAWithRegionsRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: cls,
		AttributeHandles:  attributes,
		RegionHandles:     regions,
	})
	return wrapStatusErr(err)
}

// SubscribeInteractionClassWithRegions — IEEE 1516.1 §9.10.
func (f *Federate) SubscribeInteractionClassWithRegions(
	ctx context.Context,
	cls uint64,
	regions []uint64,
) error {
	_, err := f.conn.ddm.SubscribeInteractionClassWithRegions(ctx, &rtiv1.SubscribeICWithRegionsRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: cls,
		RegionHandles:          regions,
	})
	return wrapStatusErr(err)
}

// RegisterObjectInstanceWithRegions — IEEE 1516.1 §9.5.
//
// Two-step pattern (matches pysdk): mints the object handle via
// ObjectService.RegisterObjectInstance, then forwards the per-attribute
// region bindings to DDMService.RegisterObjectInstanceWithRegions.
// Returns the minted object handle.
func (f *Federate) RegisterObjectInstanceWithRegions(
	ctx context.Context,
	cls uint64,
	bindings []AttributeRegions,
	objectName string,
) (uint64, error) {
	regResp, err := f.conn.obj.RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: cls,
		ObjectName:        objectName,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	objHandle := regResp.GetObjectHandle()
	actualName := regResp.GetObjectName()
	if actualName == "" {
		actualName = objectName
	}

	pbBindings := make([]*rtiv1.AttributeRegions, 0, len(bindings))
	for _, b := range bindings {
		pbBindings = append(pbBindings, &rtiv1.AttributeRegions{
			AttributeHandle: b.AttributeHandle,
			RegionHandles:   append([]uint64(nil), b.RegionHandles...),
		})
	}

	_, err = f.conn.ddm.RegisterObjectInstanceWithRegions(ctx, &rtiv1.RegisterObjectWithRegionsRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: cls,
		ObjectName:        actualName,
		AttributeRegions:  pbBindings,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	return objHandle, nil
}

// --- M23 W5 — §9 missing services ----------------------------------

func (f *Federate) attrRegionsToWire(bindings []AttributeRegions) []*rtiv1.AttributeRegions {
	out := make([]*rtiv1.AttributeRegions, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, &rtiv1.AttributeRegions{
			AttributeHandle: b.AttributeHandle,
			RegionHandles:   append([]uint64(nil), b.RegionHandles...),
		})
	}
	return out
}

// AssociateRegionsForUpdates — IEEE 1516.1-2010 §9.6 (M23 W5).
func (f *Federate) AssociateRegionsForUpdates(ctx context.Context, obj uint64, bindings []AttributeRegions) error {
	_, err := f.conn.ddm.AssociateRegionsForUpdates(ctx, &rtiv1.AssociateRegionsForUpdatesRequest{
		WireVersion:      rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:   f.federationName,
		FederateHandle:   f.federateHandle,
		ObjectHandle:     obj,
		AttributeRegions: f.attrRegionsToWire(bindings),
	})
	return wrapStatusErr(err)
}

// UnassociateRegionsForUpdates — IEEE 1516.1-2010 §9.7 (M23 W5).
// Empty bindings drops ALL associations for the object.
func (f *Federate) UnassociateRegionsForUpdates(ctx context.Context, obj uint64, bindings []AttributeRegions) error {
	_, err := f.conn.ddm.UnassociateRegionsForUpdates(ctx, &rtiv1.UnassociateRegionsForUpdatesRequest{
		WireVersion:      rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:   f.federationName,
		FederateHandle:   f.federateHandle,
		ObjectHandle:     obj,
		AttributeRegions: f.attrRegionsToWire(bindings),
	})
	return wrapStatusErr(err)
}

// UnsubscribeObjectClassAttributesWithRegions — IEEE 1516.1-2010 §9.9 (M23 W5).
func (f *Federate) UnsubscribeObjectClassAttributesWithRegions(
	ctx context.Context, cls uint64, attributes, regions []uint64,
) error {
	_, err := f.conn.ddm.UnsubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.UnsubscribeOCAWithRegionsRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: cls,
		AttributeHandles:  attributes,
		RegionHandles:     regions,
	})
	return wrapStatusErr(err)
}

// UnsubscribeInteractionClassWithRegions — IEEE 1516.1-2010 §9.11 (M23 W5).
func (f *Federate) UnsubscribeInteractionClassWithRegions(ctx context.Context, cls uint64, regions []uint64) error {
	_, err := f.conn.ddm.UnsubscribeInteractionClassWithRegions(ctx, &rtiv1.UnsubscribeICWithRegionsRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: cls,
		RegionHandles:          regions,
	})
	return wrapStatusErr(err)
}

// SendInteractionWithRegions — IEEE 1516.1-2010 §9.12 (M23 W5).
// M23 simplification: regions are recorded on the wire but the existing
// DDM filter (M10) applies via object.Registry; per-call region filter
// is a follow-up.
func (f *Federate) SendInteractionWithRegions(
	ctx context.Context,
	cls uint64,
	parameters map[uint64][]byte,
	regions []uint64,
	timestamp *float64,
) error {
	req := &rtiv1.SendInteractionWithRegionsRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: cls,
		Parameters:             parameters,
		RegionHandles:          regions,
	}
	if timestamp != nil {
		v := *timestamp
		req.LogicalTime = &v
	}
	_, err := f.conn.ddm.SendInteractionWithRegions(ctx, req)
	return wrapStatusErr(err)
}

// RequestAttributeValueUpdateWithRegions — IEEE 1516.1-2010 §9.13 (M23 W5).
func (f *Federate) RequestAttributeValueUpdateWithRegions(
	ctx context.Context,
	cls uint64,
	attributes, regions []uint64,
	tag []byte,
) error {
	_, err := f.conn.ddm.RequestAttributeValueUpdateWithRegions(ctx, &rtiv1.RequestAttributeValueUpdateWithRegionsRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: cls,
		AttributeHandles:  attributes,
		RegionHandles:     regions,
		UserSuppliedTag:   append([]byte(nil), tag...),
	})
	return wrapStatusErr(err)
}
