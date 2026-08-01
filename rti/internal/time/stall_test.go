package time

import (
	"context"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestFederationMembers_UnionRegulationAndNER: a federate that has
// only NER'd (no EnableRegulation visible to stateStore yet) is still
// a member for halt-fan-out purposes; symmetrically, a regulating
// federate with no NER is also a member. The output is handle-sorted.
func TestFederationMembers_UnionRegulationAndNER(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 3, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable 3: %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable 1: %v", err)
	}
	// Inject a federate purely via NER state-table to simulate a
	// constrained-only peer that lives only in nerStore.
	ext := extOf(mgr)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("fed", 2)
	ns.pendingNER = true
	ns.pendingSince = stdtime.Unix(0, 0)
	ext.mu.Unlock()

	got := mgr.federationMembers("fed")
	if len(got) != 3 {
		t.Fatalf("members = %+v, want 3 entries", got)
	}
	for i := 0; i < len(got)-1; i++ {
		if got[i] >= got[i+1] {
			t.Errorf("members not handle-sorted: %+v", got)
		}
	}
}

// TestMarkHalted_Idempotent: marking the same federation halted twice
// is a clean no-op (both checks see halted=true).
func TestMarkHalted_Idempotent(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ext := extOf(mgr)
	ext.markHalted("fed")
	ext.markHalted("fed")
	if !ext.isHalted("fed") {
		t.Errorf("isHalted = false, want true after markHalted")
	}
	if ext.isHalted("other") {
		t.Errorf("isHalted other federation = true, want false")
	}
}

// TestCheckStalls_HaltEmitsOnePerFederateInFederation: the fan-out
// sends exactly one FederationHalted to every member, in handle-
// sorted order, and emits exactly one event-log record per halt.
func TestCheckStalls_HaltEmitsOnePerFederateInFederation(t *testing.T) {
	clk := core.NewFakeClock(zeroTime())
	out := &recordingHaltOutbox{}
	log := &recordingHaltLog{}
	mgr, err := New(Options{
		Clock:        clk,
		Outbox:       out,
		EventLog:     log,
		StallTimeout: 5 * stdtime.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 5, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable 5: %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable 2: %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 9, core.LogicalTime(1)); err != nil {
		t.Fatalf("enable 9: %v", err)
	}
	// fed5 NERs (will be the stall trigger — only it has pendingSince).
	if err := mgr.NextMessageRequest(ctx, "fed", 5, core.LogicalTime(10)); err != nil {
		t.Fatalf("NER 5: %v", err)
	}
	clk.Advance(10 * stdtime.Second)
	if h := mgr.CheckStalls(ctx); h != 1 {
		t.Fatalf("CheckStalls = %d, want 1", h)
	}

	// One log record (per-halt write-ahead).
	if got := log.snapshot(); len(got) != 1 {
		t.Fatalf("log records = %d, want 1", len(got))
	}
	rec := log.snapshot()[0]
	if rec.Cause != HaltCauseStall || rec.StalledFederate != 5 {
		t.Errorf("log rec = %+v, want stall cause + stalled 5", rec)
	}

	// Three outbox sends, one per member, in handle-sorted order.
	sends := out.snapshot()
	if len(sends) != 3 {
		t.Fatalf("outbox sends = %d, want 3", len(sends))
	}
	wantHandles := []core.FederateHandle{2, 5, 9}
	for i, s := range sends {
		if s.h != wantHandles[i] {
			t.Errorf("send[%d] handle = %d, want %d", i, s.h, wantHandles[i])
		}
		if s.cause != HaltCauseStall || s.stalled != 5 {
			t.Errorf("send[%d] payload = %+v, want stall+5", i, s)
		}
	}
}

// TestCheckStalls_TwiceIdempotent: a second CheckStalls after a halt
// returns 0 (the federation is in the halted set and is skipped).
func TestCheckStalls_TwiceIdempotent(t *testing.T) {
	clk := core.NewFakeClock(zeroTime())
	out := &recordingHaltOutbox{}
	mgr, err := New(Options{
		Clock:        clk,
		Outbox:       out,
		StallTimeout: 5 * stdtime.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1))
	_ = mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(10))
	clk.Advance(10 * stdtime.Second)
	if h := mgr.CheckStalls(ctx); h != 1 {
		t.Fatalf("first CheckStalls = %d, want 1", h)
	}
	if h := mgr.CheckStalls(ctx); h != 0 {
		t.Errorf("second CheckStalls = %d, want 0 (halted set is sticky)", h)
	}
}

// TestHaltedFederation_RejectsAllStateMutations: every state-mutating
// Manager method on a halted federation returns ErrFederationHalted.
func TestHaltedFederation_RejectsAllStateMutations(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ext := extOf(mgr)
	ext.markHalted("fed")
	ctx := context.Background()
	if e := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1)); !errors.Is(e, core.ErrFederationHalted) {
		t.Errorf("EnableRegulation: err = %v, want ErrFederationHalted", e)
	}
	if e := mgr.DisableRegulation(ctx, "fed", 1); !errors.Is(e, core.ErrFederationHalted) {
		t.Errorf("DisableRegulation: err = %v, want ErrFederationHalted", e)
	}
	if e := mgr.EnableConstrained(ctx, "fed", 1); !errors.Is(e, core.ErrFederationHalted) {
		t.Errorf("EnableConstrained: err = %v, want ErrFederationHalted", e)
	}
	if e := mgr.DisableConstrained(ctx, "fed", 1); !errors.Is(e, core.ErrFederationHalted) {
		t.Errorf("DisableConstrained: err = %v, want ErrFederationHalted", e)
	}
	if e := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(2)); !errors.Is(e, core.ErrFederationHalted) {
		t.Errorf("NER: err = %v, want ErrFederationHalted", e)
	}
}

// recordingHaltOutbox captures FederationHalted sends for assertion.
// Distinct from recordingOutbox in ner_test.go because that fixture
// type-asserts to *TimeAdvanceGrant; here we accept both grant and
// halt events but only record the latter.
type recordingHaltOutbox struct {
	recordingOutbox
	halts []recordedHaltSend
}

type recordedHaltSend struct {
	fed     core.FederationName
	h       core.FederateHandle
	cause   string
	stalled core.FederateHandle
}

func (r *recordingHaltOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	if hev, ok := evt.(*FederationHalted); ok {
		r.recordingOutbox.mu.Lock()
		r.halts = append(r.halts, recordedHaltSend{fed: fed, h: h, cause: hev.Cause, stalled: hev.StalledFederate})
		r.recordingOutbox.mu.Unlock()
		return nil
	}
	// Delegate grant events to the embedded recording outbox.
	return r.recordingOutbox.Send(ctx, fed, h, evt)
}

func (r *recordingHaltOutbox) snapshot() []recordedHaltSend {
	r.recordingOutbox.mu.Lock()
	defer r.recordingOutbox.mu.Unlock()
	out := make([]recordedHaltSend, len(r.halts))
	copy(out, r.halts)
	return out
}

// recordingHaltLog captures federationHaltedRecord appends.
type recordingHaltLog struct {
	recs []*federationHaltedRecord
}

func (l *recordingHaltLog) Append(_ context.Context, _ core.FederationName, evt core.EventRecord) error {
	if r, ok := evt.(*federationHaltedRecord); ok {
		l.recs = append(l.recs, r)
	}
	return nil
}

func (*recordingHaltLog) Sync(_ context.Context, _ core.FederationName) error { return nil }

func (*recordingHaltLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("recordingHaltLog: OpenReader not supported")
}

func (l *recordingHaltLog) snapshot() []*federationHaltedRecord {
	out := make([]*federationHaltedRecord, len(l.recs))
	copy(out, l.recs)
	return out
}
