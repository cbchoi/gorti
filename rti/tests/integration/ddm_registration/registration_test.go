package ddm_registration_test

import (
	"bytes"
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (f *federation) objectCount() uint32 {
	f.t.Helper()
	s, err := f.admin.Snapshot(f.ctx, &rtiv1.SnapshotRequest{WireVersion: 1, FederationName: f.name})
	check(f.t, err)
	if len(s.Federations) != 1 {
		f.t.Fatalf("expected one federation: %v", s)
	}
	return s.Federations[0].ObjectInstanceCount
}

func (f *federation) setRange(owner int, region, lower, upper uint64, opts ...grpc.CallOption) error {
	_, err := f.ddm.SetRangeBounds(f.ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[owner], RegionHandle: region, DimensionHandle: f.dimension, Bounds: &rtiv1.Range{Lower: lower, Upper: upper}}, opts...)
	return err
}

func (f *federation) commit(owner int, region uint64) {
	f.t.Helper()
	_, err := f.ddm.CommitRegionModifications(f.ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[owner], RegionHandles: []uint64{region}})
	check(f.t, err)
}

func (f *federation) region(owner int, lower, upper uint64) uint64 {
	f.t.Helper()
	r, err := f.ddm.CreateRegion(f.ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[owner], RoutingSpaceHandle: f.space, DimensionHandles: []uint64{f.dimension}})
	check(f.t, err)
	if r.RegionHandle == 0 {
		f.t.Fatal("CreateRegion returned zero handle")
	}
	f.regionOwners[r.RegionHandle] = owner
	check(f.t, f.setRange(owner, r.RegionHandle, lower, upper))
	f.commit(owner, r.RegionHandle)
	f.bounds(r.RegionHandle, lower, upper)
	return r.RegionHandle
}

func (f *federation) bounds(region, lower, upper uint64) {
	f.t.Helper()
	r, err := f.ddm.QueryBounds(f.regionQueryContext(region), &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: f.name,
		RegionHandle: region, DimensionHandle: f.dimension})
	check(f.t, err)
	if !r.Found || r.Bounds == nil || r.Bounds.Lower != lower || r.Bounds.Upper != upper {
		f.t.Errorf("committed range = %v, want [%d,%d]", r, lower, upper)
	}
}

func (f *federation) regionQueryContext(region uint64) context.Context {
	f.t.Helper()
	owner, found := f.regionOwners[region]
	if !found {
		f.t.Fatalf("missing region creator for authenticated query: %d", region)
	}
	return f.queryContexts[owner]
}

func (f *federation) subscribe(who int, regions ...uint64) {
	f.t.Helper()
	attrs := []uint64{f.position, f.velocity}
	if len(regions) == 0 {
		_, err := f.decl.SubscribeObjectClassAttributes(f.ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: f.name,
			FederateHandle: f.handles[who], ObjectClassHandle: f.class, AttributeHandles: attrs})
		check(f.t, err)
		return
	}
	_, err := f.ddm.SubscribeObjectClassAttributesWithRegions(f.ctx, &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[who], ObjectClassHandle: f.class, AttributeHandles: attrs, RegionHandles: regions})
	check(f.t, err)
}

func (f *federation) register(name string, pairs ...*rtiv1.AttributeRegions) (*rtiv1.RegisterObjectWithRegionsResponse, error) {
	return f.ddm.RegisterObjectInstanceWithRegions(f.ctx, &rtiv1.RegisterObjectWithRegionsRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], ObjectClassHandle: f.class, ObjectName: name, AttributeRegions: pairs})
}

func (f *federation) reserve(name string) {
	f.t.Helper()
	_, err := f.objects.ReserveObjectInstanceName(f.ctx, &rtiv1.ReserveObjectInstanceNameRequest{
		WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectName: name,
	})
	check(f.t, err)
	f.callbacks[0].wait(f.t, "name reservation "+name, func(event *rtiv1.FederateEvent) bool {
		return event.GetReservationSucceeded().GetObjectName() == name
	})
}

func (f *federation) lookup(name string, handle uint64) {
	f.t.Helper()
	r, err := f.support.GetObjectInstanceHandle(f.ctx, &rtiv1.GetObjectInstanceHandleRequest{WireVersion: 1, FederationName: f.name, ObjectName: name})
	if handle == 0 {
		if status.Code(err) != codes.NotFound {
			f.t.Errorf("failed registration left name %q: response=%v error=%v", name, r, err)
		}
		return
	}
	if err != nil || r.GetObjectHandle() != handle {
		f.t.Errorf("name lookup = %v, %v; want handle %d", r, err, handle)
	}
	reverse, err := f.support.GetObjectInstanceName(f.ctx, &rtiv1.GetObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, ObjectHandle: handle})
	if err != nil || reverse.GetObjectName() != name {
		f.t.Errorf("handle lookup = %v, %v; want name %q", reverse, err, name)
	}
}

func (f *federation) identity(r *rtiv1.RegisterObjectWithRegionsResponse, name string, before uint32) uint64 {
	f.t.Helper()
	if got := f.objectCount(); got != before+1 {
		f.t.Errorf("real object count = %d, want %d (exactly one registration)", got, before+1)
	}
	if r.GetObjectHandle() == 0 {
		f.t.Fatalf("RegisterObjectInstanceWithRegions returned zero object handle: %v", r)
	}
	if r.ObjectName != name {
		f.t.Errorf("registration name = %q, want %q", r.ObjectName, name)
	}
	f.lookup(name, r.ObjectHandle)
	return r.ObjectHandle
}

func (f *federation) expectCallbacks(who int, handle uint64, discoveries, reflections int, value []byte) {
	f.t.Helper()
	var gotDiscover, gotReflect int
	for _, e := range f.callbacks[who].snapshot() {
		if d := e.GetDiscover(); d != nil {
			gotDiscover++
			if d.ObjectHandle != handle || d.ObjectClassHandle != f.class {
				f.t.Errorf("subscriber %d unexpected discovery: %v", who, d)
			}
		}
		if r := e.GetReflect(); r != nil {
			gotReflect++
			if gotDiscover == 0 {
				f.t.Errorf("subscriber %d reflection preceded discovery", who)
			}
			if r.ObjectHandle != handle || !bytes.Equal(r.Attributes[f.position], value) {
				f.t.Errorf("subscriber %d unexpected reflection: %v", who, r)
			}
		}
	}
	if gotDiscover != discoveries || gotReflect != reflections {
		f.t.Errorf("subscriber %d callbacks: Discover=%d Reflect=%d, want Discover=%d Reflect=%d", who, gotDiscover, gotReflect, discoveries, reflections)
	}
}

func expectRPCError(t *testing.T, err error, trailer metadata.MD, code codes.Code, exception string) {
	t.Helper()
	if status.Code(err) != code {
		t.Errorf("RPC status = %v (%v), want %v", status.Code(err), err, code)
	}
	if values := trailer.Get("rti-spec-exception"); len(values) != 1 || values[0] != exception {
		t.Errorf("RPC exception trailer = %v, want [%s]", values, exception)
	}
}

func (f *federation) deleteObjectAndRegion(handle, region uint64, name string, before uint32) {
	f.t.Helper()
	_, err := f.objects.DeleteObjectInstance(f.ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], ObjectHandle: handle})
	check(f.t, err)
	f.barrier()
	f.lookup(name, 0)
	if got := f.objectCount(); got != before {
		f.t.Errorf("post-delete object count = %d, want %d", got, before)
	}
	_, err = f.ddm.DeleteRegion(f.ctx, &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], RegionHandle: region})
	if err != nil {
		f.t.Fatalf("producer region remained in use after object deletion: %v", err)
	}
	var trailer metadata.MD
	r, err := f.ddm.QueryBounds(f.regionQueryContext(region), &rtiv1.QueryBoundsRequest{WireVersion: 1, FederationName: f.name,
		RegionHandle: region, DimensionHandle: f.dimension}, grpc.Trailer(&trailer))
	expectRPCError(f.t, err, trailer, codes.NotFound, "InvalidRegion")
	if r != nil {
		f.t.Errorf("deleted producer region remains addressable: %v", r)
	}
}

func TestProductionRegisterObjectInstanceWithRegionsRouting(t *testing.T) {
	f := startFederation(t)
	publisher := f.region(0, 10, 20)
	near := f.region(1, 15, 25)
	far := f.region(2, 100, 200)
	if publisher == near || publisher == far || near == far {
		t.Fatal("region handles must be distinct")
	}
	f.subscribe(1, near)
	f.subscribe(2, far)
	f.subscribe(3)
	f.barrier()
	before := f.objectCount()
	f.reserve("regional-object")
	r, err := f.register("regional-object", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{publisher}},
		&rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{publisher}})
	check(t, err)
	handle := f.identity(r, "regional-object", before)
	f.barrier()
	f.expectCallbacks(0, handle, 0, 0, nil)
	f.expectCallbacks(1, handle, 1, 0, nil)
	f.expectCallbacks(2, handle, 0, 0, nil)
	f.expectCallbacks(3, handle, 1, 0, nil)
	value := []byte{0x40, 0x24, 0, 0, 0, 0, 0, 0}
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], ObjectHandle: handle, Attributes: map[uint64][]byte{f.position: value}})
	check(t, err)
	f.barrier()
	f.expectCallbacks(0, handle, 0, 0, value)
	f.expectCallbacks(1, handle, 1, 1, value)
	f.expectCallbacks(2, handle, 0, 0, value)
	f.expectCallbacks(3, handle, 1, 1, value)
	if got := f.objectCount(); got != before+1 {
		t.Errorf("update changed object count to %d, want %d", got, before+1)
	}
	f.deleteObjectAndRegion(handle, publisher, "regional-object", before)
}

func TestProductionRegisterObjectInstanceWithRegionsFailureAtomicity(t *testing.T) {
	for _, scenario := range []string{"invalid_later_region_pair", "foreign_region", "invalid_attribute", "dimension_context_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			f := startFederation(t)
			owned := f.region(0, 10, 20)
			foreign := f.region(1, 15, 25)
			f.subscribe(1, foreign)
			f.subscribe(2, f.region(2, 100, 200))
			f.subscribe(3)
			f.barrier()
			before := f.objectCount()
			first := &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{owned}}
			later := &rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{owned}}
			wantCode, wantException := codes.NotFound, "InvalidRegion"
			switch scenario {
			case "invalid_later_region_pair":
				later.RegionHandles = []uint64{owned, ^uint64(0)}
			case "foreign_region":
				later.RegionHandles = []uint64{foreign}
				wantCode, wantException = codes.PermissionDenied, "RegionNotCreatedByThisFederate"
			case "invalid_attribute":
				later.AttributeHandle = ^uint64(0)
				wantException = "AttributeNotDefined"
			case "dimension_context_mismatch":
				dim, err := f.ddm.LookupDimension(f.ctx, &rtiv1.LookupDimensionRequest{WireVersion: 1,
					FederationName: f.name, RoutingSpaceHandle: f.space, Name: "Y"})
				check(t, err)
				if !dim.Found || dim.DimensionHandle == 0 || dim.DimensionHandle == f.dimension {
					t.Fatalf("expected distinct declared Y dimension: %v", dim)
				}
				region, err := f.ddm.CreateRegion(f.ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: f.name,
					FederateHandle: f.handles[0], RoutingSpaceHandle: f.space, DimensionHandles: []uint64{dim.DimensionHandle}})
				check(t, err)
				if region.RegionHandle == 0 {
					t.Fatal("Y region returned zero handle")
				}
				_, err = f.ddm.SetRangeBounds(f.ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], RegionHandle: region.RegionHandle, DimensionHandle: dim.DimensionHandle, Bounds: &rtiv1.Range{Upper: 1000}})
				check(t, err)
				f.commit(0, region.RegionHandle)
				later.RegionHandles = []uint64{region.RegionHandle}
				wantCode, wantException = codes.InvalidArgument, "InvalidRegionContext"
			}
			name := "retry-" + scenario
			f.reserve(name)
			var trailer metadata.MD
			r, err := f.ddm.RegisterObjectInstanceWithRegions(f.ctx, &rtiv1.RegisterObjectWithRegionsRequest{WireVersion: 1,
				FederationName: f.name, FederateHandle: f.handles[0], ObjectClassHandle: f.class, ObjectName: name,
				AttributeRegions: []*rtiv1.AttributeRegions{first, later}}, grpc.Trailer(&trailer))
			if err == nil {
				t.Errorf("invalid registration accepted: response=%v", r)
			}
			expectRPCError(t, err, trailer, wantCode, wantException)
			f.barrier()
			if got := f.objectCount(); got != before {
				t.Errorf("failed registration leaked object: count=%d, want %d", got, before)
			}
			f.lookup(name, 0)
			for who := range f.handles {
				f.expectCallbacks(who, 0, 0, 0, nil)
			}
			// Retry the same name without reservation repair or cleanup.
			r, err = f.register(name, first, &rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{owned}})
			check(t, err)
			handle := f.identity(r, name, before)
			f.barrier()
			f.expectCallbacks(0, handle, 0, 0, nil)
			f.expectCallbacks(1, handle, 1, 0, nil)
			f.expectCallbacks(2, handle, 0, 0, nil)
			f.expectCallbacks(3, handle, 1, 0, nil)
			f.deleteObjectAndRegion(handle, owned, name, before)
		})
	}
}

func TestProductionSetRangeBoundsRejectsInvalidPreservesPending(t *testing.T) {
	for _, tc := range []struct {
		name         string
		lower, upper uint64
	}{
		{"inverted", 40, 30}, {"over_limit", 30, 1001},
		{"equal_zero", 0, 0}, {"equal_middle", 30, 30},
		{"equal_at_limit", 1000, 1000}, {"uint64_maximum", 0, ^uint64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := startFederation(t)
			region := f.region(0, 10, 20)
			check(t, f.setRange(0, region, 30, 40))
			f.bounds(region, 10, 20)
			var trailer metadata.MD
			err := f.setRange(0, region, tc.lower, tc.upper, grpc.Trailer(&trailer))
			if err == nil {
				t.Errorf("SetRangeBounds accepted invalid range [%d,%d]", tc.lower, tc.upper)
			}
			expectRPCError(t, err, trailer, codes.InvalidArgument, "InvalidRangeBound")
			f.bounds(region, 10, 20)
			f.commit(0, region)
			f.bounds(region, 30, 40)
			check(t, f.setRange(0, region, 990, 1000))
			f.commit(0, region)
			f.bounds(region, 990, 1000)
		})
	}
}
