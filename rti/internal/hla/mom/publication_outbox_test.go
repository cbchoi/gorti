package mom_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestMOMPublicationOutboxReservationsStageCommitAndRelease(t *testing.T) {
	elapsed := &elapsedPublicationOutbox{}
	catalog := &catalogOutbox{}
	for _, fixture := range []struct {
		name   string
		out    core.ReservableOutbox
		events func() []core.OutboundEvent
	}{
		{"elapsed", elapsed, func() []core.OutboundEvent {
			var events []core.OutboundEvent
			for _, event := range elapsed.events {
				events = append(events, event)
			}
			return events
		}},
		{"catalog", catalog, func() []core.OutboundEvent {
			var events []core.OutboundEvent
			for _, delivery := range catalog.sent {
				if delivery.fed != "publication" || delivery.recipient != 7 {
					t.Fatalf("reservation lost routing identity: %+v", delivery)
				}
				events = append(events, delivery.event)
			}
			return events
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			scope := &object.CallbackEvent{Kind: object.CallbackAttributesInScope}
			reflection := &object.CallbackEvent{Kind: object.CallbackReflect}
			deliveries := []core.OutboxDelivery{{Recipient: 7, Event: scope}, {Recipient: 7, Event: reflection}}
			reserve := func() core.OutboxReservation {
				t.Helper()
				r, err := fixture.out.Reserve(context.Background(), "publication", deliveries)
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			discarded := reserve()
			if len(fixture.events()) != 0 {
				t.Fatal("reserve published callbacks before commit")
			}
			discarded.Release()
			if err := discarded.Commit(); err != nil || len(fixture.events()) != 0 {
				t.Fatalf("released reservation published callbacks: %v", err)
			}
			committed := reserve()
			deliveries[0] = core.OutboxDelivery{Recipient: 99, Event: reflection}
			for i := 0; i < 2; i++ {
				if err := committed.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			committed.Release()
			got := fixture.events()
			if len(got) != 2 || got[0] != scope || got[1] != reflection {
				t.Fatalf("reservation changed, duplicated, or reordered callbacks: %+v", got)
			}
		})
	}
	if !elapsed.mu.TryLock() {
		t.Fatal("elapsed fixture leaked its reservation lock")
	}
	elapsed.mu.Unlock()
}
