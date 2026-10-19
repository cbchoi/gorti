package grpc_test

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestTransportationAuthenticatedActivationChangesActualReflectionsOnlyAtEntry(t *testing.T) {
	f := newIEEETransportationFixture(t)
	request := &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT}
	if _, err := f.objClient.PrepareAttributeTransportationTypeChange(f.ctx, request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing auth=%v", err)
	}
	if _, err := f.objClient.PrepareAttributeTransportationTypeChange(f.peerContext, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong auth=%v", err)
	}
	prepared, err := f.objClient.PrepareAttributeTransportationTypeChange(f.ownerContext, request)
	ieeeOODDMOK(t, err)
	var trailer metadata.MD
	_, err = f.objClient.PrepareAttributeTransportationTypeChange(f.ownerContext, request, grpcgo.Trailer(&trailer))
	ieeeOODDMException(t, err, trailer, "AttributeAlreadyBeingChanged")
	checkUpdate := func(want core.TransportType) {
		t.Helper()
		f.drain(f.acquirer)
		ieeeOODDMOK(t, f.objects.UpdateAttributesWithMetadata(f.ctx, ieeeOODDMFederation, f.owner, f.obj, map[core.AttributeHandle][]byte{f.value: {1}}, nil, core.ObjectMessageOptions{}))
		for {
			select {
			case batch := <-f.queues[f.acquirer]:
				for _, e := range batch {
					if reflect, ok := e.(*object.CallbackEvent); ok && reflect.Kind == object.CallbackReflect {
						if reflect.Metadata.Transportation != want {
							t.Fatalf("reflection transportation=%v want=%v", reflect.Metadata.Transportation, want)
						}
						return
					}
				}
			case <-f.ctx.Done():
				t.Fatal("missing actual reflection")
			}
		}
	}
	checkUpdate(core.TransportTypeReliable)
	activation := &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), Ticket: prepared.Ticket, ExpectedFederationGeneration: prepared.FederationGeneration, ExpectedCallbackEpoch: prepared.CallbackEpoch}
	if _, err := f.objClient.ActivateTransportationTypeChange(f.peerContext, activation); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign activation=%v", err)
	}
	if _, err := f.objClient.ActivateTransportationTypeChange(f.ctx, activation); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated activation=%v", err)
	}
	other := proto.Clone(activation).(*rtiv1.ActivateTransportationTypeChangeRequest)
	other.FederateHandle = uint64(f.acquirer)
	if _, err := f.objClient.ActivateTransportationTypeChange(f.peerContext, other); err == nil {
		t.Fatal("ticket usable by another authenticated member")
	}
	checkUpdate(core.TransportTypeReliable)
	activated, err := f.objClient.ActivateTransportationTypeChange(f.ownerContext, activation)
	ieeeOODDMOK(t, err)
	if !proto.Equal(activated, prepared) {
		t.Fatalf("activation payload differs: %v", activated)
	}
	checkUpdate(core.TransportTypeBestEffort)
	// Legacy immediate calls remain immediate, and old activation replay must not overwrite them.
	request.TransportType = rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
	_, err = f.objClient.RequestAttributeTransportationTypeChange(f.ctx, request)
	ieeeOODDMOK(t, err)
	_, err = f.objClient.ActivateTransportationTypeChange(f.ownerContext, activation)
	ieeeOODDMOK(t, err)
	checkUpdate(core.TransportTypeReliable)
}

func TestTransportationActivationRejectsChangedEpochAndRetiredRestoreTicket(t *testing.T) {
	for _, retirement := range []string{"epoch", "generation", "restore"} {
		t.Run(retirement, func(t *testing.T) {
			f := newIEEETransportationFixture(t)
			prepared, err := f.objClient.PrepareAttributeTransportationTypeChange(f.ownerContext, &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT})
			ieeeOODDMOK(t, err)
			activation := &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), Ticket: prepared.Ticket, ExpectedFederationGeneration: prepared.FederationGeneration, ExpectedCallbackEpoch: prepared.CallbackEpoch}
			switch retirement {
			case "epoch":
				f.epochs.epoch.Add(1)
			case "generation":
				activation.ExpectedFederationGeneration++
			case "restore":
				commit, err := f.objects.PrepareImportState(ieeeOODDMFederation, f.objects.ExportState(ieeeOODDMFederation))
				ieeeOODDMOK(t, err)
				commit()
			}
			var trailer metadata.MD
			_, err = f.objClient.ActivateTransportationTypeChange(f.ownerContext, activation, grpcgo.Trailer(&trailer))
			ieeeOODDMException(t, err, trailer, "RTIinternalError")
			if got := f.objects.AttributeTransportType(ieeeOODDMFederation, f.obj, f.value); got != core.TransportTypeUnspecified {
				t.Fatalf("stale activation changed override: %v", got)
			}
		})
	}
}

func TestTransportationOwnershipLossReturnsEmptyConfirmationWithoutReactivatingReacquiredAttribute(t *testing.T) {
	f := newIEEETransportationFixture(t)
	prepared, err := f.objClient.PrepareAttributeTransportationTypeChange(f.ownerContext, &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT})
	ieeeOODDMOK(t, err)
	ieeeOODDMOK(t, f.owners.UnconditionalDivest(f.ctx, ieeeOODDMFederation, f.owner, f.obj, []core.AttributeHandle{f.value}))
	ieeeOODDMOK(t, f.owners.AcquireIfAvailable(f.ctx, ieeeOODDMFederation, f.owner, f.obj, []core.AttributeHandle{f.value}, nil))
	result, err := f.objClient.ActivateTransportationTypeChange(f.ownerContext, &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), Ticket: prepared.Ticket, ExpectedFederationGeneration: prepared.FederationGeneration, ExpectedCallbackEpoch: prepared.CallbackEpoch})
	ieeeOODDMOK(t, err)
	if len(result.AttributeHandles) != 0 || result.ObjectHandle != uint64(f.obj) {
		t.Fatalf("lost ownership confirmation=%v", result)
	}
	if got := f.objects.AttributeTransportType(ieeeOODDMFederation, f.obj, f.value); got != core.TransportTypeUnspecified {
		t.Fatalf("reacquired override=%v", got)
	}
}

func TestTransportationActivationHonorsCheckpointAdmissionWithoutConsumingTicket(t *testing.T) {
	f := newIEEETransportationFixture(t)
	request := &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT}
	prepared, err := f.objClient.PrepareAttributeTransportationTypeChange(f.ownerContext, request)
	ieeeOODDMOK(t, err)
	activation := &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), Ticket: prepared.Ticket, ExpectedFederationGeneration: prepared.FederationGeneration, ExpectedCallbackEpoch: prepared.CallbackEpoch}
	for _, checkpoint := range []struct {
		err  error
		name string
	}{{core.ErrSaveAlreadyInProgress, "SaveInProgress"}, {core.ErrRestoreAlreadyInProgress, "RestoreInProgress"}} {
		f.admission.block(checkpoint.err)
		var trailer metadata.MD
		_, err = f.objClient.ActivateTransportationTypeChange(f.ownerContext, activation, grpcgo.Trailer(&trailer))
		ieeeOODDMException(t, err, trailer, checkpoint.name)
		_, err = f.objClient.PrepareAttributeTransportationTypeChange(f.ownerContext, request, grpcgo.Trailer(&trailer))
		ieeeOODDMException(t, err, trailer, checkpoint.name)
		if got := f.objects.AttributeTransportType(ieeeOODDMFederation, f.obj, f.value); got != core.TransportTypeUnspecified {
			t.Fatalf("checkpoint-blocked activation changed override: %v", got)
		}
	}
	f.admission.block(nil)
	_, err = f.objClient.ActivateTransportationTypeChange(f.ownerContext, activation)
	ieeeOODDMOK(t, err)
}
