package federate

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func TestGoRemainingCallbackVariantsAreDelivered(t *testing.T) {
	f := &Federate{federateHandle: 7, objectClasses: map[uint64]uint64{80: 19}}
	events := []*rtiv1.FederateEvent{
		{Event: &rtiv1.FederateEvent_OwnershipAssumption{OwnershipAssumption: &rtiv1.RequestAttributeOwnershipAssumption{ObjectHandle: 80, AttributeHandles: []uint64{2}, Tag: []byte(testUserTag)}}},
		{Event: &rtiv1.FederateEvent_OwnershipAcquired{OwnershipAcquired: &rtiv1.AttributeOwnershipAcquisitionNotification{ObjectHandle: 80, AttributeHandles: []uint64{2}, OwningFederate: 7}}},
		{Event: &rtiv1.FederateEvent_OwnershipDivestConfirmed{OwnershipDivestConfirmed: &rtiv1.RequestDivestitureConfirmation{ObjectHandle: 80, AttributeHandles: []uint64{2}}}},
		{Event: &rtiv1.FederateEvent_OwnershipReleaseRequested{OwnershipReleaseRequested: &rtiv1.RequestAttributeOwnershipRelease{ObjectHandle: 80, AttributeHandles: []uint64{2}}}},
		{Event: &rtiv1.FederateEvent_OwnershipUnavailable{OwnershipUnavailable: &rtiv1.AttributeOwnershipUnavailable{ObjectHandle: 80, AttributeHandles: []uint64{7}}}},
		{Event: &rtiv1.FederateEvent_SyncRegistrationSucceeded{SyncRegistrationSucceeded: &rtiv1.SyncRegistrationSucceeded{Label: "sync"}}},
		{Event: &rtiv1.FederateEvent_SyncRegistrationFailed{SyncRegistrationFailed: &rtiv1.SyncRegistrationFailed{Label: "sync"}}},
		{Event: &rtiv1.FederateEvent_SaveInitiate{SaveInitiate: &rtiv1.InitiateFederateSave{Label: "save"}}},
		{Event: &rtiv1.FederateEvent_SaveCompleted{SaveCompleted: &rtiv1.FederationSaved{Label: "save"}}},
		{Event: &rtiv1.FederateEvent_SaveFailed{SaveFailed: &rtiv1.FederationNotSaved{Label: "save"}}},
		{Event: &rtiv1.FederateEvent_RestoreInitiate{RestoreInitiate: &rtiv1.InitiateFederateRestore{Label: "save", FederateHandle: 7}}},
		{Event: &rtiv1.FederateEvent_RestoreFailed{RestoreFailed: &rtiv1.FederationNotRestored{Label: "save"}}},
		{Event: &rtiv1.FederateEvent_RestoreRequestSucceeded{RestoreRequestSucceeded: &rtiv1.RequestFederationRestoreSucceeded{Label: "save"}}},
		{Event: &rtiv1.FederateEvent_RestoreRequestFailed{RestoreRequestFailed: &rtiv1.RequestFederationRestoreFailed{Label: "save", Reason: "missing"}}},
		{Event: &rtiv1.FederateEvent_RestoreBegun{RestoreBegun: &rtiv1.FederationRestoreBegun{}}},
		{Event: &rtiv1.FederateEvent_ReservationMultiSucceeded{ReservationMultiSucceeded: &rtiv1.MultipleObjectInstanceNameReservationSucceeded{ObjectNames: []string{"one", "two"}}}},
		{Event: &rtiv1.FederateEvent_ReservationMultiFailed{ReservationMultiFailed: &rtiv1.MultipleObjectInstanceNameReservationFailed{RequestedNames: []string{"one", "two"}, CollidingNames: []string{"two"}}}},
		{Event: &rtiv1.FederateEvent_StartRegistration{StartRegistration: &rtiv1.StartRegistrationForObjectClass{ObjectClassHandle: 19}}},
		{Event: &rtiv1.FederateEvent_StopRegistration{StopRegistration: &rtiv1.StopRegistrationForObjectClass{ObjectClassHandle: 19}}},
		{Event: &rtiv1.FederateEvent_TurnInteractionsOn{TurnInteractionsOn: &rtiv1.TurnInteractionsOn{InteractionClassHandle: 27}}},
		{Event: &rtiv1.FederateEvent_TurnInteractionsOff{TurnInteractionsOff: &rtiv1.TurnInteractionsOff{InteractionClassHandle: 27}}},
		{Event: &rtiv1.FederateEvent_AttributesInScope{AttributesInScope: &rtiv1.AttributesInScope{ObjectHandle: 80, AttributeHandles: []uint64{2}}}},
		{Event: &rtiv1.FederateEvent_AttributesOutOfScope{AttributesOutOfScope: &rtiv1.AttributesOutOfScope{ObjectHandle: 80, AttributeHandles: []uint64{2}}}},
	}
	for _, e := range events {
		if got := f.translate(e); got == nil {
			t.Errorf("dropped recognized callback %T", e.Event)
		}
	}
	if _, ok := f.ownedObjectAttributes[80][2]; !ok {
		t.Fatal("acquisition callback did not update ownership, or confirmation request prematurely removed it")
	}
	if _, ok := f.ownedObjectAttributes[80][7]; ok {
		t.Fatal("unavailable callback fabricated ownership")
	}
}

func TestGoCallbackReceiptSurvivesTranslation(t *testing.T) {
	f := &Federate{federateHandle: 7}
	e := &rtiv1.FederateEvent{CallbackReceipt: []byte{0, 1, 255}, Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 5}}}
	got := f.translate(e)
	carrier, ok := got.(interface{ CallbackReceipt() []byte })
	if !ok {
		t.Fatal("Go SDK discards callback invocation receipt")
	}
	e.CallbackReceipt[0] = 9
	cloned := carrier.CallbackReceipt()
	cloned[1] = 9
	if receipt := carrier.CallbackReceipt(); len(receipt) != 3 || receipt[0] != 0 || receipt[1] != 1 {
		t.Fatalf("receipt aliases mutable input: %v", receipt)
	}
}
