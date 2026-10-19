package m37spec

import (
	"context"
	"testing"
	gotime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

const retractFed = core.FederationName("m37_retraction")

// stubEvent is a minimal core.OutboundEvent for buffering.
type stubEvent struct{ seq uint64 }

func (s *stubEvent) Seq() uint64 { return s.seq }

// The historical test name is retained in the evidence inventory. Under 8.22,
// queued copies are canceled and already delivered copies require callbacks.
func TestSpec_M37_Retract_NotifiesWouldHaveReceived(t *testing.T) {
	ctx := context.Background()
	outbox := newFakeOutbox()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewManualClock(gotime.Unix(0, 0)),
		Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}

	if err := mgr.EnableRegulation(ctx, retractFed, 1, 1); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{2, 3, 4} {
		if err := mgr.EnableConstrained(ctx, retractFed, h); err != nil {
			t.Fatal(err)
		}
	}
	ts := core.LogicalTime(10)
	for _, id := range []uint64{42, 7} {
		admission, err := mgr.ReserveRetractionMessage(ctx, retractFed, 1, id, ts)
		if err != nil {
			t.Fatal(err)
		}
		if err := admission.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var deliveries []core.MessageDelivery
	for _, h := range []core.FederateHandle{2, 3, 5, 6} {
		deliveries = append(deliveries, core.MessageDelivery{Recipient: h, Timestamp: ts, Event: &stubEvent{}, Sender: 1, RetractionHandle: 42})
	}
	queue := mgr.ReserveTSO(retractFed, deliveries)
	for _, d := range queue.Immediate() {
		if err := outbox.Send(ctx, retractFed, d.Recipient, d.Event); err != nil {
			t.Fatal(err)
		}
	}
	queue.Commit(ctx)
	if err := mgr.BufferTSOWithRetraction(ctx, retractFed, 4, ts, &stubEvent{}, 1, 7); err != nil {
		t.Fatalf("BufferTSOWithRetraction(4): %v", err)
	}
	outbox.Reset()

	removed := mgr.RetractMessageNotify(ctx, retractFed, 1, 42)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}

	// Nonconstrained recipients 5 and 6 received the original sent-TSO copies.
	got := map[core.FederateHandle]int{}
	for _, rec := range outbox.Sent() {
		rr, ok := rec.Event.(*timepkg.RequestRetraction)
		if !ok {
			t.Fatalf("unexpected outbound event %T to federate %d", rec.Event, rec.Federate)
		}
		if rr.Sender != 1 || rr.RetractionHandle != 42 {
			t.Errorf("RequestRetraction = {sender=%d handle=%d}, want {1 42}", rr.Sender, rr.RetractionHandle)
		}
		got[rec.Federate]++
	}
	if got[5] != 1 || got[6] != 1 {
		t.Errorf("RequestRetraction recipients = %v, want delivered recipients 5 and 6", got)
	}

	// Unrelated entry survives: retracting handle 7 still removes it.
	if n := mgr.RetractMessageNotify(ctx, retractFed, 1, 7); n != 1 {
		t.Errorf("unrelated entry removed = %d, want 1 (buffer must have survived)", n)
	}
}

// TestSpec_M37_Retract_NoMatch_NoEvents: a retract that matches nothing
// (already delivered / wrong handle) removes nothing and emits nothing.
func TestSpec_M37_Retract_NoMatch_NoEvents(t *testing.T) {
	outbox := newFakeOutbox()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewManualClock(gotime.Unix(0, 0)),
		Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}
	if n := mgr.RetractMessageNotify(context.Background(), retractFed, 1, 99); n != 0 {
		t.Errorf("removed = %d, want 0", n)
	}
	if n := len(outbox.Sent()); n != 0 {
		t.Errorf("events = %d, want 0", n)
	}
}
