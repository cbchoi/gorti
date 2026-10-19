package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/protobuf/proto"
)

func ownershipCheckpointFixture(t *testing.T) (*ownership.Manager, *rtiv1.FederateEvent) {
	t.Helper()
	_, owner, out, _, _ := receiptAdapterForDomain(t)
	if err := owner.AcquireIfAvailable(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := toFederateEvent((<-out.ch)[0])
	if err != nil {
		t.Fatal(err)
	}
	event.CallbackReceipt = bytes.Repeat([]byte{3}, 32)
	return owner, event
}

func restoredOwnershipAdapter(t *testing.T) (*OwnershipCallbackReceipts, *ownership.Manager, core.OwnershipCallbackScope, *rtiv1.FederateEvent, *rtiv1.FederateEvent) {
	t.Helper()
	owner, source := ownershipCheckpointFixture(t)
	state := owner.ExportState("fed")
	binding, err := CaptureOwnershipCheckpointBinding(state, 2, source, false)
	if err != nil {
		t.Fatal(err)
	}
	body := proto.Clone(source).(*rtiv1.FederateEvent)
	body.CallbackReceipt, body.OwnershipReceipt = nil, nil
	replay, err := PrepareOwnershipCheckpointEvent(state, 2, body, binding, false)
	if err != nil {
		t.Fatal(err)
	}
	identity := sha256.Sum256(source.CallbackReceipt)
	replay.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity[:]}
	replay.CallbackReceipt = bytes.Repeat([]byte{4}, 32)
	owner.ImportState("fed", state)
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 8, CallbackEpoch: 9}
	reservation, err := owner.PrepareOwnershipCallbackScopeRestore("fed", map[core.FederateHandle]core.OwnershipCallbackScope{2: scope})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	receipts, err := NewOwnershipCallbackReceipts(OwnershipCallbackReceiptOptions{
		Coordinator: owner,
		Authenticate: func(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
			if fed != scope.Federation || h != scope.Recipient {
				return core.ErrOwnershipCallbackScope
			}
			return nil
		},
		AcquireScope: func(_ context.Context, actual core.OwnershipCallbackScope) (func(), error) {
			if actual != scope {
				return nil, core.ErrOwnershipCallbackScope
			}
			return func() {}, nil
		},
		RecoveryScope: func(_ context.Context, fed core.FederationName, h core.FederateHandle) (core.OwnershipCallbackScope, error) {
			if fed != scope.Federation || h != scope.Recipient {
				return core.OwnershipCallbackScope{}, core.ErrOwnershipCallbackScope
			}
			return scope, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return receipts, owner, scope, source, replay
}

func TestOwnershipCheckpointFreshScopeAttemptAndStaleSource(t *testing.T) {
	receipts, owner, scope, source, replay := restoredOwnershipAdapter(t)
	if _, err := receipts.Prepare(context.Background(), "fed", 2, source); !errors.Is(err, core.ErrOwnershipCallbackScope) {
		t.Fatal("source capability crossed restore", err)
	}
	finish, err := receipts.Prepare(context.Background(), "fed", 2, replay)
	if err != nil {
		t.Fatal("fresh restored ownership attempt rejected", err)
	}
	if finish == nil || replay.OwnershipReceipt.FederationGeneration != scope.FederationGeneration || replay.OwnershipReceipt.CallbackEpoch != scope.CallbackEpoch || replay.OwnershipReceipt.DeliveryAttempt != 1 {
		t.Fatal("replay did not use new scope/attempt", replay.OwnershipReceipt)
	}
	finish(true)
	wire := replay.OwnershipReceipt
	receipt := core.OwnershipCallbackReceipt{FederationGeneration: wire.FederationGeneration, CallbackEpoch: wire.CallbackEpoch, CallbackID: wire.CallbackId, DeliveryAttempt: wire.DeliveryAttempt}
	if err := owner.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal(err)
	}
	if err := owner.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal("duplicate fresh report", err)
	}
	if owner.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("fresh report retained obligation")
	}
}

func TestOwnershipCheckpointOutcomeOnlyPreservesGeneralCarrier(t *testing.T) {
	receipts, owner, scope, _, replay := restoredOwnershipAdapter(t)
	replay.CallbackRecovery.OutcomeOnly = true
	finish, err := receipts.Prepare(context.Background(), "fed", 2, replay)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	wire := replay.OwnershipReceipt
	if err := owner.ReportOwnershipCallbackInvocation(context.Background(), scope, core.OwnershipCallbackReceipt{FederationGeneration: wire.FederationGeneration, CallbackEpoch: wire.CallbackEpoch, CallbackID: wire.CallbackId, DeliveryAttempt: wire.DeliveryAttempt}); err != nil {
		t.Fatal(err)
	}
	sink := &fakeStream{ctx: context.Background()}
	stream := &ownershipReceiptEventsStream{StreamService_EventsServer: sink, receipts: receipts, fed: "fed", handle: 2}
	if err := stream.Send(replay); err != nil {
		t.Fatal(err)
	}
	if len(sink.sent) != 1 || sink.sent[0].GetOwnershipUnavailable() == nil || sink.sent[0].GetOwnershipReceipt() != nil || !bytes.Equal(sink.sent[0].CallbackReceipt, replay.CallbackReceipt) {
		t.Fatal("ownership tombstone suppressed or reauthorized general outcome carrier", sink.sent)
	}
}

func TestOwnershipCheckpointCompletedMarkerHasNoOwnershipAuthority(t *testing.T) {
	receipts, owner, _, _, replay := restoredOwnershipAdapter(t)
	before := owner.ExportState("fed")
	marker := &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: replay.CallbackRecovery.InvocationIdentity, OutcomeOnly: true, Completed: true}}
	for attempt := 0; attempt < 2; attempt++ {
		finish, err := receipts.Prepare(context.Background(), "fed", 2, marker)
		if err != nil || finish != nil {
			t.Fatal("reconciliation marker acquired ownership authority", err)
		}
	}
	if !reflect.DeepEqual(before, owner.ExportState("fed")) {
		t.Fatal("reconciliation marker changed ownership obligations")
	}
}

func TestOwnershipCheckpointRestoredControlCannotRetargetStream(t *testing.T) {
	for _, batches := range []bool{false, true} {
		name := "events"
		if batches {
			name = "batches"
		}
		t.Run(name, func(t *testing.T) {
			receipts, _, _, _, _ := restoredOwnershipAdapter(t)
			staleHandle, freshHandle := uint64(99), uint64(3)
			stale := &rtiv1.FederateEvent{CallbackReceipt: bytes.Repeat([]byte{4}, 32), CallbackRecovery: &rtiv1.CallbackRecovery{InvocationIdentity: bytes.Repeat([]byte{5}, 32), OutcomeOnly: true}, Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{FederateHandle: &staleHandle}}}
			fresh := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{FederateHandle: &freshHandle}}}
			if batches {
				sink := &fakeBatchStream{ctx: context.Background()}
				stream := &ownershipReceiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: receipts, fed: "fed", handle: 2}
				if err := stream.Send(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{stale}}); err != nil {
					t.Fatal(err)
				}
				if stream.handle != 2 {
					t.Fatal("recovered old terminal retargeted batch stream", stream.handle)
				}
				if err := stream.Send(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{fresh}}); err != nil {
					t.Fatal(err)
				}
				if stream.handle != 3 {
					t.Fatal("new terminal did not update batch identity")
				}
			} else {
				sink := &fakeStream{ctx: context.Background()}
				stream := &ownershipReceiptEventsStream{StreamService_EventsServer: sink, receipts: receipts, fed: "fed", handle: 2}
				if err := stream.Send(stale); err != nil {
					t.Fatal(err)
				}
				if stream.handle != 2 {
					t.Fatal("recovered old terminal retargeted event stream", stream.handle)
				}
				if err := stream.Send(fresh); err != nil {
					t.Fatal(err)
				}
				if stream.handle != 3 {
					t.Fatal("new terminal did not update event identity")
				}
			}
		})
	}
}

func TestOwnershipCheckpointCompletedRetainedBindingCanBeSavedAgain(t *testing.T) {
	receipts, owner, scope, _, replay := restoredOwnershipAdapter(t)
	replay.CallbackRecovery.OutcomeOnly = true
	finish, err := receipts.Prepare(context.Background(), "fed", 2, replay)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	wire := replay.OwnershipReceipt
	if err := owner.ReportOwnershipCallbackInvocation(context.Background(), scope, core.OwnershipCallbackReceipt{FederationGeneration: wire.FederationGeneration, CallbackEpoch: wire.CallbackEpoch, CallbackID: wire.CallbackId, DeliveryAttempt: wire.DeliveryAttempt}); err != nil {
		t.Fatal(err)
	}
	state := owner.ExportState("fed")
	retained := &OwnershipCheckpointBinding{CallbackID: wire.CallbackId}
	body := proto.Clone(replay).(*rtiv1.FederateEvent)
	body.CallbackReceipt, body.OwnershipReceipt = nil, nil
	prepared, err := PrepareOwnershipCheckpointEvent(state, 2, body, retained, true)
	if err != nil || prepared.OwnershipReceipt != nil {
		t.Fatal("completed ownership got wire authority", err)
	}
	got, err := CaptureOwnershipCheckpointBinding(state, 2, prepared, true, retained)
	if err != nil || !reflect.DeepEqual(got, retained) {
		t.Fatal("second save lost completed ownership relationship", got, err)
	}
}

func TestOwnershipCheckpointRecoveryMetadataDoesNotAuthorize(t *testing.T) {
	for _, invalid := range []string{"noRecovery", "shortIdentity", "foreignRecipient", "completedWithBody"} {
		t.Run(invalid, func(t *testing.T) {
			receipts, owner, _, _, replay := restoredOwnershipAdapter(t)
			before := owner.ExportState("fed")
			h := core.FederateHandle(2)
			switch invalid {
			case "noRecovery":
				replay.CallbackRecovery = nil
			case "shortIdentity":
				replay.CallbackRecovery.InvocationIdentity = []byte{1}
			case "foreignRecipient":
				h = 3
			case "completedWithBody":
				replay.CallbackRecovery.OutcomeOnly, replay.CallbackRecovery.Completed = true, true
			}
			if _, err := receipts.Prepare(context.Background(), "fed", h, replay); err == nil {
				t.Fatal("recovery identity substituted for live ownership authorization")
			}
			if !reflect.DeepEqual(before, owner.ExportState("fed")) {
				t.Fatal("rejected carrier changed ownership")
			}
		})
	}
}

func TestOwnershipCheckpointPendingBindingWithoutCapabilities(t *testing.T) {
	owner, event := ownershipCheckpointFixture(t)
	state := owner.ExportState("fed")
	before := proto.Clone(event)
	binding, err := CaptureOwnershipCheckpointBinding(state, 2, event, false)
	if err != nil || binding == nil || binding.CallbackID != event.OwnershipReceipt.CallbackId {
		t.Fatalf("binding=%+v error=%v", binding, err)
	}
	data, err := json.Marshal(binding)
	if err != nil || string(data) != `{"callback_id":1}` {
		t.Fatalf("capability-free binding=%s error=%v", data, err)
	}
	body := proto.Clone(event).(*rtiv1.FederateEvent)
	body.CallbackReceipt, body.OwnershipReceipt = nil, nil
	replay, err := PrepareOwnershipCheckpointEvent(state, 2, body, binding, false)
	if err != nil {
		t.Fatal(err)
	}
	if replay.OwnershipReceipt == nil || replay.OwnershipReceipt.CallbackId != 1 || replay.OwnershipReceipt.FederationGeneration != 0 || replay.OwnershipReceipt.CallbackEpoch != 0 || replay.OwnershipReceipt.DeliveryAttempt != 0 {
		t.Fatal("restored binding retained authority", replay.OwnershipReceipt)
	}
	if !proto.Equal(before, event) || !reflect.DeepEqual(state, owner.ExportState("fed")) {
		t.Fatal("checkpoint preparation mutated live input")
	}
}

func TestOwnershipCheckpointRepeatedRestoreRetainsBinding(t *testing.T) {
	_, owner, _, _, replay := restoredOwnershipAdapter(t)
	state := owner.ExportState("fed")
	before := proto.Clone(replay)
	binding, err := CaptureOwnershipCheckpointBinding(state, 2, replay, false)
	if err != nil || binding == nil || binding.CallbackID != 1 {
		t.Fatalf("unscoped replay cannot be saved again: %+v %v", binding, err)
	}
	if !proto.Equal(before, replay) {
		t.Fatal("repeated capture changed stable identity/body")
	}
}

func TestOwnershipCheckpointTerminalKindsAndRetainedProjection(t *testing.T) {
	for _, kind := range []ownership.CallbackKind{ownership.CallbackAcquisitionNotification, ownership.CallbackUnavailable, ownership.CallbackAcquisitionCancellation} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			owner, event := ownershipCheckpointFixture(t)
			state := owner.ExportState("fed")
			pending := &state.PendingCallbacks[0]
			pending.Kind = kind
			pending.RetainedProjection = &ownership.CallbackProjection{RegisteredClass: 8, KnownClass: 9, Attributes: []core.AttributeHandle{3}}
			switch kind {
			case ownership.CallbackAcquisitionNotification:
				pending.Federate, pending.Tag = 2, []byte{0, 255}
				event.Event = &rtiv1.FederateEvent_OwnershipAcquired{OwnershipAcquired: &rtiv1.AttributeOwnershipAcquisitionNotification{ObjectHandle: 100, AttributeHandles: []uint64{3}, OwningFederate: 2, Tag: []byte{0, 255}}}
			case ownership.CallbackUnavailable:
				event.GetOwnershipUnavailable().AttributeHandles = []uint64{3}
			case ownership.CallbackAcquisitionCancellation:
				event.Event = &rtiv1.FederateEvent_OwnershipAcquisitionCancelled{OwnershipAcquisitionCancelled: &rtiv1.ConfirmAttributeOwnershipAcquisitionCancellation{ObjectHandle: 100, AttributeHandles: []uint64{3}}}
			}
			binding, err := CaptureOwnershipCheckpointBinding(state, 2, event, false)
			if err != nil {
				t.Fatal(err)
			}
			body := proto.Clone(event).(*rtiv1.FederateEvent)
			body.CallbackReceipt, body.OwnershipReceipt = nil, nil
			for _, outcomeOnly := range []bool{false, true} {
				replay, err := PrepareOwnershipCheckpointEvent(state, 2, body, binding, outcomeOnly)
				if err != nil {
					t.Fatal("projected terminal relationship", err)
				}
				replay.OwnershipReceipt = nil
				if !proto.Equal(body, replay) {
					t.Fatal("portable terminal body changed")
				}
			}
		})
	}
}

func TestOwnershipCheckpointCompletedRequiresOutcomeOnly(t *testing.T) {
	owner, event := ownershipCheckpointFixture(t)
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 5}
	receipt, finish, err := owner.BeginOwnershipCallbackDelivery(context.Background(), scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := owner.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal(err)
	}
	state := owner.ExportState("fed")
	if _, err := CaptureOwnershipCheckpointBinding(state, 2, event, false); err == nil {
		t.Fatal("completed ownership callback was allowed to replay its body")
	}
	binding, err := CaptureOwnershipCheckpointBinding(state, 2, event, true)
	if err != nil {
		t.Fatal(err)
	}
	body := proto.Clone(event).(*rtiv1.FederateEvent)
	body.CallbackReceipt, body.OwnershipReceipt = nil, nil
	replay, err := PrepareOwnershipCheckpointEvent(state, 2, body, binding, true)
	if err != nil || replay.OwnershipReceipt != nil || !proto.Equal(body, replay) {
		t.Fatalf("completed ownership suppressed/reauthorized general outcome: %+v %v", replay, err)
	}
	if _, err := PrepareOwnershipCheckpointEvent(state, 2, body, binding, false); err == nil {
		t.Fatal("completed ownership became a pending body")
	}
}

func TestOwnershipCheckpointRejectsMismatchedBindingAtomically(t *testing.T) {
	owner, event := ownershipCheckpointFixture(t)
	state := owner.ExportState("fed")
	binding, err := CaptureOwnershipCheckpointBinding(state, 2, event, false)
	if err != nil {
		t.Fatal(err)
	}
	body := proto.Clone(event).(*rtiv1.FederateEvent)
	body.CallbackReceipt, body.OwnershipReceipt = nil, nil
	for name, mutate := range map[string]func(*rtiv1.FederateEvent, *OwnershipCheckpointBinding){
		"unknownID": func(_ *rtiv1.FederateEvent, b *OwnershipCheckpointBinding) { b.CallbackID++ },
		"zeroID":    func(_ *rtiv1.FederateEvent, b *OwnershipCheckpointBinding) { b.CallbackID = 0 },
		"wrongObject": func(e *rtiv1.FederateEvent, _ *OwnershipCheckpointBinding) {
			e.GetOwnershipUnavailable().ObjectHandle++
		},
		"wrongAttribute": func(e *rtiv1.FederateEvent, _ *OwnershipCheckpointBinding) {
			e.GetOwnershipUnavailable().AttributeHandles[0]++
		},
		"storedOwnershipAuthority": func(e *rtiv1.FederateEvent, _ *OwnershipCheckpointBinding) {
			e.OwnershipReceipt = event.OwnershipReceipt
		},
		"storedGeneralAuthority": func(e *rtiv1.FederateEvent, _ *OwnershipCheckpointBinding) { e.CallbackReceipt = event.CallbackReceipt },
		"missingBody":            func(e *rtiv1.FederateEvent, _ *OwnershipCheckpointBinding) { e.Event = nil },
	} {
		t.Run(name, func(t *testing.T) {
			e, b := proto.Clone(body).(*rtiv1.FederateEvent), *binding
			mutate(e, &b)
			if _, err := PrepareOwnershipCheckpointEvent(state, 2, e, &b, false); err == nil {
				t.Fatal("invalid ownership recovery accepted")
			}
			if !reflect.DeepEqual(state, owner.ExportState("fed")) {
				t.Fatal("invalid restore changed ownership state")
			}
		})
	}
	if _, err := PrepareOwnershipCheckpointEvent(state, 3, body, binding, false); err == nil {
		t.Fatal("cross-recipient recovery accepted")
	}
	if _, err := PrepareOwnershipCheckpointEvent(state, 2, body, nil, false); err == nil {
		t.Fatal("strict terminal lost ownership binding")
	}
}
