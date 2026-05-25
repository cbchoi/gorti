// M20.2 — RetractMessage unit tests against the time.Manager's
// TSO buffer.

package time

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeRetractEvent is a minimal core.OutboundEvent used to test
// retract semantics. The retract path doesn't inspect event content,
// so a no-op event suffices.
type fakeRetractEvent struct{ seq uint64 }

func (e *fakeRetractEvent) Seq() uint64 { return e.seq }

func TestRetractMessage_RemovesMatchingBufferedEvent(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	fed := core.FederationName("test")
	sender := core.FederateHandle(1)
	recipient := core.FederateHandle(2)
	const retractionHandle uint64 = 42

	// Buffer an event with retraction handle 42.
	if err := mgr.BufferTSOWithRetraction(
		ctx, fed, recipient, core.LogicalTime(5.0),
		&fakeRetractEvent{seq: 1}, sender, retractionHandle,
	); err != nil {
		t.Fatalf("BufferTSOWithRetraction: %v", err)
	}

	// Buffer another event with a different retraction handle.
	if err := mgr.BufferTSOWithRetraction(
		ctx, fed, recipient, core.LogicalTime(6.0),
		&fakeRetractEvent{seq: 2}, sender, 99,
	); err != nil {
		t.Fatalf("BufferTSOWithRetraction: %v", err)
	}

	// Sanity: LITS reflects min(5.0, 6.0) = 5.0
	lits, ok := mgr.QueryLITS(fed, recipient)
	if !ok || lits != 5.0 {
		t.Fatalf("pre-retract LITS = (%v, %v), want (5.0, true)", lits, ok)
	}

	// Retract the first message.
	removed := mgr.RetractMessage(fed, sender, retractionHandle)
	if removed != 1 {
		t.Errorf("RetractMessage removed %d events, want 1", removed)
	}

	// LITS now reflects min of remaining = 6.0
	lits, ok = mgr.QueryLITS(fed, recipient)
	if !ok || lits != 6.0 {
		t.Errorf("post-retract LITS = (%v, %v), want (6.0, true)", lits, ok)
	}
}

func TestRetractMessage_ZeroHandleNeverMatches(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	fed := core.FederationName("test")
	if err := mgr.BufferTSOWithRetraction(
		ctx, fed, 2, core.LogicalTime(5.0),
		&fakeRetractEvent{}, 1, 0,
	); err != nil {
		t.Fatalf("BufferTSOWithRetraction: %v", err)
	}
	// Zero handle never matches.
	if got := mgr.RetractMessage(fed, 1, 0); got != 0 {
		t.Errorf("RetractMessage(zero) removed %d, want 0", got)
	}
}

func TestRetractMessage_NonMatchingSenderLeavesBuffer(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	fed := core.FederationName("test")
	if err := mgr.BufferTSOWithRetraction(
		ctx, fed, 2, core.LogicalTime(5.0),
		&fakeRetractEvent{}, /*sender=*/ 7, /*handle=*/ 42,
	); err != nil {
		t.Fatalf("BufferTSOWithRetraction: %v", err)
	}
	// Wrong sender → no removal.
	if got := mgr.RetractMessage(fed, /*sender=*/ 99, 42); got != 0 {
		t.Errorf("RetractMessage(wrong sender) removed %d, want 0", got)
	}
	// Right sender → removed.
	if got := mgr.RetractMessage(fed, 7, 42); got != 1 {
		t.Errorf("RetractMessage(right sender) removed %d, want 1", got)
	}
}

func TestRetractMessage_MultipleRecipientsAllPurged(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	fed := core.FederationName("test")
	const sender core.FederateHandle = 1
	const handle uint64 = 42

	for _, rec := range []core.FederateHandle{2, 3, 4} {
		if err := mgr.BufferTSOWithRetraction(
			ctx, fed, rec, core.LogicalTime(5.0),
			&fakeRetractEvent{}, sender, handle,
		); err != nil {
			t.Fatalf("BufferTSO recipient %d: %v", rec, err)
		}
	}
	// One retract should clear all 3 recipients' buffers of this msg.
	if got := mgr.RetractMessage(fed, sender, handle); got != 3 {
		t.Errorf("RetractMessage cross-recipient removed %d, want 3", got)
	}
}
