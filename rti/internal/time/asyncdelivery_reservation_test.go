package time

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestReserveTSOClassifiesMixedTimeEngagementInOneBatch(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := mgr.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}

	reservation := mgr.ReserveTSO("fed", []core.TSOBufferedDelivery{
		{Recipient: 2, Timestamp: 5, Event: &fakeRetractEvent{seq: 1}},
		{Recipient: 3, Timestamp: 5, Event: &fakeRetractEvent{seq: 2}},
	})
	immediate := reservation.Immediate()
	buffered := reservation.Buffered()
	if len(immediate) != 1 || immediate[0].Recipient != 3 {
		reservation.Release()
		t.Fatalf("immediate deliveries = %+v, want only non-time-engaged federate 3", immediate)
	}
	if len(buffered) != 1 || buffered[0].Recipient != 2 {
		reservation.Release()
		t.Fatalf("buffered deliveries = %+v, want only time-engaged federate 2", buffered)
	}

	reservation.Commit(ctx)
	if logicalTime, ok := mgr.QueryLITS("fed", 2); !ok || logicalTime != 5 {
		t.Fatalf("time-engaged LITS = (%v, %v), want (5, true)", logicalTime, ok)
	}
	if logicalTime, ok := mgr.QueryLITS("fed", 3); ok {
		t.Fatalf("non-time-engaged LITS = (%v, true), want no buffered event", logicalTime)
	}
}
