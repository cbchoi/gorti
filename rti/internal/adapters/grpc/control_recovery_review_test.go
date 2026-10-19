package grpc

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestControlRecoveryTransportationAuthorityCannotBeSavedWithoutBinding(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 4)
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: &rtiv1.TransportationChange{
		Ticket: bytes.Repeat([]byte{0xac}, 32), FederationGeneration: 7, CallbackEpoch: 9,
		InteractionClassHandle: 12, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE,
	}}}
	if err := out.Send(context.Background(), "f", 1, &requiredCallbackEvent{event: event}); err != nil {
		t.Fatal(err)
	}
	if _, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f"); err == nil {
		t.Fatal("transportation activation authority saved without a verified portable intent binding")
	}
}

func TestControlRecoveryObsoleteAdvisoryCannotSurviveLedger(t *testing.T) {
	for _, boundary := range []string{"checkpoint", "dispatch", "reconnect"} {
		t.Run(boundary, func(t *testing.T) {
			r, raw, out := requiredCallbackFixture(t, 1)
			ctx := context.Background()
			ch, cancel, err := raw.Subscribe(ctx, "f", 1)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cancel() }()
			guard := &streamKnowledgeGuard{}
			event := delivery.GuardCancellableEvent(&ownership.CallbackEvent{Sequence: 1, Kind: ownership.CallbackAssumption, Object: 7, Attributes: []core.AttributeHandle{1}}, guard, object.ErrStaleObjectKnowledge)
			if err := out.Send(ctx, "f", 1, event); err != nil {
				t.Fatal(err)
			}
			queued := (<-ch)[0]
			guard.stale.Store(true)
			switch boundary {
			case "dispatch":
				wire, err := toDispatchFederateEvent(queued)
				if err != nil || wire != nil {
					t.Fatalf("obsolete advice delivered: %v %v", wire, err)
				}
			case "reconnect":
				r.markDisconnected("f", 1)
				if pending := r.pendingRedelivery("f", 1); len(pending) != 0 {
					t.Fatalf("obsolete advice retried: %v", pending)
				}
			}
			data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
			if err != nil {
				t.Fatal(err)
			}
			image, err := decodeCallbackCheckpoint(data)
			if err != nil || len(image.Deliveries) != 0 || r.pending != 0 || len(r.entries) != 0 {
				t.Fatalf("obsolete obligation leaked: %+v pending=%d entries=%d err=%v", image, r.pending, len(r.entries), err)
			}
			if err := out.Send(ctx, "f", 1, &ownership.CallbackEvent{Sequence: 2, Kind: ownership.CallbackAssumption, Object: 7, Attributes: []core.AttributeHandle{1}}); err != nil {
				t.Fatalf("obsolete advice retained capacity: %v", err)
			}
		})
	}
}

func completedControlReviewFixture(t *testing.T) (*CallbackReceipts, *rtiv1.FederateEvent) {
	t.Helper()
	r, _, _ := requiredCallbackFixture(t, 4)
	data := []byte(`{"version":2,"deliveries":[{"recipient":1,"order":1,"event":null,"identity":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","phase":"completed"}]}`)
	pending, reservation, err := r.PrepareCheckpointReplay(context.Background(), "f", data, []core.RestoreParticipant{{SavedHandle: 1, CurrentHandle: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	reservation.Admit()
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	r.restoreCommitted("f", "saved", map[core.FederateHandle]core.FederateHandle{1: 1})
	wire, err := EncodeOutboundEvent(pending[1][0])
	if err != nil {
		t.Fatal(err)
	}
	return r, wire
}

func TestControlRecoveryIdentityOnlyCompletedRowDoesNotInventOutcome(t *testing.T) {
	r, _ := completedControlReviewFixture(t)
	data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	image, err := decodeCallbackCheckpoint(data)
	if err != nil || len(image.Deliveries) != 1 || image.Deliveries[0].Outcome != nil {
		t.Fatalf("invented outcome: %+v err=%v", image, err)
	}
}

func TestControlRecoveryInvalidCompletedResultFailsDecode(t *testing.T) {
	for _, result := range []string{`{"success":true,"exception":"contradiction"}`, `{"success":false,"exception":""}`} {
		data := []byte(`{"version":2,"deliveries":[{"recipient":1,"order":1,"event":null,"identity":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","phase":"completed","outcome":` + result + `}]}`)
		if _, err := decodeCallbackCheckpoint(data); err == nil {
			t.Fatalf("invalid result accepted: %s", result)
		}
	}
}

func TestControlRecoveryCompletedMarkerFailedSendAndReconnect(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(map[bool]string{false: "events", true: "batches"}[batched], func(t *testing.T) {
			r, marker := completedControlReviewFixture(t)
			failure := errors.New("lost completed-marker transport")
			if batched {
				stream := &receiptBatchesStream{StreamService_EventBatchesServer: &fakeBatchStream{ctx: context.Background(), sendErr: failure}, receipts: r, fed: "f", handle: 1}
				if err := stream.Send(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{marker}}); !errors.Is(err, failure) {
					t.Fatal(err)
				}
			} else {
				stream := &receiptEventsStream{StreamService_EventsServer: &fakeStream{ctx: context.Background(), sendErr: failure}, receipts: r, fed: "f", handle: 1}
				if err := stream.Send(marker); !errors.Is(err, failure) {
					t.Fatal(err)
				}
			}
			assertRetry := func() {
				t.Helper()
				pending := r.pendingRedelivery("f", 1)
				if len(pending) != 1 || !pending[0].GetCallbackRecovery().GetCompleted() || len(pending[0].CallbackReceipt) != 0 || pending[0].Event != nil {
					t.Fatalf("completed marker not retried safely: %v", pending)
				}
			}
			assertRetry()
			r.markDisconnected("f", 1)
			assertRetry()
			if r.pending != 0 {
				t.Fatal("marker consumes invocation capacity")
			}
		})
	}
}
