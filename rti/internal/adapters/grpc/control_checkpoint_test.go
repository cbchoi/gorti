package grpc

import (
	"context"
	"encoding/hex"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestControlCheckpointRetainsEnteredOutcomeWithoutReentry(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 16)
	ctx := context.Background()
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 3}}}
	if err := out.Send(ctx, "f", 1, &requiredCallbackEvent{event: event}); err != nil {
		t.Fatal(err)
	}
	var old []byte
	for ticket := range r.entries {
		old = []byte(ticket)
	}
	if err := r.ReportEntry(ctx, "f", 1, old); err != nil {
		t.Fatal(err)
	}
	data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		t.Fatal(err)
	}
	if image.Version != 2 || len(image.Deliveries) != 1 || image.Deliveries[0].Phase != callbackPhaseOutcome {
		t.Fatalf("entered control lost: %+v", image)
	}
	identity, err := hex.DecodeString(image.Deliveries[0].Identity)
	if err != nil || len(identity) != 32 {
		t.Fatal("invalid identity")
	}
	pending, reservation, err := r.PrepareCheckpointReplay(ctx, "f", data, []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	wire, err := EncodeOutboundEvent(pending[1][0])
	if err != nil {
		t.Fatal(err)
	}
	if !wire.GetCallbackRecovery().GetOutcomeOnly() || wire.GetCallbackRecovery().GetCompleted() {
		t.Fatal("body could be invoked again", wire)
	}
	reservation.Admit()
	commit, skip, err := r.preparedDelivery("f", 1, wire)
	if err != nil || skip {
		t.Fatalf("outcome carrier suppressed: %v %v", skip, err)
	}
	commit(true)
	if err := r.Report(ctx, "f", 1, wire.CallbackReceipt, true, ""); err != nil {
		t.Fatal(err)
	}
}

func TestControlCheckpointCompletedOutcomeBecomesReceiptlessMarker(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 16)
	ctx := context.Background()
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_ReservationSucceeded{ReservationSucceeded: &rtiv1.ObjectInstanceNameReservationSucceeded{ObjectName: "owned"}}}
	if err := out.Send(ctx, "f", 1, &requiredCallbackEvent{event: event}); err != nil {
		t.Fatal(err)
	}
	var ticket []byte
	for key := range r.entries {
		ticket = []byte(key)
	}
	if err := r.ReportEntry(ctx, "f", 1, ticket); err != nil {
		t.Fatal(err)
	}
	r.entries[string(ticket)].sent = true
	if err := r.Report(ctx, "f", 1, ticket, false, "user failure"); err != nil {
		t.Fatal(err)
	}
	data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(image.Deliveries) != 1 || image.Deliveries[0].Phase != callbackPhaseCompleted || len(image.Deliveries[0].Event) != 0 {
		t.Fatalf("lost acknowledgment tombstone: %+v", image)
	}
	pending, reservation, err := r.PrepareCheckpointReplay(ctx, "f", data, []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	wire, err := EncodeOutboundEvent(pending[1][0])
	if err != nil {
		t.Fatal(err)
	}
	if !wire.GetCallbackRecovery().GetCompleted() || !wire.GetCallbackRecovery().GetOutcomeOnly() || len(wire.CallbackReceipt) != 0 || wire.Event != nil {
		t.Fatal("invalid reconciliation marker", wire)
	}
	reservation.Admit()
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	r.restoreCommitted("f", "saved", map[core.FederateHandle]core.FederateHandle{1: 1})
	again, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	next, err := decodeCallbackCheckpoint(again)
	if err != nil || len(next.Deliveries) != 1 || next.Deliveries[0].Identity != image.Deliveries[0].Identity || next.Deliveries[0].Phase != callbackPhaseCompleted {
		t.Fatalf("repeated save lost outcome tombstone: %+v %v", next, err)
	}
}
