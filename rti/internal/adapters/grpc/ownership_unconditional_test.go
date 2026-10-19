package grpc_test

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func unconditionalOwnershipOptions(opts *ownership.Options, f *ieeeOODDMFixture) {
	opts.PrepareDivestiture = func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
		return f.objects.PrepareOwnershipReleaseDenied(ctx, fed, h, obj, attrs)
	}
	opts.Subscribers = ownership.NewPublisherResolver(func(fed core.FederationName, obj core.ObjectHandle) core.ObjectClassHandle {
		return f.objects.ClassOf(fed, obj)
	}, f.decl, f.foms)
}

func TestIEEE2010UnconditionalDivestProgressionRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t, unconditionalOwnershipOptions)
	for _, attr := range []core.AttributeHandle{f.value, f.other} {
		ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{attr}, false, []byte("same-tag-separate-requests")))
	}
	f.drain(f.owner)
	request := &rtiv1.UnconditionalDivestRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value), 999999}}
	before := f.owners.ExportState(ieeeOODDMFederation)
	var trailer metadata.MD
	_, err := f.ownClient.UnconditionalAttributeOwnershipDivestiture(f.ctx, request, grpcgo.Trailer(&trailer))
	ieeeOODDMException(t, err, trailer, "AttributeNotDefined")
	if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) || len(f.drain(f.acquirer)) != 0 {
		t.Fatal("invalid mixed set progressed acquisitions")
	}
	request.AttributeHandles = []uint64{uint64(f.value), uint64(f.other)}
	_, err = f.ownClient.UnconditionalAttributeOwnershipDivestiture(f.ctx, request)
	ieeeOODDMOK(t, err)
	if callbacks := f.drain(f.owner); len(callbacks) != 0 {
		t.Fatalf("former owner received a callback after divestiture: %v", callbacks)
	}
	callbacks := f.drain(f.acquirer)
	if len(callbacks) != 2 {
		t.Fatalf("want two separated notifications, got %v", callbacks)
	}
	seen := map[core.AttributeHandle]bool{}
	for _, callback := range callbacks {
		e, ok := callback.(*ownership.CallbackEvent)
		if !ok || e.Kind != ownership.CallbackAcquisitionNotification || len(e.Attributes) != 1 || len(e.Tag) != 0 {
			t.Fatalf("incorrect acquisition callback: %+v", callback)
		}
		seen[e.Attributes[0]] = true
	}
	if !seen[f.value] || !seen[f.other] || f.owners.Snapshot(ieeeOODDMFederation).PendingAcquiresCount != 0 {
		t.Fatal("not all waiting acquisitions completed")
	}
	for _, attr := range []core.AttributeHandle{f.value, f.other} {
		response, err := f.ownClient.QueryAttributeOwnership(f.ctx, &rtiv1.QueryOwnershipRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandle: uint64(attr)})
		ieeeOODDMOK(t, err)
		if !response.Owned || response.OwnerFederateHandle != uint64(f.acquirer) {
			t.Fatal(response)
		}
	}
}

func TestIEEE2010AssumptionFindsKnownPublisherAfterUnsubscribeRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t, unconditionalOwnershipOptions)
	// The acquirer retains object knowledge but publishes only value and has
	// stopped subscribing. The stranger subscribes but publishes no attributes.
	ieeeOODDMOK(t, f.decl.UnpublishObjectClassAttributes(f.ctx, ieeeOODDMFederation, f.acquirer, f.cls, []core.AttributeHandle{f.other}))
	ieeeOODDMOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, ieeeOODDMFederation, f.acquirer, f.cls, []core.AttributeHandle{f.value, f.other}))
	ieeeOODDMOK(t, f.decl.SubscribeObjectClassAttributes(f.ctx, ieeeOODDMFederation, f.stranger, f.cls, []core.AttributeHandle{f.value, f.other}))
	f.drain(f.stranger)
	_, err := f.ownClient.UnconditionalAttributeOwnershipDivestiture(f.ctx, &rtiv1.UnconditionalDivestRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value), uint64(f.other)}})
	ieeeOODDMOK(t, err)
	if len(f.drain(f.owner)) != 0 || len(f.drain(f.stranger)) != 0 {
		t.Fatal("divester or nonpublisher received an assumption")
	}
	callbacks := f.drain(f.acquirer)
	if len(callbacks) != 1 {
		t.Fatalf("unsubscribed eligible publisher was not solicited: %v", callbacks)
	}
	e, ok := callbacks[0].(*ownership.CallbackEvent)
	if !ok || e.Kind != ownership.CallbackAssumption || !reflect.DeepEqual(e.Attributes, []core.AttributeHandle{f.value}) || len(e.Tag) != 0 {
		t.Fatalf("assumption eligibility/tag=%+v", callbacks[0])
	}
	ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.value}, true, nil))
	if !f.owners.IsOwnedBy(ieeeOODDMFederation, f.acquirer, f.obj, f.value) {
		t.Fatal("solicited publisher could not acquire")
	}
	if _, owned := f.owners.QueryOwnership(ieeeOODDMFederation, f.obj, f.other); owned {
		t.Fatal("attribute with no eligible acquirer did not stay unowned")
	}
}
