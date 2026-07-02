package m37spec

import (
	"context"
	"testing"
	gotime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

const retractFed = core.FederationName("m37_retraction")

// stubEvent is a minimal core.OutboundEvent for buffering.
type stubEvent struct{ seq uint64 }

func (s *stubEvent) Seq() uint64 { return s.seq }

// TestSpec_M37_Retract_NotifiesWouldHaveReceived: IEEE 1516.1-2010
// §8.22 — retracting a buffered TSO message removes it from every
// recipient's buffer AND fires RequestRetraction on each federate that
// WOULD have received it. Unrelated buffered messages are untouched
// and their recipients hear nothing.
func TestSpec_M37_Retract_NotifiesWouldHaveReceived(t *testing.T) {
	ctx := context.Background()
	outbox := newFakeOutbox()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewFakeClock(gotime.Unix(0, 0)),
		Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}

	// Sender = federate 1, retraction handle 42, buffered for
	// recipients 2 and 3. Recipient 4 holds an UNRELATED entry
	// (handle 7).
	ts := core.LogicalTime(10)
	for _, h := range []core.FederateHandle{2, 3} {
		if err := mgr.BufferTSOWithRetraction(ctx, retractFed, h, ts, &stubEvent{}, 1, 42); err != nil {
			t.Fatalf("BufferTSOWithRetraction(%d): %v", h, err)
		}
	}
	if err := mgr.BufferTSOWithRetraction(ctx, retractFed, 4, ts, &stubEvent{}, 1, 7); err != nil {
		t.Fatalf("BufferTSOWithRetraction(4): %v", err)
	}

	removed := mgr.RetractMessageNotify(ctx, retractFed, 1, 42)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}

	// Recipients 2 and 3 each hear exactly one RequestRetraction.
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
	if got[2] != 1 || got[3] != 1 || len(got) != 2 {
		t.Errorf("RequestRetraction recipients = %v, want exactly {2:1, 3:1}", got)
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
		Clock:  core.NewFakeClock(gotime.Unix(0, 0)),
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
