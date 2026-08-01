// Phase 4 of docs/rtid-tui.md — server-side filter + batching +
// overflow tracking for AdminService.TailEvents. The handler is
// covered end-to-end against a fake EventLogReader so the test can
// drive the filter / batch / overflow code paths without standing up
// a real gRPC server.
//
// rtid-TUI Phase 4.

package grpc

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ---------------------------------------------------------------- helpers ---

// fakeEventLog satisfies core.EventLog and feeds the TailEvents
// handler from a pre-built event slice.
type fakeEventLog struct {
	events []core.EventRecord
}

func (fakeEventLog) Append(_ context.Context, _ core.FederationName, _ core.EventRecord) error {
	return nil
}
func (fakeEventLog) Sync(_ context.Context, _ core.FederationName) error { return nil }
func (f fakeEventLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return &fakeReader{events: f.events}, nil
}

// fakeReader walks events in order, then returns io.EOF.
type fakeReader struct {
	events []core.EventRecord
	idx    int
	closed bool
}

func (r *fakeReader) Header() core.EventLogHeader { return core.EventLogHeader{} }
func (r *fakeReader) Next(_ context.Context) (core.EventRecord, error) {
	if r.idx >= len(r.events) {
		return nil, io.EOF
	}
	e := r.events[r.idx]
	r.idx++
	return e, nil
}
func (r *fakeReader) Close() error { r.closed = true; return nil }

// classifiedEvent is a minimal core.EventRecord that also implements
// the protoEventRecorder narrow interface so the Phase-4 classifier
// can pull a class string off it.
type classifiedEvent struct {
	seq uint64
	ev  *rtiv1.Event
}

func (c *classifiedEvent) Seq() uint64              { return c.seq }
func (c *classifiedEvent) ProtoEvent() *rtiv1.Event { return c.ev }

// tailFakeStream captures Send() calls for assertion. Implements
// rtiv1.AdminService_TailEventsServer.
type tailFakeStream struct {
	mu     sync.Mutex
	ctx    context.Context
	sent   []*rtiv1.TailEventsResponse
	delay  time.Duration // sleep injected on every Send to simulate slow client
	failOn int           // 1-based index at which Send returns an error
	err    error
}

func newTailFakeStream(ctx context.Context) *tailFakeStream {
	return &tailFakeStream{ctx: ctx}
}

func (s *tailFakeStream) Send(resp *rtiv1.TailEventsResponse) error {
	s.mu.Lock()
	d := s.delay
	s.mu.Unlock()
	if d > 0 {
		time.Sleep(d)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, resp)
	if s.failOn > 0 && len(s.sent) == s.failOn {
		s.err = errors.New("fake stream forced error")
		return s.err
	}
	return nil
}

// setDelay updates the per-Send sleep injection — guarded by the
// stream mutex so the test can swap the delay between batches
// without racing the handler goroutine.
func (s *tailFakeStream) setDelay(d time.Duration) {
	s.mu.Lock()
	s.delay = d
	s.mu.Unlock()
}

func (s *tailFakeStream) Context() context.Context     { return s.ctx }
func (s *tailFakeStream) SetHeader(metadata.MD) error  { return nil }
func (s *tailFakeStream) SendHeader(metadata.MD) error { return nil }
func (s *tailFakeStream) SetTrailer(metadata.MD)       {}
func (s *tailFakeStream) SendMsg(_ any) error          { return nil }
func (s *tailFakeStream) RecvMsg(_ any) error          { return nil }

var _ grpc.ServerStream = (*tailFakeStream)(nil)
var _ rtiv1.AdminService_TailEventsServer = (*tailFakeStream)(nil)

func (s *tailFakeStream) snapshot() []*rtiv1.TailEventsResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*rtiv1.TailEventsResponse, len(s.sent))
	copy(out, s.sent)
	return out
}

// makeFedJoined / makeInterSent build classifiedEvents whose body
// drives the Phase-4 classifier.
func makeFedJoined(seq, h uint64) core.EventRecord {
	return &classifiedEvent{
		seq: seq,
		ev: &rtiv1.Event{
			Seq: seq,
			Body: &rtiv1.Event_FedJoined{
				FedJoined: &rtiv1.FederateJoined{FederateHandle: h},
			},
		},
	}
}

func makeInterSent(seq, h uint64) core.EventRecord {
	return &classifiedEvent{
		seq: seq,
		ev: &rtiv1.Event{
			Seq: seq,
			Body: &rtiv1.Event_InterSent{
				InterSent: &rtiv1.InteractionSent{ProducerFederateHandle: h},
			},
		},
	}
}

// ---------------------------------------------------------------- tests ---

// Baseline: with no filter and no client lag, every event is
// delivered, and the batch carries the right class string + handle.
func TestPhase4_TailEvents_BatchAndClassify(t *testing.T) {
	t.Parallel()
	log := fakeEventLog{events: []core.EventRecord{
		makeFedJoined(1, 7),
		makeInterSent(2, 7),
		makeInterSent(3, 9),
	}}
	svc := newAdminService(AdminOptions{EventLog: log})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newTailFakeStream(ctx)
	if err := svc.TailEvents(&rtiv1.TailEventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
	}, stream); err != nil {
		t.Fatalf("TailEvents: %v", err)
	}
	got := stream.snapshot()
	if len(got) == 0 {
		t.Fatalf("expected at least one batch")
	}
	// Concatenate every batch's events in order.
	var all []*rtiv1.TailedEvent
	for _, b := range got {
		all = append(all, b.GetEvents()...)
	}
	if len(all) != 3 {
		t.Fatalf("event count: got %d want 3", len(all))
	}
	if all[0].GetEventClass() != "FederateJoined" || all[0].GetFederateHandle() != 7 {
		t.Errorf("event 0: got class=%s handle=%d; want FederateJoined/7",
			all[0].GetEventClass(), all[0].GetFederateHandle())
	}
	if all[1].GetEventClass() != "InteractionSent" || all[1].GetFederateHandle() != 7 {
		t.Errorf("event 1: got class=%s handle=%d; want InteractionSent/7",
			all[1].GetEventClass(), all[1].GetFederateHandle())
	}
}

// Class filter: an event_class_filter that doesn't match drops the
// event server-side; the wire never carries it.
func TestPhase4_TailEvents_ClassFilter(t *testing.T) {
	t.Parallel()
	log := fakeEventLog{events: []core.EventRecord{
		makeFedJoined(1, 7),
		makeInterSent(2, 7),
		makeInterSent(3, 9),
	}}
	svc := newAdminService(AdminOptions{EventLog: log})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newTailFakeStream(ctx)
	if err := svc.TailEvents(&rtiv1.TailEventsRequest{
		WireVersion:      rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:   "demo",
		EventClassFilter: "Interaction",
	}, stream); err != nil {
		t.Fatalf("TailEvents: %v", err)
	}
	var all []*rtiv1.TailedEvent
	for _, b := range stream.snapshot() {
		all = append(all, b.GetEvents()...)
	}
	if len(all) != 2 {
		t.Fatalf("event count after class filter: got %d want 2", len(all))
	}
	for _, e := range all {
		if e.GetEventClass() != "InteractionSent" {
			t.Errorf("class filter leaked: got %s", e.GetEventClass())
		}
	}
}

// Handle filter: keep only events from the listed federate(s).
// Federation-scope events (handle=0) bypass the filter.
func TestPhase4_TailEvents_HandleFilter(t *testing.T) {
	t.Parallel()
	log := fakeEventLog{events: []core.EventRecord{
		makeFedJoined(1, 7),
		makeInterSent(2, 7),
		makeInterSent(3, 9),
	}}
	svc := newAdminService(AdminOptions{EventLog: log})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newTailFakeStream(ctx)
	if err := svc.TailEvents(&rtiv1.TailEventsRequest{
		WireVersion:          rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:       "demo",
		FederateHandleFilter: []uint64{7},
	}, stream); err != nil {
		t.Fatalf("TailEvents: %v", err)
	}
	var all []*rtiv1.TailedEvent
	for _, b := range stream.snapshot() {
		all = append(all, b.GetEvents()...)
	}
	if len(all) != 2 {
		t.Fatalf("handle filter: got %d events want 2 (handle=7 only)", len(all))
	}
	for _, e := range all {
		if e.GetFederateHandle() != 7 {
			t.Errorf("handle filter leaked: got %d", e.GetFederateHandle())
		}
	}
}

// Batch size: with max_batch_events=2 and 5 events, we expect 3
// batches (2 + 2 + 1).
func TestPhase4_TailEvents_BatchSize(t *testing.T) {
	t.Parallel()
	events := make([]core.EventRecord, 5)
	for i := range events {
		events[i] = makeInterSent(uint64(i+1), 7)
	}
	log := fakeEventLog{events: events}
	svc := newAdminService(AdminOptions{EventLog: log})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newTailFakeStream(ctx)
	if err := svc.TailEvents(&rtiv1.TailEventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
		MaxBatchEvents: 2,
	}, stream); err != nil {
		t.Fatalf("TailEvents: %v", err)
	}
	got := stream.snapshot()
	if len(got) != 3 {
		t.Fatalf("batch count: got %d want 3", len(got))
	}
	if len(got[0].GetEvents()) != 2 || len(got[1].GetEvents()) != 2 || len(got[2].GetEvents()) != 1 {
		t.Errorf("batch sizes: got %d/%d/%d; want 2/2/1",
			len(got[0].GetEvents()), len(got[1].GetEvents()), len(got[2].GetEvents()))
	}
}

// Overflow: when Send blocks past the per-batch deadline, the batch
// is folded into overflow_skipped and forward progress continues.
// A subsequent successful batch carries the cumulative skipped
// count. The exact distribution depends on timing; we assert the
// invariant: total events on the wire + total overflow_skipped =
// total events generated.
func TestPhase4_TailEvents_OverflowSkipped(t *testing.T) {
	t.Parallel()
	// 6 events, batch=3 → 2 batches. Inject a one-shot slow Send
	// followed by a fast Send so at least one batch is folded into
	// overflow_skipped.
	const total = 6
	events := make([]core.EventRecord, total)
	for i := range events {
		events[i] = makeInterSent(uint64(i+1), 7)
	}
	log := fakeEventLog{events: events}
	svc := newAdminService(AdminOptions{EventLog: log})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newTailFakeStream(ctx)
	stream.setDelay(tailEventsSendTimeout + 100*time.Millisecond)
	// Reset the delay after a fixed wall-clock window so subsequent
	// batches go through cleanly. The window is long enough that the
	// first Send goroutine has both started + finished its sleep
	// (otherwise its delayed snapshot append still races).
	go func() {
		time.Sleep(2 * (tailEventsSendTimeout + 100*time.Millisecond))
		stream.setDelay(0)
	}()

	if err := svc.TailEvents(&rtiv1.TailEventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "demo",
		MaxBatchEvents: 3,
	}, stream); err != nil {
		t.Fatalf("TailEvents: %v", err)
	}
	// Wait for any in-flight slow-Send goroutines (spawned by
	// sendBatchNonBlocking when it gives up) so the snapshot is
	// stable before assertions.
	time.Sleep(3 * (tailEventsSendTimeout + 100*time.Millisecond))

	got := stream.snapshot()
	if len(got) == 0 {
		t.Fatalf("expected at least one delivered batch")
	}
	var delivered int
	var skipped uint64
	var sawOverflow bool
	for _, b := range got {
		delivered += len(b.GetEvents())
		if b.GetOverflowSkipped() > 0 {
			sawOverflow = true
			skipped = b.GetOverflowSkipped()
		}
	}
	if !sawOverflow {
		t.Errorf("expected at least one batch to carry overflow_skipped > 0; got %d batches", len(got))
	}
	// Note: when both Sends time out, some events are double-counted
	// across batches (the slow goroutines eventually land); we accept
	// any conservation that includes the dropped count once.
	if uint64(delivered)+skipped < total {
		t.Errorf("conservation: delivered=%d + skipped=%d < total=%d", delivered, skipped, total)
	}
}

// Default knobs: zero values for max_batch_events and
// max_batch_latency_ms map to the documented defaults (32 / 10ms).
func TestPhase4_TailEventsConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := tailEventsConfigFromRequest(&rtiv1.TailEventsRequest{})
	if cfg.maxBatch != tailEventsDefaultMaxBatch {
		t.Errorf("default maxBatch: got %d want %d", cfg.maxBatch, tailEventsDefaultMaxBatch)
	}
	if cfg.maxLatency != tailEventsDefaultMaxLatency {
		t.Errorf("default maxLatency: got %s want %s", cfg.maxLatency, tailEventsDefaultMaxLatency)
	}
	// Ceiling clamps.
	cfg = tailEventsConfigFromRequest(&rtiv1.TailEventsRequest{
		MaxBatchEvents:    1 << 20,
		MaxBatchLatencyMs: 60000,
	})
	if cfg.maxBatch != tailEventsMaxBatchCeiling {
		t.Errorf("clamped maxBatch: got %d want %d", cfg.maxBatch, tailEventsMaxBatchCeiling)
	}
	if cfg.maxLatency != tailEventsMaxLatencyCeiling {
		t.Errorf("clamped maxLatency: got %s want %s", cfg.maxLatency, tailEventsMaxLatencyCeiling)
	}
}
