package grpc_test

import (
	"bytes"
	"testing"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/proto"
)

func transportationCheckpointSource(t *testing.T, f *ieeeTransportationFixture) *rtiv1.FederateEvent {
	t.Helper()
	change, err := f.objClient.PrepareAttributeTransportationTypeChange(f.ownerContext, &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT})
	ieeeOODDMOK(t, err)
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: change}}
}

func TestTransportationCheckpointVerifiedCaptureStripAndFreshReservation(t *testing.T) {
	f := newIEEETransportationFixture(t)
	event := transportationCheckpointSource(t, f)
	original := proto.Clone(event).(*rtiv1.FederateEvent)
	snapshot := f.objects.ExportTransportationCheckpoint(ieeeOODDMFederation)
	if _, err := grpcsvc.CaptureTransportationCheckpointBinding(nil, f.owner, event, false); err == nil {
		t.Fatal("unverified real source capability accepted")
	}
	if _, err := grpcsvc.CaptureTransportationCheckpointBinding(snapshot, f.acquirer, event, false); err == nil {
		t.Fatal("foreign source recipient accepted")
	}
	binding, err := grpcsvc.CaptureTransportationCheckpointBinding(snapshot, f.owner, event, false)
	ieeeOODDMOK(t, err)
	stripped, err := grpcsvc.StripTransportationCheckpointEvent(event, binding)
	ieeeOODDMOK(t, err)
	if body := stripped.GetTransportationChangeConfirmed(); len(body.Ticket) != 0 || body.FederationGeneration != 0 || body.CallbackEpoch != 0 {
		t.Fatal("nested source capability persisted")
	}
	if !proto.Equal(original, event) {
		t.Fatal("capture/strip mutated source event")
	}
	if err := grpcsvc.ValidateTransportationCheckpointEvent(event, binding); err == nil {
		t.Fatal("stored nested capability accepted")
	}
	commit, err := f.objects.PrepareImportState(ieeeOODDMFederation, f.objects.ExportState(ieeeOODDMFederation))
	ieeeOODDMOK(t, err)
	commit()
	scope := snapshot[string(original.GetTransportationChangeConfirmed().Ticket)].Scope
	scope.CallbackEpoch++
	reservation, err := grpcsvc.PrepareTransportationCheckpointEvents(f.ctx, f.objects, []grpcsvc.TransportationCheckpointReplay{{Event: stripped, Binding: binding, Scope: scope}})
	ieeeOODDMOK(t, err)
	defer reservation.Release()
	fresh := reservation.Events()[0].GetTransportationChangeConfirmed()
	if len(fresh.Ticket) != 32 || bytes.Equal(fresh.Ticket, original.GetTransportationChangeConfirmed().Ticket) || fresh.FederationGeneration != scope.Member.Generation || fresh.CallbackEpoch != scope.CallbackEpoch {
		t.Fatal("restored nested authority not fresh", fresh)
	}
	reservation.Commit()
	_, err = f.objects.ActivateTransportationChange(f.ctx, scope, fresh.Ticket)
	ieeeOODDMOK(t, err)
	got, err := f.objects.QueryAttributeTransportationType(f.ctx, ieeeOODDMFederation, f.owner, f.obj, f.value)
	ieeeOODDMOK(t, err)
	if got != core.TransportTypeBestEffort {
		t.Fatal("fresh pending ticket did not activate actual transport", got)
	}
}

func TestTransportationCheckpointOutcomeSurvivesTicketEvictionWithoutActivation(t *testing.T) {
	f := newIEEETransportationFixture(t)
	event := transportationCheckpointSource(t, f)
	change := event.GetTransportationChangeConfirmed()
	scope := f.objects.ExportTransportationCheckpoint(ieeeOODDMFederation)[string(change.Ticket)].Scope
	_, err := f.objects.ActivateTransportationChange(f.ctx, scope, change.Ticket)
	ieeeOODDMOK(t, err)
	for i := 0; i < 256; i++ {
		next, err := f.objects.PrepareAttributeTransportationChange(f.ctx, scope, f.obj, []core.AttributeHandle{f.value}, core.TransportTypeReliable)
		ieeeOODDMOK(t, err)
		_, err = f.objects.ActivateTransportationChange(f.ctx, scope, next.Ticket)
		ieeeOODDMOK(t, err)
	}
	if _, retained := f.objects.ExportTransportationCheckpoint(ieeeOODDMFederation)[string(change.Ticket)]; retained {
		t.Fatal("test did not evict the original activated capability")
	}
	binding, err := grpcsvc.CaptureTransportationCheckpointBinding(nil, f.owner, event, true)
	ieeeOODDMOK(t, err)
	if binding != nil {
		t.Fatal("outcome-only capture retained activation intent")
	}
	stripped, err := grpcsvc.StripTransportationCheckpointEvent(event, nil)
	ieeeOODDMOK(t, err)
	stripped.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: bytes.Repeat([]byte{1}, 32), OutcomeOnly: true}
	reservation, err := grpcsvc.PrepareTransportationCheckpointEvents(f.ctx, nil, []grpcsvc.TransportationCheckpointReplay{{Event: stripped, OutcomeOnly: true}})
	ieeeOODDMOK(t, err)
	defer reservation.Release()
	reservation.Commit()
	body := reservation.Events()[0].GetTransportationChangeConfirmed()
	if len(body.Ticket) != 0 || body.FederationGeneration != 0 || body.CallbackEpoch != 0 {
		t.Fatal("outcome-only replay gained activation authority")
	}
	got, err := f.objects.QueryAttributeTransportationType(f.ctx, ieeeOODDMFederation, f.owner, f.obj, f.value)
	ieeeOODDMOK(t, err)
	if got != core.TransportTypeReliable {
		t.Fatal("outcome reconciliation reapplied original best-effort change", got)
	}
	if _, err := grpcsvc.PrepareTransportationCheckpointEvents(f.ctx, f.objects, []grpcsvc.TransportationCheckpointReplay{{Event: stripped}}); err == nil {
		t.Fatal("pending replay without binding accepted")
	}
}
