package delivery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var errGuardExpired = errors.New("test callback epoch expired")

type testEventGuard struct{ expired atomic.Bool }

func (g *testEventGuard) Validate() error {
	if g.expired.Load() {
		return errGuardExpired
	}
	return nil
}

func TestGuardedEventStaleAdmissionIsAtomic(t *testing.T) {
	for _, reserve := range []bool{false, true} {
		name := "send"
		if reserve {
			name = "fanout"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			out := NewWithBatch(4, 1, 0)
			var queues []<-chan []core.OutboundEvent
			for _, recipient := range []core.FederateHandle{1, 2} {
				queue, closeQueue, err := out.Subscribe(ctx, "f", recipient)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = closeQueue() })
				queues = append(queues, queue)
			}
			guard := &testEventGuard{}
			guard.expired.Store(true)
			stale := GuardEvent(&fakeOutboundEvent{seq: 2}, guard)
			if reserve {
				reservation, err := out.Reserve(ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: &fakeOutboundEvent{seq: 1}}, {Recipient: 2, Event: stale}})
				if !errors.Is(err, errGuardExpired) || reservation != nil {
					t.Fatalf("stale fanout admitted: %v", err)
				}
			} else if err := out.Send(ctx, "f", 1, stale); !errors.Is(err, errGuardExpired) {
				t.Fatalf("stale send admitted: %v", err)
			}
			for i, queue := range queues {
				select {
				case events := <-queue:
					t.Fatalf("recipient %d received partial admission: %v", i+1, events)
				default:
				}
				if err := out.Send(ctx, "f", core.FederateHandle(i+1), &fakeOutboundEvent{seq: 3}); err != nil {
					t.Fatalf("rejected guard leaked capacity or lock: %v", err)
				}
			}
		})
	}
}

func TestGuardedEventCommitRetainsWholeFanoutAfterInvalidation(t *testing.T) {
	for _, recipients := range []int{1, 2} {
		ctx := context.Background()
		out := NewWithBatch(4, 1, 0)
		guard := &testEventGuard{}
		var queues []<-chan []core.OutboundEvent
		var deliveries []core.OutboxDelivery
		for i := 1; i <= recipients; i++ {
			queue, closeQueue, err := out.Subscribe(ctx, "f", core.FederateHandle(i))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = closeQueue() })
			queues = append(queues, queue)
			event := GuardEvent(&fakeOutboundEvent{seq: uint64(i)}, guard)
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: core.FederateHandle(i), Event: event})
		}
		reservation, err := out.Reserve(ctx, "f", deliveries)
		if err != nil {
			t.Fatal(err)
		}
		guard.expired.Store(true)
		if err := reservation.Commit(); err != nil {
			t.Fatalf("accepted fanout failed after domain commit: %v", err)
		}
		if err := reservation.Commit(); err != nil {
			t.Fatal(err)
		}
		reservation.Release()
		for i, queue := range queues {
			select {
			case events := <-queue:
				if len(events) != 1 || events[0] != deliveries[i].Event || events[0].Seq() != uint64(i+1) {
					t.Fatalf("admitted event was replaced or lost: %v", events)
				}
				if resolved, err := ResolveEvent(events[0]); !errors.Is(err, errGuardExpired) || resolved != nil {
					t.Fatalf("queue consumer resolved stale coordinates: %v %v", resolved, err)
				}
			default:
				t.Fatal("commit silently dropped an admitted event")
			}
			select {
			case <-queue:
				t.Fatal("repeated commit duplicated callback")
			default:
			}
		}
	}
}

func TestGuardedEventResolvePreservesLivePayload(t *testing.T) {
	source := &fakeOutboundEvent{seq: 4}
	guard := &testEventGuard{}
	event := GuardEvent(GuardEvent(source, guard), guard)
	resolved, err := ResolveEvent(event)
	if err != nil || resolved != source || event.Seq() != 4 {
		t.Fatalf("resolved=%v error=%v", resolved, err)
	}
	if _, err := ResolveEvent(GuardEvent(source, nil)); err == nil {
		t.Fatal("missing guard accepted")
	}
}
