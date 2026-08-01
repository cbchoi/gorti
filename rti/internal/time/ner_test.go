package time

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// recordingOutbox is a goroutine-safe Outbox capturing every grant for
// in-package assertion. Mirrors the spec-package fakeOutbox but stays
// in this package so tests can use it without an import cycle.
type recordingOutbox struct {
	mu    sync.Mutex
	sends []recordedSend
}

type recordedSend struct {
	fed core.FederationName
	h   core.FederateHandle
	t   core.LogicalTime
}

func (r *recordingOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	g, ok := evt.(*TimeAdvanceGrant)
	if !ok {
		return errors.New("recordingOutbox: unexpected event type")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sends = append(r.sends, recordedSend{fed: fed, h: h, t: g.Time})
	return nil
}

func (r *recordingOutbox) snapshot() []recordedSend {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedSend, len(r.sends))
	copy(out, r.sends)
	return out
}

// M36 DB-1: the pre-M36 checkLookahead floored the advance target at
// currentTime + lookahead, which violates IEEE 1516.1 §8.8 (lookahead
// constrains outgoing TSO timestamps, not the advance target). The
// three tests below cover the corrected checkAdvanceTarget semantics.

// TestCheckAdvanceTarget_AcceptsCurrentTime: a request to the
// federate's exact current time is legal (equality granted).
func TestCheckAdvanceTarget_AcceptsCurrentTime(t *testing.T) {
	if err := checkAdvanceTarget(2.0, 2.0); err != nil {
		t.Errorf("self-time target: err = %v, want nil", err)
	}
}

// TestCheckAdvanceTarget_AcceptsBelowLookaheadFloor: a target above
// currentTime but below the OLD currentTime + lookahead floor is legal
// — the fixture-exposed case (advance to 1.0 with lookahead 2.0).
func TestCheckAdvanceTarget_AcceptsBelowLookaheadFloor(t *testing.T) {
	if err := checkAdvanceTarget(0.0, 1.0); err != nil {
		t.Errorf("target below old lookahead floor: err = %v, want nil", err)
	}
}

// TestCheckAdvanceTarget_RejectsPast: a target strictly below
// currentTime returns ErrTimeRequestInPast.
func TestCheckAdvanceTarget_RejectsPast(t *testing.T) {
	err := checkAdvanceTarget(2.0, 1.5)
	if !errors.Is(err, core.ErrTimeRequestInPast) {
		t.Errorf("target in past: err = %v, want ErrTimeRequestInPast", err)
	}
}

// TestNERStore_ExtOf_IsPerManager: extOf returns distinct stores for
// distinct *Manager values, and the same store on repeat lookups.
func TestNERStore_ExtOf_IsPerManager(t *testing.T) {
	mgrA, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	mgrB, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	if extOf(mgrA) == extOf(mgrB) {
		t.Errorf("distinct managers share an ext store")
	}
	first := extOf(mgrA)
	second := extOf(mgrA)
	if first != second {
		t.Errorf("repeat lookup on same manager produced different stores")
	}
}

// TestNER_BatchedGrants_HandleSorted: when a single NER call satisfies
// multiple pending federates' requested times (because the new pending
// contribution raises LBTS above the requested floor), grants are
// emitted in handle-sorted order, not call-arrival order. Mirrors the
// determinism contract NFR-DET-1 in a closer-to-the-metal scenario
// than the spec test.
func TestNER_BatchedGrants_HandleSorted(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{
		Clock:  core.NewFakeClock(zeroTime()),
		Outbox: out,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable fed1: %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable fed2: %v", err)
	}

	// First NER for fed1: LBTS=1 (fed1 pending → 1+1=2; fed2 not → 0+1=1;
	// min=1). 1>1 false → no full grant. Forced-grant requires LBTS<req
	// (1<1 false) → no forced grant either. fed1 pending, no emission.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(1)); err != nil {
		t.Fatalf("NER fed1: %v", err)
	}
	if got := out.snapshot(); len(got) != 0 {
		t.Fatalf("after fed1 NER alone: grants = %+v, want 0", got)
	}

	// Second NER for fed2: now both pending, both contribute 1+1=2,
	// LBTS=2. 2>1 ✓ → full grants for both in handle order [1,2].
	if err := mgr.NextMessageRequest(ctx, "fed", 2, core.LogicalTime(1)); err != nil {
		t.Fatalf("NER fed2: %v", err)
	}
	sends := out.snapshot()
	if len(sends) != 2 {
		t.Fatalf("after fed2 NER: grants = %+v, want exactly 2", sends)
	}
	if sends[0].h != 1 || sends[1].h != 2 {
		t.Errorf("grant order = [%d, %d], want [1, 2]", sends[0].h, sends[1].h)
	}
}

// TestNER_RegulatingSnapshot_PendingPromotesFloor: a pending federate's
// contribution to LBTS uses requestedTime+lookahead (not
// currentTime+lookahead), per the HLA NER promise.
func TestNER_RegulatingSnapshot_PendingPromotesFloor(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// No NER yet: snapshot uses currentTime=0, lookahead=1 → time field 0.
	pre := mgr.regulatingSnapshot("fed")
	if len(pre) != 1 || pre[0].Time != core.LogicalTime(0) {
		t.Fatalf("pre-NER snapshot = %+v, want one entry with Time=0", pre)
	}

	// Inject a pending NER directly into the side-table without
	// running the full NER path (which would resolve the grant).
	ext := extOf(mgr)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("fed", 1)
	ns.pendingNER = true
	ns.requestedTime = core.LogicalTime(7)
	ext.mu.Unlock()

	post := mgr.regulatingSnapshot("fed")
	if len(post) != 1 || post[0].Time != core.LogicalTime(7) {
		t.Fatalf("post-NER snapshot = %+v, want one entry with Time=7 (requested)", post)
	}
}

// TestEmitGrant_NilEventLog_DropsSilently: cut-1 relaxation — when
// Options.EventLog is nil, emitGrant skips the Append step rather than
// panicking on a nil interface.
func TestEmitGrant_NilEventLog_DropsSilently(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{
		Clock:  core.NewFakeClock(zeroTime()),
		Outbox: out,
		// EventLog: nil — exercise the cut-1 relaxation branch.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// Sole regulator: full grant at requested=5 fires immediately.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5)); err != nil {
		t.Fatalf("NER: %v", err)
	}
	if got := out.snapshot(); len(got) != 1 || got[0].t != core.LogicalTime(5) {
		t.Errorf("with nil EventLog, grants = %+v, want one grant at t=5", got)
	}
}
