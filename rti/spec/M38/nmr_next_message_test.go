// Package m38spec — M38 Agent GA acceptance tests: §8.8/§8.9
// nextMessageRequest(-Available) grant semantics.
//
// IEEE 1516.1-2010 §8.8 (nextMessageRequest): the RTI shall grant the
// advance at min(requested t, timestamp of the next TSO message that
// will be delivered to the joined federate), delivering the messages at
// the grant time BEFORE the grant (§8.14). When no deliverable TSO
// message exists and the LBTS over the regulating set does not yet
// cover the requested time, the request stays PENDING — the spec
// defines no interim grant callback. §8.9 (nextMessageRequestAvailable)
// is identical except the LBTS comparison at the grant time is
// inclusive (messages at exactly the grant time may still arrive after
// the grant).
//
// These tests retire gorti's pre-M38 interim semantics ("sole-pending
// NER/NMRA force-granted at LBTS keeping pending", advance.go
// decideGrant), which emitted a spec-invisible extra grant — the
// tm_tso_ordering extra `GRANT time=1.000000` and the IVCT
// tc_time_regulation NER xfail both trace to it.
package m38spec

import (
	"context"
	"errors"
	"sync"
	"testing"
	gotime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

const nmrFed = core.FederationName("m38_nmr_next_message")

// fakeOutbox records every Send in arrival order. Goroutine-safe.
type fakeOutbox struct {
	mu   sync.Mutex
	sent []sentRecord
}

type sentRecord struct {
	Federate core.FederateHandle
	Event    core.OutboundEvent
}

func newFakeOutbox() *fakeOutbox { return &fakeOutbox{} }

func (o *fakeOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{Federate: h, Event: evt})
	return nil
}

func (o *fakeOutbox) Sent() []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]sentRecord, len(o.sent))
	copy(out, o.sent)
	return out
}

// grantsTo filters the recording down to TimeAdvanceGrant events for h.
func (o *fakeOutbox) grantsTo(h core.FederateHandle) []*timepkg.TimeAdvanceGrant {
	var out []*timepkg.TimeAdvanceGrant
	for _, rec := range o.Sent() {
		if rec.Federate != h {
			continue
		}
		if g, ok := rec.Event.(*timepkg.TimeAdvanceGrant); ok {
			out = append(out, g)
		}
	}
	return out
}

// eventsTo returns the ordered per-recipient event stream for h.
func (o *fakeOutbox) eventsTo(h core.FederateHandle) []core.OutboundEvent {
	var out []core.OutboundEvent
	for _, rec := range o.Sent() {
		if rec.Federate == h {
			out = append(out, rec.Event)
		}
	}
	return out
}

// stubTSO is a minimal core.OutboundEvent standing in for a buffered
// TSO reflect/interaction on the wire.
type stubTSO struct{ seq uint64 }

func (e stubTSO) Seq() uint64 { return e.seq }

func newManager(t *testing.T) (*timepkg.Manager, *fakeOutbox) {
	t.Helper()
	outbox := newFakeOutbox()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewFakeClock(gotime.Unix(0, 0)),
		Outbox: outbox,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}
	return mgr, outbox
}

// TestSpec_M38_NMR_NoTSO_NoInterimGrant — §8.8: NMR(2.0) with
// LBTS = 1.0 (one idle regulator at t=0, lookahead 1.0) and NO queued
// TSO message stays pending with NO grant callback of any kind. The
// pre-M38 forced-grant hatch emitted GRANT(1.0) here.
func TestSpec_M38_NMR_NoTSO_NoInterimGrant(t *testing.T) {
	ctx := context.Background()
	mgr, outbox := newManager(t)

	if err := mgr.EnableRegulation(ctx, nmrFed, 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, nmrFed, 2); err != nil {
		t.Fatalf("EnableConstrained(2): %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, nmrFed, 2, 2.0); err != nil {
		t.Fatalf("NMR(2, 2.0): %v", err)
	}

	if got := outbox.Sent(); len(got) != 0 {
		t.Fatalf("§8.8: NMR(2.0) with LBTS=1.0 and no queued TSO must stay pending; outbox = %v, want empty", got)
	}
	// The request is still outstanding: a second advance call is the
	// §8.8 "in time advancing state" error.
	if err := mgr.NextMessageRequest(ctx, nmrFed, 2, 2.0); !errors.Is(err, core.ErrTimeAdvancingState) {
		t.Fatalf("second NMR while pending = %v, want ErrTimeAdvancingState", err)
	}
}

// TestSpec_M38_NMR_GrantAtNextTSOTime_DeliveryFirst — §8.8 + §8.14:
// with a TSO message stamped 1.0 buffered for the requester, the
// pending NMR(2.0) is granted at min(2.0, 1.0) = 1.0 as soon as LBTS
// rises strictly above 1.0 (the regulator's own NMR(1.0) promotes its
// floor to 1.0 + 1.0 = 2.0), and the message precedes the grant on the
// wire. The grant COMPLETES the request (§8.8: one request, one grant)
// — pending is cleared, so a follow-up NMR is legal.
func TestSpec_M38_NMR_GrantAtNextTSOTime_DeliveryFirst(t *testing.T) {
	ctx := context.Background()
	mgr, outbox := newManager(t)

	if err := mgr.EnableRegulation(ctx, nmrFed, 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, nmrFed, 2); err != nil {
		t.Fatalf("EnableConstrained(2): %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, nmrFed, 2, 2.0); err != nil {
		t.Fatalf("NMR(2, 2.0): %v", err)
	}

	// TSO message stamped 1.0 arrives for the pending requester. LBTS
	// is still exactly 1.0: another regulator send at exactly 1.0
	// remains possible, so §8.8's "all messages at the grant time
	// delivered before the grant" cannot yet be guaranteed — hold.
	if err := mgr.BufferTSO(ctx, nmrFed, 2, 1.0, stubTSO{seq: 1}); err != nil {
		t.Fatalf("BufferTSO: %v", err)
	}
	if got := outbox.grantsTo(2); len(got) != 0 {
		t.Fatalf("grant fired at LBTS == message time (%v); §8.8 needs LBTS strictly above the grant time", got)
	}

	// Regulator NMR(1.0): its LBTS floor promotes to requested +
	// lookahead = 2.0 > 1.0 — the requester's grant fires at the
	// message time.
	if err := mgr.NextMessageRequest(ctx, nmrFed, 1, 1.0); err != nil {
		t.Fatalf("NMR(1, 1.0): %v", err)
	}

	grants := outbox.grantsTo(2)
	if len(grants) != 1 {
		t.Fatalf("requester received %d grants %v, want exactly 1", len(grants), grants)
	}
	if ft := float64(grants[0].Time); ft != 1.0 {
		t.Errorf("§8.8: grant time = %v, want 1.0 (min(requested 2.0, next TSO 1.0))", ft)
	}
	// §8.14 — the buffered TSO event precedes the grant on the wire.
	stream := outbox.eventsTo(2)
	if len(stream) != 2 {
		t.Fatalf("requester stream = %v, want [TSO event, grant]", stream)
	}
	if _, ok := stream[0].(stubTSO); !ok {
		t.Errorf("§8.14: first wire event = %T, want the buffered TSO event before the grant", stream[0])
	}
	if _, ok := stream[1].(*timepkg.TimeAdvanceGrant); !ok {
		t.Errorf("second wire event = %T, want the TimeAdvanceGrant", stream[1])
	}
	// Pending cleared: a fresh NMR is legal (no ErrTimeAdvancingState).
	if err := mgr.NextMessageRequest(ctx, nmrFed, 2, 2.0); err != nil {
		t.Fatalf("§8.8: grant at message time completes the request; follow-up NMR = %v, want nil", err)
	}
}

// TestSpec_M38_NMR_TSOArrivalUnblocksPendingRequest — the IVCT
// tc_time_regulation shape (test_tc010_ner_delivers_tso_before_grant):
// regulator TARs to 10 (LBTS 11), constrained NMR(20) stays pending
// (LBTS < requested, queue empty), then a TSO message stamped 5.0
// arrives — its buffering must re-evaluate the pending request and
// grant at 5.0 with the message delivered first.
func TestSpec_M38_NMR_TSOArrivalUnblocksPendingRequest(t *testing.T) {
	ctx := context.Background()
	mgr, outbox := newManager(t)

	if err := mgr.EnableRegulation(ctx, nmrFed, 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, nmrFed, 2); err != nil {
		t.Fatalf("EnableConstrained(2): %v", err)
	}
	// Regulator advances to 10 (grants itself; LBTS becomes 11).
	if err := mgr.TimeAdvanceRequest(ctx, nmrFed, 1, 10.0); err != nil {
		t.Fatalf("TAR(1, 10.0): %v", err)
	}

	if err := mgr.NextMessageRequest(ctx, nmrFed, 2, 20.0); err != nil {
		t.Fatalf("NMR(2, 20.0): %v", err)
	}
	if got := outbox.grantsTo(2); len(got) != 0 {
		t.Fatalf("§8.8: NMR(20) with LBTS=11 and no queued TSO must stay pending; grants = %v", got)
	}

	// TSO message stamped 5.0 arrives → grant at 5.0 (LBTS 11 > 5),
	// message first.
	if err := mgr.BufferTSO(ctx, nmrFed, 2, 5.0, stubTSO{seq: 7}); err != nil {
		t.Fatalf("BufferTSO: %v", err)
	}
	grants := outbox.grantsTo(2)
	if len(grants) != 1 {
		t.Fatalf("TSO arrival must re-evaluate the pending NMR: grants = %v, want exactly 1", grants)
	}
	if ft := float64(grants[0].Time); ft != 5.0 {
		t.Errorf("§8.8: grant time = %v, want 5.0 (the message time, not LBTS=11)", ft)
	}
	stream := outbox.eventsTo(2)
	if len(stream) != 2 {
		t.Fatalf("requester stream = %v, want [TSO event, grant]", stream)
	}
	if _, ok := stream[0].(stubTSO); !ok {
		t.Errorf("§8.14: first wire event = %T, want the TSO event before its grant", stream[0])
	}
}

// TestSpec_M38_NMR_FullGrantAtRequested — §8.8 completion case: no
// queued TSO, LBTS rises past the requested time → grant at EXACTLY
// the requested time (never at LBTS).
func TestSpec_M38_NMR_FullGrantAtRequested(t *testing.T) {
	ctx := context.Background()
	mgr, outbox := newManager(t)

	if err := mgr.EnableRegulation(ctx, nmrFed, 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, nmrFed, 2); err != nil {
		t.Fatalf("EnableConstrained(2): %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, nmrFed, 2, 2.0); err != nil {
		t.Fatalf("NMR(2, 2.0): %v", err)
	}
	// Regulator NMR(5.0) → floor 6.0 → LBTS 6.0 > 2.0.
	if err := mgr.NextMessageRequest(ctx, nmrFed, 1, 5.0); err != nil {
		t.Fatalf("NMR(1, 5.0): %v", err)
	}

	grants := outbox.grantsTo(2)
	if len(grants) != 1 {
		t.Fatalf("requester received %d grants %v, want exactly 1", len(grants), grants)
	}
	if ft := float64(grants[0].Time); ft != 2.0 {
		t.Errorf("§8.8: grant time = %v, want exactly the requested 2.0", ft)
	}
}

// TestSpec_M38_NMRA_InclusiveBoundary — §8.9: the Available variant
// grants when LBTS EQUALS the grant time, both for the requested-time
// completion and for the queued-message case.
func TestSpec_M38_NMRA_InclusiveBoundary(t *testing.T) {
	ctx := context.Background()
	mgr, outbox := newManager(t)

	if err := mgr.EnableRegulation(ctx, nmrFed, 1, 2.0); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, nmrFed, 2); err != nil {
		t.Fatalf("EnableConstrained(2): %v", err)
	}
	if err := mgr.EnableConstrained(ctx, nmrFed, 3); err != nil {
		t.Fatalf("EnableConstrained(3): %v", err)
	}

	// LBTS = 0 + 2.0 = 2.0. Federate 3 has a TSO message at exactly
	// 2.0 queued, federate 2 has none.
	if err := mgr.BufferTSO(ctx, nmrFed, 3, 2.0, stubTSO{seq: 9}); err != nil {
		t.Fatalf("BufferTSO: %v", err)
	}

	// NMRA(2.0) with LBTS == 2.0 → grant at 2.0 (inclusive boundary).
	if err := mgr.NextMessageRequestAvailable(ctx, nmrFed, 2, 2.0); err != nil {
		t.Fatalf("NMRA(2, 2.0): %v", err)
	}
	grants := outbox.grantsTo(2)
	if len(grants) != 1 || float64(grants[0].Time) != 2.0 {
		t.Fatalf("§8.9: NMRA(2.0) at LBTS == 2.0 grants at 2.0; got %v", grants)
	}

	// NMRA(5.0) with queued TSO at 2.0 == LBTS → grant at the message
	// time, message first.
	if err := mgr.NextMessageRequestAvailable(ctx, nmrFed, 3, 5.0); err != nil {
		t.Fatalf("NMRA(3, 5.0): %v", err)
	}
	grants = outbox.grantsTo(3)
	if len(grants) != 1 || float64(grants[0].Time) != 2.0 {
		t.Fatalf("§8.9: NMRA(5.0) with TSO at 2.0 == LBTS grants at 2.0; got %v", grants)
	}
	stream := outbox.eventsTo(3)
	if len(stream) != 2 {
		t.Fatalf("federate 3 stream = %v, want [TSO event, grant]", stream)
	}
	if _, ok := stream[0].(stubTSO); !ok {
		t.Errorf("§8.14: first wire event = %T, want the TSO event before its grant", stream[0])
	}
	// §8.9: the grant at the message time COMPLETES the request (one
	// request, one grant) — a follow-up NMRA is legal.
	if err := mgr.NextMessageRequestAvailable(ctx, nmrFed, 3, 5.0); err != nil {
		t.Fatalf("§8.9: grant at message time completes the request; follow-up NMRA = %v, want nil", err)
	}
}
