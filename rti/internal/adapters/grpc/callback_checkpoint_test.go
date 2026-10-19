package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func checkpointReceive(seq uint64) *rtiv1.FederateEvent {
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	return &rtiv1.FederateEvent{Seq: seq, Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: 7, Parameters: map[uint64][]byte{1: {0, 255}}, Metadata: &rtiv1.ObjectCallbackMetadata{ReceivedOrder: &order}}}}
}
func TestCallbackCheckpointConservesDistinctReadyTransportClientData(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 32)
	ctx := context.Background()
	for _, h := range []core.FederateHandle{1, 1, 2} {
		if err := out.Send(ctx, "f", h, &requiredCallbackEvent{event: checkpointReceive(0)}); err != nil {
			t.Fatal(err)
		}
	}
	var entryTicket []byte
	r.mu.Lock()
	for ticket, entry := range r.entries {
		if entry.h == 2 {
			entryTicket = []byte(ticket)
		} else {
			entry.sent = true
		}
	}
	r.mu.Unlock()
	if err := r.ReportEntry(ctx, "f", 2, entryTicket); err != nil {
		t.Fatal(err)
	}
	image, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(image, entryTicket) {
		t.Fatal("receipt persisted")
	}
	decoded, err := decodeCallbackCheckpoint(image)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Deliveries) != 3 || decoded.Deliveries[2].Phase != callbackPhaseOutcome {
		t.Fatalf("equal data collapsed or entered outcome lost: %+v", decoded.Deliveries)
	}
	if decoded.Deliveries[0].Order >= decoded.Deliveries[1].Order {
		t.Fatal("admission order lost")
	}
	if !bytes.Equal(decoded.Deliveries[0].Event, decoded.Deliveries[1].Event) {
		t.Fatal("equal payload unexpectedly changed")
	}
}
func TestCallbackCheckpointReplayFreshTicketsBeforePublicationAndRollback(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 16)
	ctx := context.Background()
	if err := out.Send(ctx, "f", 1, &requiredCallbackEvent{event: checkpointReceive(8)}); err != nil {
		t.Fatal(err)
	}
	var old []byte
	for ticket := range r.entries {
		old = []byte(ticket)
	}
	data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	participants := []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}}
	pending, replay, err := r.PrepareCheckpointReplay(ctx, "f", data, participants)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := EncodeOutboundEvent(pending[1][0])
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.CallbackReceipt) != 32 || bytes.Equal(old, wire.CallbackReceipt) {
		t.Fatal("old capability reused")
	}
	if r.entries[string(wire.CallbackReceipt)] == nil {
		t.Fatal("ticket not reserved before publication")
	}
	replay.Release()
	replay.Release()
	if r.pending != 1 || len(r.entries) != 1 {
		t.Fatal("replay rollback damaged live state")
	}
	pending, replay, err = r.PrepareCheckpointReplay(ctx, "f", data, participants)
	if err != nil {
		t.Fatal(err)
	}
	wire, err = EncodeOutboundEvent(pending[1][0])
	if err != nil {
		t.Fatal(err)
	}
	replay.Admit()
	if err = replay.Commit(); err != nil {
		t.Fatal(err)
	}
	r.restoreCommitted("f", "saved", map[core.FederateHandle]core.FederateHandle{1: 1})
	if err = r.ReportEntry(ctx, "f", 1, old); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("old receipt survived", err)
	}
	if err = r.ReportEntry(ctx, "f", 1, wire.CallbackReceipt); err != nil {
		t.Fatal("fresh receipt removed", err)
	}
	if err = r.Report(ctx, "f", 1, wire.CallbackReceipt, true, ""); err != nil {
		t.Fatal(err)
	}
	if r.pending != 0 {
		t.Fatal("pending leak")
	}
}
func TestCallbackCheckpointRejectsCorruptCapabilitiesAndRosterAtomically(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 16)
	event := checkpointReceive(1)
	payload, _ := proto.Marshal(event)
	valid := callbackCheckpointImage{Version: 1, Deliveries: []callbackCheckpointDelivery{{Recipient: 1, Order: 1, Event: payload}}}
	for _, name := range []string{"version", "duplicate-order", "duplicate-key", "duplicate-recipient", "capability", "unknown-wire", "unknown-json", "roster", "trailing"} {
		t.Run(name, func(t *testing.T) {
			image := valid
			image.Deliveries = append([]callbackCheckpointDelivery(nil), valid.Deliveries...)
			participants := []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}}
			switch name {
			case "version":
				image.Version = 2
			case "duplicate-order":
				image.Deliveries = append(image.Deliveries, image.Deliveries[0])
			case "capability":
				v := proto.Clone(event).(*rtiv1.FederateEvent)
				v.CallbackReceipt = make([]byte, 32)
				image.Deliveries[0].Event, _ = proto.Marshal(v)
			case "unknown-wire":
				image.Deliveries[0].Event = append(bytes.Clone(payload), 0xf8, 0x7f, 0x01)
			case "roster":
				participants = nil
			}
			data, _ := json.Marshal(image)
			if name == "duplicate-key" {
				data = append(data[:len(data)-1], []byte(`,"version":1}`)...)
			}
			if name == "duplicate-recipient" {
				data = bytes.Replace(data, []byte(`"recipient":1`), []byte(`"recipient":1,"recipient":1`), 1)
			}
			if name == "unknown-json" {
				data = append(data[:len(data)-1], []byte(`,"extra":1}`)...)
			}
			if name == "trailing" {
				data = append(data, []byte(`{}`)...)
			}
			if _, _, err := r.PrepareCheckpointReplay(context.Background(), "f", data, participants); err == nil {
				t.Fatal("invalid checkpoint accepted")
			}
			if r.pending != 0 || len(r.entries) != 0 {
				t.Fatal("invalid preparation mutated ledger")
			}
		})
	}
}
func TestCallbackCheckpointRebindChangesOnlyFederateIdentity(t *testing.T) {
	event := checkpointReceive(12)
	producer := uint64(1)
	event.GetReceive().Metadata.ProducingFederate = &producer
	payload, _ := proto.Marshal(event)
	data, _ := json.Marshal(callbackCheckpointImage{Version: 1, Deliveries: []callbackCheckpointDelivery{{Recipient: 1, Order: 1, Event: payload}}})
	out, err := (CallbackCheckpoint{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil })
	if err != nil {
		t.Fatal(err)
	}
	image, err := decodeCallbackCheckpoint(out)
	if err != nil {
		t.Fatal(err)
	}
	v := &rtiv1.FederateEvent{}
	if err = proto.Unmarshal(image.Deliveries[0].Event, v); err != nil {
		t.Fatal(err)
	}
	if image.Deliveries[0].Recipient != 101 || v.GetReceive().Metadata.GetProducingFederate() != 101 || v.GetReceive().InteractionClassHandle != 7 || v.Seq != 12 {
		t.Fatal("identity rewrite touched non-federate values", v)
	}
}
