package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

func TestReservationPressureDistinguishesOversizedFanout(t *testing.T) {
	box := NewWithBatch(1, 1, 0)
	box.Bind("f", 1)
	box.Bind("f", 2)
	defer box.UnbindFederation("f")
	ctx := context.Background()
	event := &syncpkg.SynchronizedEvent{}
	if err := box.Send(ctx, "f", 2, event); err != nil {
		t.Fatal(err)
	}
	_, err := box.Reserve(ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: event}, {Recipient: 2, Event: event}})
	var pressure *ReservationPressure
	if !errors.As(err, &pressure) || !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("transient fanout pressure=%v", err)
	}
	for _, stat := range box.OutboxStats() {
		if stat.Handle == 1 && stat.QueueDepth != 0 {
			t.Fatal("partial fanout")
		}
	}
	_, err = box.Reserve(ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: event}, {Recipient: 1, Event: event}})
	if !errors.Is(err, core.ErrFederateOverflow) || errors.As(err, &pressure) {
		t.Fatalf("oversized transfer is retryable: %v", err)
	}
}
