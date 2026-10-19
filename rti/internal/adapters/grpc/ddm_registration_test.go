package grpc

import (
	"context"
	"errors"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type regionRegistrationRegistry struct {
	core.ObjectRegistry
	calls    int
	bindings map[core.AttributeHandle][]core.DDMRegionHandleCore
	err      error
}

type groupedRegistrationRegistry struct {
	core.ObjectRegistry
	calls  int
	groups []core.AttributeRegionGroup
}

func (r *groupedRegistrationRegistry) RegisterWithRegionGroups(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ string, groups []core.AttributeRegionGroup) (core.ObjectHandle, string, error) {
	r.calls++
	r.groups = groups
	return 45, canonicalRegisteredName, nil
}

func TestDDMGroupedRegistrationPreservesEmptyProducts(t *testing.T) {
	for _, groups := range []*rtiv1.AttributeRegionGroups{
		{}, {Groups: []*rtiv1.AttributeRegionGroup{{AttributeHandles: []uint64{1}}, {RegionHandles: []uint64{7}}}},
	} {
		r := &groupedRegistrationRegistry{}
		req := &rtiv1.RegisterObjectWithRegionsRequest{WireVersion: 1, FederationName: "ddm", FederateHandle: 8, ObjectClassHandle: 3, AttributeRegionGroups: groups}
		response, err := newDDMService(nil, r).RegisterObjectInstanceWithRegions(context.Background(), req)
		if err != nil || response.GetObjectHandle() != 45 || r.calls != 1 || len(r.groups) != len(groups.Groups) {
			t.Fatalf("grouped registration was not forwarded once: %v %v %+v", response, err, r)
		}
		for i, group := range groups.Groups {
			if len(r.groups[i].Attributes) != len(group.AttributeHandles) || len(r.groups[i].Regions) != len(group.RegionHandles) {
				t.Fatal("empty product was flattened away")
			}
		}
		req.AttributeRegions = []*rtiv1.AttributeRegions{{AttributeHandle: 1, RegionHandles: []uint64{7}}}
		if _, err := newDDMService(nil, r).RegisterObjectInstanceWithRegions(context.Background(), req); status.Code(err) != codes.InvalidArgument || r.calls != 1 {
			t.Fatalf("mixed registration reached mutation: %v", err)
		}
	}
	_, err := newDDMService(nil, &regionRegistrationRegistry{}).RegisterObjectInstanceWithRegions(context.Background(), &rtiv1.RegisterObjectWithRegionsRequest{WireVersion: 1, AttributeRegionGroups: &rtiv1.AttributeRegionGroups{}})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("missing grouped capability did not fail closed: %v", err)
	}
}

func (r *regionRegistrationRegistry) RegisterWithRegions(_ context.Context, fed core.FederationName, owner core.FederateHandle, cls core.ObjectClassHandle, name string, bindings map[core.AttributeHandle][]core.DDMRegionHandleCore) (core.ObjectHandle, string, error) {
	if fed != "ddm" || owner != 8 || cls != 3 || name != "target" {
		return 0, "", errors.New("registration identity changed")
	}
	r.calls++
	r.bindings = bindings
	return 45, canonicalRegisteredName, r.err
}

func regionRegistrationRequest() *rtiv1.RegisterObjectWithRegionsRequest {
	return &rtiv1.RegisterObjectWithRegionsRequest{
		WireVersion: 1, FederationName: "ddm", FederateHandle: 8,
		ObjectClassHandle: 3, ObjectName: "target",
		AttributeRegions: []*rtiv1.AttributeRegions{
			{AttributeHandle: 2, RegionHandles: []uint64{7, 5}},
			{AttributeHandle: 2, RegionHandles: []uint64{5, 9}},
			{AttributeHandle: 1, RegionHandles: []uint64{4}},
		},
	}
}

func TestDDMRegistrationUsesSingleObjectTransaction(t *testing.T) {
	r := &regionRegistrationRegistry{}
	s := newDDMService(nil, r)
	got, err := s.RegisterObjectInstanceWithRegions(context.Background(), regionRegistrationRequest())
	if err != nil || got.GetObjectHandle() != 45 || got.GetObjectName() != canonicalRegisteredName || r.calls != 1 {
		t.Fatalf("real registration not forwarded: response=%v calls=%d error=%v", got, r.calls, err)
	}
	want := map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {4}, 2: {5, 7, 9}}
	if !reflect.DeepEqual(r.bindings, want) {
		t.Fatalf("repeated attribute pairs lost regions: got %v want %v", r.bindings, want)
	}
}

func TestDDMRegistrationPropagatesFailure(t *testing.T) {
	r := &regionRegistrationRegistry{err: core.ErrRegionNotOwnedByFederate}
	got, err := newDDMService(nil, r).RegisterObjectInstanceWithRegions(context.Background(), regionRegistrationRequest())
	if got != nil || status.Code(err) != codes.PermissionDenied || r.calls != 1 {
		t.Fatalf("registration failure hidden: response=%v calls=%d error=%v", got, r.calls, err)
	}
}

func TestDDMRegistrationRejectsMalformedPairs(t *testing.T) {
	for _, pair := range []*rtiv1.AttributeRegions{nil, {AttributeHandle: 0, RegionHandles: []uint64{1}}, {AttributeHandle: 1, RegionHandles: []uint64{0}}, {AttributeHandle: 1}} {
		r := &regionRegistrationRegistry{}
		req := regionRegistrationRequest()
		req.AttributeRegions = append(req.AttributeRegions, pair)
		got, err := newDDMService(nil, r).RegisterObjectInstanceWithRegions(context.Background(), req)
		if got != nil || status.Code(err) != codes.InvalidArgument || r.calls != 0 {
			t.Fatalf("malformed pair %v accepted: response=%v calls=%d error=%v", pair, got, r.calls, err)
		}
	}
}

func TestDDMRegistrationWithoutCapabilityFailsClosed(t *testing.T) {
	got, err := newDDMService(nil, nil).RegisterObjectInstanceWithRegions(context.Background(), regionRegistrationRequest())
	if got != nil || status.Code(err) != codes.Unimplemented {
		t.Fatalf("unavailable transaction returned success: response=%v error=%v", got, err)
	}
}

func TestDDMValidationErrorIdentity(t *testing.T) {
	for sentinel, want := range map[error]string{core.ErrInvalidRangeBounds: "InvalidRangeBound", core.ErrInvalidRegionContext: "InvalidRegionContext"} {
		name, ok := specExceptionName(sentinel)
		if !ok || name != want || status.Code(errToStatus(context.Background(), sentinel)) != codes.InvalidArgument {
			t.Fatalf("error identity: %v -> %q %v", sentinel, name, ok)
		}
	}
}

const (
	canonicalRegisteredName = "canonical"
)
