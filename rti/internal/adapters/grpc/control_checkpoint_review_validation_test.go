package grpc

import (
	"context"
	"errors"
	"math"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
)

type controlReviewNames struct{ core.FOMHandleNameLookup }

func (controlReviewNames) ObjectClassName(h core.ObjectClassHandle) (string, bool) {
	return "Object", h == 7
}
func (controlReviewNames) InteractionClassName(h core.InteractionClassHandle) (string, bool) {
	return "Interaction", h == 8
}

func TestControlCheckpointReviewScalarValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event *rtiv1.FederateEvent
		kind  logicaltime.Representation
		valid bool
	}{
		{"float grant", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 2}}}, logicaltime.HLAfloat64Time, true},
		{"nan grant", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: math.NaN()}}}, logicaltime.HLAfloat64Time, false},
		{"integer missing selector", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{}}}, logicaltime.HLAinteger64Time, false},
		{"integer exact", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTimeValue: &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: 1<<53 + 1}}}}}, logicaltime.HLAinteger64Time, true},
		{"known class", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_StartRegistration{StartRegistration: &rtiv1.StartRegistrationForObjectClass{ObjectClassHandle: 7}}}, logicaltime.HLAfloat64Time, true},
		{"unknown class", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_StartRegistration{StartRegistration: &rtiv1.StartRegistrationForObjectClass{ObjectClassHandle: 9}}}, logicaltime.HLAfloat64Time, false},
		{"empty label", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SaveCompleted{SaveCompleted: &rtiv1.FederationSaved{}}}, logicaltime.HLAfloat64Time, false},
		{"duplicate required", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: "s", RequiredFederates: []uint64{1, 1}}}}, logicaltime.HLAfloat64Time, false},
		{"duplicate attributes", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_ProvideUpdate{ProvideUpdate: &rtiv1.ProvideAttributeValueUpdate{ObjectHandle: 1, AttributeHandles: []uint64{2, 2}}}}, logicaltime.HLAfloat64Time, false},
		{"bad reason", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncRegistrationFailed{SyncRegistrationFailed: &rtiv1.SyncRegistrationFailed{Label: "s", Reason: 99}}}, logicaltime.HLAfloat64Time, false},
		{"interaction transportation", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: &rtiv1.TransportationChange{InteractionClassHandle: 8, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE}}}, logicaltime.HLAfloat64Time, true},
		{"bad transportation enum", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: &rtiv1.TransportationChange{InteractionClassHandle: 8, TransportType: 99}}}, logicaltime.HLAfloat64Time, false},
		{"failed empty reservation echoes request", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_ReservationFailed{ReservationFailed: &rtiv1.ObjectInstanceNameReservationFailed{}}}, logicaltime.HLAfloat64Time, true},
		{"failed duplicate reservation echoes request", &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_ReservationMultiFailed{ReservationMultiFailed: &rtiv1.MultipleObjectInstanceNameReservationFailed{RequestedNames: []string{"n", "n"}, CollidingNames: []string{"n"}}}}, logicaltime.HLAfloat64Time, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := proto.Clone(tc.event)
			err := validateCheckpointControl(tc.event, controlReviewNames{}, tc.kind)
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, want valid %v", err, tc.valid)
			}
			if !proto.Equal(before, tc.event) {
				t.Fatal("validation mutated original control")
			}
		})
	}
}

func TestControlCheckpointReviewWorkingTransitionFailsClosed(t *testing.T) {
	for _, phase := range []string{"entry", callbackPhaseOutcome} {
		t.Run(phase, func(t *testing.T) {
			ledger := checkpointReviewLedger(t, 4)
			event := controlCheckpointReviewEvent(t, "sync_registration_succeeded")
			ticket, sent, err := ledger.PrepareRequired(context.Background(), "f", 1, event)
			if err != nil {
				t.Fatal(err)
			}
			sent(true)
			ledger.mu.Lock()
			row := ledger.entries[string(ticket)]
			if phase == "entry" {
				row.entryWorking = make(chan struct{})
			} else {
				row.outcomeWorking = make(chan struct{})
			}
			ledger.mu.Unlock()
			if _, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return ledger }}).Marshal("f"); err == nil {
				t.Fatal("in-progress callback transition was checkpointed")
			}
			ledger.mu.Lock()
			unchanged := ledger.pending == 1 && len(ledger.entries) == 1 && ledger.entries[string(ticket)] == row && !row.applied
			ledger.mu.Unlock()
			if !unchanged {
				t.Fatal("failed capture changed live receipt obligation")
			}
		})
	}
}

func TestControlCheckpointReviewTypedFederateRewrite(t *testing.T) {
	event := &rtiv1.FederateEvent{Seq: 3, Event: &rtiv1.FederateEvent_RetractionRequested{RetractionRequested: &rtiv1.RequestRetraction{SenderFederate: 3, MessageRetractionHandle: 3}}}
	if err := rewriteCheckpointControlFederates(event, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil }); err != nil {
		t.Fatal(err)
	}
	if event.Seq != 3 || event.GetRetractionRequested().MessageRetractionHandle != 3 || event.GetRetractionRequested().SenderFederate != 103 {
		t.Fatal("rewriter changed a non-federate numeric identity")
	}
	event = &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: "s", RequiredFederates: []uint64{1, 2}}}}
	before := proto.Clone(event)
	err := rewriteCheckpointControlFederates(event, func(h core.FederateHandle) (core.FederateHandle, error) {
		if h == 2 {
			return 0, errors.New("not in roster")
		}
		return h + 100, nil
	})
	if err == nil || !proto.Equal(before, event) {
		t.Fatal("failed typed rewrite partially mutated event")
	}
}
