package object

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestIndependentNameReviewCallbackAdmissionFailureIsAtomic(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		mode := "single"
		if multiple {
			mode = "multiple"
		}
		for _, full := range []bool{false, true} {
			failure := "unavailable"
			if full {
				failure = "capacity"
			}
			t.Run(mode+"/"+failure, func(t *testing.T) {
				ctx := context.Background()
				const fed core.FederationName = "atomic-reservation"
				outbox := delivery.NewWithBatch(1, 1, 0)
				registry, _ := newNoWALRegistry(t, outbox)
				var events <-chan []core.OutboundEvent
				subscribe := func() {
					t.Helper()
					var closeSubscription func() error
					var err error
					events, closeSubscription, err = outbox.Subscribe(ctx, fed, 1)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = closeSubscription() })
				}
				marker := &CallbackEvent{Sequence: 99, Kind: CallbackReservationSucceeded, ObjectName: "unrelated-marker"}
				wantErr := core.ErrOutboxUnavailable
				if full {
					subscribe()
					if err := outbox.Send(ctx, fed, 1, marker); err != nil {
						t.Fatal(err)
					}
					wantErr = core.ErrFederateOverflow
				}
				reserve := func() error {
					if multiple {
						return registry.ReserveMultipleObjectInstanceNames(ctx, fed, 1, []string{"first", "second"})
					}
					return registry.ReserveObjectInstanceName(ctx, fed, 1, "first")
				}
				before := registry.ExportState(fed)
				if err := reserve(); !errors.Is(err, wantErr) {
					t.Fatalf("reserve error = %v, want %v", err, wantErr)
				}
				after := registry.ExportState(fed)
				if !reflect.DeepEqual(before, after) {
					t.Errorf("rejected callback changed registry state: reservations %v -> %v; sequence %d -> %d", before.Reservations, after.Reservations, before.LastOutboundSequence, after.LastOutboundSequence)
				}
				if full {
					select {
					case batch := <-events:
						if len(batch) != 1 || batch[0] != marker {
							t.Fatalf("pre-existing queue entry was changed: %v", batch)
						}
					case <-time.After(time.Second):
						t.Fatal("pre-existing queue entry was lost")
					}
				} else {
					subscribe()
				}
				select {
				case batch := <-events:
					t.Fatalf("rejected operation left a callback: %v", batch)
				default:
				}
				if err := reserve(); err != nil {
					t.Fatalf("retry after capacity recovery: %v", err)
				}
				select {
				case batch := <-events:
					if len(batch) != 1 {
						t.Fatalf("retry callbacks = %d, want 1", len(batch))
					}
					event, ok := batch[0].(*CallbackEvent)
					if !ok {
						t.Fatalf("retry callback type = %T", batch[0])
					}
					wantKind := CallbackReservationSucceeded
					if multiple {
						wantKind = CallbackMultipleReservationSucceeded
					}
					if event.Kind != wantKind || event.Sequence != before.LastOutboundSequence+1 {
						t.Errorf("retry callback kind/sequence = %v/%d, want %v/%d", event.Kind, event.Sequence, wantKind, before.LastOutboundSequence+1)
					}
				case <-time.After(time.Second):
					t.Fatal("successful retry has no callback")
				}
			})
		}
	}
}
