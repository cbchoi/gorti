package grpc

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// pushOnlyOutbox satisfies core.Outbox but NOT SubscribableOutbox.
// Used to assert the Events handler returns Unimplemented when the
// production outbox cannot be subscribed to.
type pushOnlyOutbox struct{}

func (pushOnlyOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

// fakeSubscribableOutbox is an in-memory SubscribableOutbox: every
// Subscribe gets its own batch channel, and events pushed via push()
// flow to the most recent subscriber as singleton batches. The fake
// is sufficient for handler tests that don't exercise multi-federate
// fanout.
type fakeSubscribableOutbox struct {
	mu           sync.Mutex
	ch           chan []core.OutboundEvent
	subscribeErr error
	cancelErr    error
	subscribed   int
	cancelled    int
}

func newFakeSubscribableOutbox() *fakeSubscribableOutbox {
	return &fakeSubscribableOutbox{ch: make(chan []core.OutboundEvent, 8)}
}

func (f *fakeSubscribableOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, evt core.OutboundEvent) error {
	f.ch <- []core.OutboundEvent{evt}
	return nil
}

func (f *fakeSubscribableOutbox) Subscribe(_ context.Context, _ core.FederationName, _ core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subscribeErr != nil {
		return nil, nil, f.subscribeErr
	}
	f.subscribed++
	cancel := func() error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.cancelled++
		return f.cancelErr
	}
	return f.ch, cancel, nil
}

func (f *fakeSubscribableOutbox) push(evt core.OutboundEvent) { f.ch <- []core.OutboundEvent{evt} }

func (f *fakeSubscribableOutbox) closeChan() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.ch)
}

// fakeStream implements rtiv1.StreamService_EventsServer. It records every
// Send and lets tests inject a Send error on demand.
type fakeStream struct {
	grpc.ServerStream
	ctx     context.Context
	mu      sync.Mutex
	sent    []*rtiv1.FederateEvent
	sendErr error
}

type fakeBatchStream struct {
	grpc.ServerStream
	ctx       context.Context
	mu        sync.Mutex
	sent      []*rtiv1.FederateEventBatch
	sendErr   error
	failAt    int
	sendCalls int
}

type discardBatchStream struct {
	grpc.ServerStream
	ctx   context.Context
	sends int
}

type marshalBatchStream struct {
	grpc.ServerStream
	ctx   context.Context
	sends int
	bytes int
}

func (s *fakeBatchStream) Context() context.Context { return s.ctx }

func (s *fakeBatchStream) Send(batch *rtiv1.FederateEventBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCalls++
	if s.sendErr != nil && (s.failAt == 0 || s.sendCalls == s.failAt) {
		return s.sendErr
	}
	s.sent = append(s.sent, batch)
	return nil
}

func (s *discardBatchStream) Context() context.Context { return s.ctx }

func (s *discardBatchStream) Send(*rtiv1.FederateEventBatch) error {
	s.sends++
	return nil
}

func (s *marshalBatchStream) Context() context.Context { return s.ctx }

func (s *marshalBatchStream) Send(batch *rtiv1.FederateEventBatch) error {
	wire, err := proto.Marshal(batch)
	if err != nil {
		return err
	}
	s.sends++
	s.bytes += len(wire)
	return nil
}

func newFakeStream(ctx context.Context) *fakeStream {
	return &fakeStream{ctx: ctx}
}

func (s *fakeStream) Context() context.Context { return s.ctx }

func (s *fakeStream) Send(evt *rtiv1.FederateEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, evt)
	return nil
}

func (s *fakeStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeStream) SetTrailer(metadata.MD)       {}
func (s *fakeStream) SendMsg(_ interface{}) error  { return nil }
func (s *fakeStream) RecvMsg(_ interface{}) error  { return nil }

// fakeOutboundEvent satisfies core.OutboundEvent and exposes Inner() so the
// handler's toFederateEvent can recover the proto.
type fakeOutboundEvent struct {
	pb *rtiv1.FederateEvent
}

func (f *fakeOutboundEvent) Seq() uint64                 { return f.pb.GetSeq() }
func (f *fakeOutboundEvent) Inner() *rtiv1.FederateEvent { return f.pb }

func requireReadyBatch(t *testing.T, sent []*rtiv1.FederateEventBatch) []*rtiv1.FederateEventBatch {
	t.Helper()
	if len(sent) == 0 {
		t.Fatal("EventBatches sent no ready handshake")
	}
	if !sent[0].GetReady() || len(sent[0].GetEvents()) != 0 {
		t.Fatalf("first batch = %+v, want empty ready handshake", sent[0])
	}
	return sent[1:]
}

type fakeCallbackMembership struct {
	federation core.FederationName
	handle     core.FederateHandle
	generation uint64
	validates  int
}

func (m *fakeCallbackMembership) ValidateMember(fed core.FederationName, h core.FederateHandle) error {
	m.validates++
	if fed != m.federation || h != m.handle {
		return core.ErrFederateNotJoined
	}
	return nil
}

func (m *fakeCallbackMembership) GenerationFor(fed core.FederationName) (uint64, bool) {
	return m.generation, fed == m.federation
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestStreamService_Events_NonSubscribableOutbox_ReturnsUnimplemented(t *testing.T) {
	svc := newStreamService(pushOnlyOutbox{})
	stream := newFakeStream(context.Background())
	err := svc.Events(&rtiv1.EventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed",
		FederateHandle: 1,
	}, stream)
	if err == nil {
		t.Fatal("want Unimplemented, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unimplemented {
		t.Errorf("code = %v, want Unimplemented", st.Code())
	}
}

func TestStreamService_Events_RejectsBadWireVersion(t *testing.T) {
	svc := newStreamService(newFakeSubscribableOutbox())
	stream := newFakeStream(context.Background())
	err := svc.Events(&rtiv1.EventsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED,
	}, stream)
	if err == nil {
		t.Fatal("want error for unspecified wire version")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", st.Code())
	}
}

func TestStreamService_Events_SubscribeError_MapsToStatus(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	outbox.subscribeErr = core.ErrFederateNotJoined
	svc := newStreamService(outbox)
	stream := newFakeStream(context.Background())
	err := svc.Events(&rtiv1.EventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed",
		FederateHandle: 1,
	}, stream)
	if err == nil {
		t.Fatal("want error from Subscribe, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", st.Code())
	}
}

func TestStreamService_Events_FlowsChannelEventsToStream(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newFakeStream(ctx)

	// Pre-load events; close to terminate the loop after draining.
	outbox.push(&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 1}})
	outbox.push(&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 2}})
	outbox.push(&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 3}})

	done := make(chan error, 1)
	go func() {
		done <- svc.Events(&rtiv1.EventsRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: "fed",
			FederateHandle: 1,
		}, stream)
	}()

	// Give the handler a moment to drain, then close to signal end-of-stream.
	time.Sleep(20 * time.Millisecond)
	outbox.closeChan()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Events returned %v, want nil after channel close", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Events did not return within 1s after channel close")
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.sent) != 3 {
		t.Fatalf("sent %d events, want 3", len(stream.sent))
	}
	for i, evt := range stream.sent {
		if evt.GetSeq() != uint64(i+1) {
			t.Errorf("sent[%d].Seq = %d, want %d", i, evt.GetSeq(), i+1)
		}
	}
	if outbox.cancelled != 1 {
		t.Errorf("cancel called %d times, want 1", outbox.cancelled)
	}
}

func TestStreamService_EventBatches_PreservesOutboxBatch(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	stream := &fakeBatchStream{ctx: context.Background()}
	outbox.ch <- []core.OutboundEvent{
		&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 1}},
		&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 2}},
		&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 3}},
	}
	close(outbox.ch)

	if err := svc.EventBatches(&rtiv1.EventsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1,
	}, stream); err != nil {
		t.Fatalf("EventBatches returned %v", err)
	}
	dataBatches := requireReadyBatch(t, stream.sent)
	if len(dataBatches) != 1 {
		t.Fatalf("sent %d data batches, want 1", len(dataBatches))
	}
	events := dataBatches[0].GetEvents()
	if len(events) != 3 {
		t.Fatalf("batch has %d events, want 3", len(events))
	}
	for i, event := range events {
		if event.GetSeq() != uint64(i+1) {
			t.Fatalf("event[%d] seq = %d, want %d", i, event.GetSeq(), i+1)
		}
	}
}

func TestStreamService_EventBatches_CoalescesReadyBatches(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	stream := &fakeBatchStream{ctx: context.Background()}
	// Enough contract-sized (32-event) outbox batches to exactly fill one
	// coalesced wire frame; scales with the cap so this test always pins
	// the current maxCoalescedEventBatchEvents value.
	for batchIndex := 0; batchIndex < maxCoalescedEventBatchEvents/32; batchIndex++ {
		batch := make([]core.OutboundEvent, 0, 32)
		for eventIndex := 0; eventIndex < 32; eventIndex++ {
			seq := uint64(batchIndex*32 + eventIndex + 1)
			batch = append(batch, &fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: seq}})
		}
		outbox.ch <- batch
	}
	close(outbox.ch)

	if err := svc.EventBatches(&rtiv1.EventsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1,
	}, stream); err != nil {
		t.Fatalf("EventBatches returned %v", err)
	}
	dataBatches := requireReadyBatch(t, stream.sent)
	if len(dataBatches) != 1 {
		t.Fatalf("sent %d data batches, want 1", len(dataBatches))
	}
	if got := len(dataBatches[0].GetEvents()); got != maxCoalescedEventBatchEvents {
		t.Fatalf("coalesced batch has %d events, want %d", got, maxCoalescedEventBatchEvents)
	}
	for i, event := range dataBatches[0].GetEvents() {
		if want := uint64(i + 1); event.GetSeq() != want {
			t.Fatalf("event[%d] seq = %d, want %d", i, event.GetSeq(), want)
		}
	}
}

func TestStreamService_EventBatches_TimeGrantEndsCoalescedBatch(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	stream := &fakeBatchStream{ctx: context.Background()}
	outbox.ch <- []core.OutboundEvent{
		&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 1}},
		&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 2}},
	}
	outbox.ch <- []core.OutboundEvent{
		&timepkg.TimeAdvanceGrant{Time: 3},
		&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 3}},
	}
	outbox.ch <- []core.OutboundEvent{
		&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 4}},
	}
	close(outbox.ch)

	if err := svc.EventBatches(&rtiv1.EventsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1,
	}, stream); err != nil {
		t.Fatalf("EventBatches returned %v", err)
	}
	dataBatches := requireReadyBatch(t, stream.sent)
	if len(dataBatches) != 2 {
		t.Fatalf("sent %d data batches, want 2", len(dataBatches))
	}
	if got := dataBatches[0].GetEvents(); len(got) != 3 ||
		got[0].GetSeq() != 1 || got[1].GetSeq() != 2 || got[2].GetGrant() == nil {
		t.Fatalf("first batch = %+v, want seq 1, seq 2, grant", got)
	}
	if got := dataBatches[1].GetEvents(); len(got) != 2 ||
		got[0].GetSeq() != 3 || got[1].GetSeq() != 4 {
		t.Fatalf("second batch = %+v, want seq 3, seq 4", got)
	}
}

func TestSendAvailableEventBatches_DoesNotWaitForMoreInput(t *testing.T) {
	ch := make(chan []core.OutboundEvent)
	stream := &fakeBatchStream{ctx: context.Background()}
	closed, err := sendAvailableEventBatches(
		[]core.OutboundEvent{&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 1}}},
		ch,
		stream,
		make([]core.OutboundEvent, 0, maxCoalescedEventBatchEvents),
	)
	if err != nil {
		t.Fatalf("sendAvailableEventBatches returned %v", err)
	}
	if closed {
		t.Fatal("sendAvailableEventBatches reported an open channel as closed")
	}
	if len(stream.sent) != 1 || stream.sent[0].GetEvents()[0].GetSeq() != 1 {
		t.Fatalf("sent batches = %+v, want one immediate event", stream.sent)
	}
}

func TestSendAvailableEventBatches_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := &fakeBatchStream{ctx: ctx}
	closed, err := sendAvailableEventBatches(
		[]core.OutboundEvent{&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 1}}},
		make(chan []core.OutboundEvent),
		stream,
		make([]core.OutboundEvent, 0, maxCoalescedEventBatchEvents),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sendAvailableEventBatches error = %v, want context.Canceled", err)
	}
	if closed {
		t.Fatal("cancelled stream reported its source channel as closed")
	}
	if len(stream.sent) != 0 {
		t.Fatalf("sent %d batches after cancellation, want 0", len(stream.sent))
	}
}

func TestStreamService_EventBatches_RejectsGenerationMismatchBeforeSubscribe(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	membership := &fakeCallbackMembership{federation: "fed", handle: 1, generation: 8}
	svc := newStreamService(outbox)
	baseStream := &fakeBatchStream{ctx: context.Background()}
	stream := &grpc.GenericServerStream[rtiv1.EventsRequest, rtiv1.FederateEventBatch]{
		ServerStream: &membershipServerStream{
			ServerStream: baseStream,
			server:       &Server{membership: membership},
		},
	}
	expected := uint64(7)

	err := svc.EventBatches(&rtiv1.EventsRequest{
		WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:               "fed",
		FederateHandle:               1,
		ExpectedFederationGeneration: &expected,
	}, stream)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("EventBatches error = %v (code %v), want FailedPrecondition", err, status.Code(err))
	}
	if outbox.subscribed != 0 {
		t.Fatalf("Subscribe called %d times for stale generation, want 0", outbox.subscribed)
	}
	if membership.validates != 1 {
		t.Fatalf("ValidateMember called %d times, want 1", membership.validates)
	}
	if len(baseStream.sent) != 0 {
		t.Fatalf("sent %d batches before rejecting stale generation, want 0", len(baseStream.sent))
	}
}

func TestStreamService_EventBatches_LegacyRequestStillValidatesMembership(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	membership := &fakeCallbackMembership{federation: "fed", handle: 1, generation: 8}
	svc := newStreamService(outbox, membership)
	stream := &fakeBatchStream{ctx: context.Background()}
	close(outbox.ch)

	err := svc.EventBatches(&rtiv1.EventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed",
		FederateHandle: 1,
	}, stream)
	if err != nil {
		t.Fatalf("legacy EventBatches request returned %v", err)
	}
	if outbox.subscribed != 1 || membership.validates != 1 {
		t.Fatalf("legacy request subscribed=%d validates=%d, want 1/1", outbox.subscribed, membership.validates)
	}
	if dataBatches := requireReadyBatch(t, stream.sent); len(dataBatches) != 0 {
		t.Fatalf("legacy request sent %d unexpected data batches", len(dataBatches))
	}
}

func TestStreamService_EventBatches_LargeBatchChunksInOrder(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	stream := &fakeBatchStream{ctx: context.Background()}
	batch := make([]core.OutboundEvent, 0, 5)
	for seq := uint64(1); seq <= 5; seq++ {
		batch = append(batch, largeCallbackEvent(seq, 1<<20))
	}
	outbox.ch <- batch
	close(outbox.ch)

	if err := svc.EventBatches(&rtiv1.EventsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1,
	}, stream); err != nil {
		t.Fatalf("EventBatches returned %v", err)
	}
	dataBatches := requireReadyBatch(t, stream.sent)
	if len(dataBatches) < 2 {
		t.Fatalf("sent %d data batches, want chunked output", len(dataBatches))
	}
	var gotSeq []uint64
	for i, sent := range dataBatches {
		if size := proto.Size(sent); size > maxEventBatchSerializedSize {
			t.Fatalf("batch[%d] serialized size = %d, limit %d", i, size, maxEventBatchSerializedSize)
		}
		for _, event := range sent.GetEvents() {
			gotSeq = append(gotSeq, event.GetSeq())
		}
	}
	for i, seq := range gotSeq {
		if want := uint64(i + 1); seq != want {
			t.Fatalf("flattened event[%d] seq = %d, want %d", i, seq, want)
		}
	}
	if len(gotSeq) != len(batch) {
		t.Fatalf("delivered %d events, want %d", len(gotSeq), len(batch))
	}
}

func TestStreamService_EventBatches_RejectsOversizedSingleEvent(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	stream := &fakeBatchStream{ctx: context.Background()}
	outbox.ch <- []core.OutboundEvent{largeCallbackEvent(41, maxEventBatchSerializedSize)}

	err := svc.EventBatches(&rtiv1.EventsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: "fed", FederateHandle: 1,
	}, stream)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("EventBatches error = %v (code %v), want ResourceExhausted", err, status.Code(err))
	}
	if !strings.Contains(err.Error(), "serialized size") {
		t.Fatalf("error %q does not explain serialized size limit", err)
	}
	if dataBatches := requireReadyBatch(t, stream.sent); len(dataBatches) != 0 {
		t.Fatalf("sent %d data batches for oversized event, want 0", len(dataBatches))
	}
}

func TestFederateEventBatchEntrySizeIsUpperBound(t *testing.T) {
	// Non-reflect/receive events take the exact proto.Size path and must
	// match precisely.
	exactEvents := []*rtiv1.FederateEvent{
		{Seq: 1},
		{Seq: 127, Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 1.25}}},
	}
	for _, event := range exactEvents {
		want := proto.Size(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{event}})
		if got := federateEventBatchEntrySize(event); got != want {
			t.Errorf("event seq %d entry size = %d, want exact %d", event.GetSeq(), got, want)
		}
	}

	// Reflect/receive events use the W6b conservative bound: never below
	// the exact size, and never wildly above it (bounded slack).
	for _, payloadSize := range []int{0, 1, 127, 128, 16_383, 16_384, 1 << 20} {
		event, err := toFederateEvent(largeCallbackEvent(uint64(payloadSize+1), payloadSize))
		if err != nil {
			t.Fatal(err)
		}
		exact := proto.Size(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{event}})
		got := federateEventBatchEntrySize(event)
		if got < exact {
			t.Errorf("payload %d: entry size bound %d < exact %d", payloadSize, got, exact)
		}
		slackBudget := federateEventEnvelopeSizeSlack + federateEventValueSizeSlack + 8
		if got > exact+slackBudget {
			t.Errorf("payload %d: entry size bound %d exceeds exact %d + slack budget %d", payloadSize, got, exact, slackBudget)
		}
	}
}

// randomBoundedCallbackEvent builds a random Reflect or Receive event whose
// payload distribution deliberately straddles the maxEventBatchSerializedSize/2
// fallback threshold of the W6b size bound. maxTotalPayload caps the sum of
// value byte lengths so callers can keep events under the per-event frame
// rejection limit.
func randomBoundedCallbackEvent(rng *rand.Rand, maxTotalPayload int) *rtiv1.FederateEvent {
	values := map[uint64][]byte{}
	remaining := maxTotalPayload
	for range rng.Intn(6) {
		var size int
		switch rng.Intn(4) {
		case 0:
			size = rng.Intn(8) // tiny, incl. empty
		case 1:
			size = rng.Intn(4096)
		case 2:
			size = rng.Intn(1 << 17)
		default:
			// Large enough that one or two values cross the exact-size
			// fallback threshold (maxEventBatchSerializedSize/2 ~ 2MB).
			size = rng.Intn(3 << 20)
		}
		if size > remaining {
			size = remaining
		}
		remaining -= size
		values[rng.Uint64()] = make([]byte, size)
	}
	var ts *float64
	if rng.Intn(2) == 0 {
		v := rng.Float64()
		ts = &v
	}
	if rng.Intn(2) == 0 {
		return &rtiv1.FederateEvent{
			Seq: rng.Uint64(),
			Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{
				ObjectHandle:      rng.Uint64(),
				ObjectClassHandle: rng.Uint64(),
				Attributes:        values,
				LogicalTime:       ts,
			}},
		}
	}
	return &rtiv1.FederateEvent{
		Seq: rng.Uint64(),
		Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
			InteractionClassHandle: rng.Uint64(),
			Parameters:             values,
			LogicalTime:            ts,
		}},
	}
}

// TestFederateEventBatchEntrySizeBoundProperty pins the W6b invariant the
// chunker relies on: the bound is NEVER below the exact serialized entry
// size, for randomized events on both sides of the exact-size fallback
// threshold.
func TestFederateEventBatchEntrySizeBoundProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(0x57C0FFEE))
	for i := 0; i < 400; i++ {
		event := randomBoundedCallbackEvent(rng, 12<<20)
		exact := proto.Size(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{event}})
		if got := federateEventBatchEntrySize(event); got < exact {
			t.Fatalf("iteration %d: entry size bound %d < exact %d (event seq %d)", i, got, exact, event.GetSeq())
		}
	}
}

// TestSendEventBatchChunks_RandomizedFramesNeverExceedCap drives the chunker
// with randomized batches and asserts no emitted frame exceeds the safe
// serialized limit while order and completeness are preserved.
func TestSendEventBatchChunks_RandomizedFramesNeverExceedCap(t *testing.T) {
	rng := rand.New(rand.NewSource(0x0B57ACE))
	for iter := 0; iter < 40; iter++ {
		count := 1 + rng.Intn(maxCoalescedEventBatchEvents)
		batch := make([]core.OutboundEvent, 0, count)
		var wantSeqs []uint64
		for i := 0; i < count; i++ {
			// Keep each event's payload under the per-event rejection
			// limit while still crossing the exact-size fallback threshold.
			pb := randomBoundedCallbackEvent(rng, 3<<20)
			pb.Seq = uint64(i + 1)
			batch = append(batch, &fakeOutboundEvent{pb: pb})
			wantSeqs = append(wantSeqs, pb.Seq)
		}
		stream := &fakeBatchStream{ctx: context.Background()}
		if err := sendEventBatchChunks(batch, stream); err != nil {
			t.Fatalf("iteration %d: sendEventBatchChunks: %v", iter, err)
		}
		var gotSeqs []uint64
		for i, sent := range stream.sent {
			if size := proto.Size(sent); size > maxEventBatchSerializedSize {
				t.Fatalf("iteration %d: frame[%d] serialized size %d exceeds limit %d", iter, i, size, maxEventBatchSerializedSize)
			}
			for _, event := range sent.GetEvents() {
				gotSeqs = append(gotSeqs, event.GetSeq())
			}
		}
		if len(gotSeqs) != len(wantSeqs) {
			t.Fatalf("iteration %d: delivered %d events, want %d", iter, len(gotSeqs), len(wantSeqs))
		}
		for i, seq := range gotSeqs {
			if seq != wantSeqs[i] {
				t.Fatalf("iteration %d: event[%d] seq = %d, want %d", iter, i, seq, wantSeqs[i])
			}
		}
	}
}

func TestSendEventBatchChunks_AcceptsEventAtExactLimit(t *testing.T) {
	event := callbackEventAtSerializedBatchSize(t, 1, maxEventBatchSerializedSize)
	stream := &fakeBatchStream{ctx: context.Background()}
	if err := sendEventBatchChunks([]core.OutboundEvent{event}, stream); err != nil {
		t.Fatalf("sendEventBatchChunks returned %v for exact-limit event", err)
	}
	if len(stream.sent) != 1 {
		t.Fatalf("sent %d batches, want 1", len(stream.sent))
	}
	if got := proto.Size(stream.sent[0]); got != maxEventBatchSerializedSize {
		t.Fatalf("serialized batch size = %d, want exact limit %d", got, maxEventBatchSerializedSize)
	}
}

func TestSendEventBatchChunks_CumulativeSizeBoundary(t *testing.T) {
	first := largeCallbackEvent(1, 16)
	firstPB, err := toFederateEvent(first)
	if err != nil {
		t.Fatal(err)
	}
	firstSize := federateEventBatchEntrySize(firstPB)

	t.Run("exact limit remains one batch", func(t *testing.T) {
		// firstSize is the W6b conservative bound for the small first
		// event, so a second event sized to the remaining budget keeps the
		// accumulated bound at exactly the limit: one frame, and the frame
		// serializes strictly under the limit (bound >= exact).
		second := callbackEventAtSerializedBatchSize(t, 2, maxEventBatchSerializedSize-firstSize)
		stream := &fakeBatchStream{ctx: context.Background()}
		if err := sendEventBatchChunks([]core.OutboundEvent{first, second}, stream); err != nil {
			t.Fatal(err)
		}
		if len(stream.sent) != 1 {
			t.Fatalf("sent %d batches, want 1", len(stream.sent))
		}
		if got := proto.Size(stream.sent[0]); got > maxEventBatchSerializedSize {
			t.Fatalf("serialized batch size = %d, want <= %d", got, maxEventBatchSerializedSize)
		}
	})

	t.Run("limit plus one splits before second event", func(t *testing.T) {
		second := callbackEventAtSerializedBatchSize(t, 2, maxEventBatchSerializedSize-firstSize+1)
		stream := &fakeBatchStream{ctx: context.Background()}
		if err := sendEventBatchChunks([]core.OutboundEvent{first, second}, stream); err != nil {
			t.Fatal(err)
		}
		if len(stream.sent) != 2 {
			t.Fatalf("sent %d batches, want 2", len(stream.sent))
		}
		if got := stream.sent[0].GetEvents()[0].GetSeq(); got != 1 {
			t.Fatalf("first batch seq = %d, want 1", got)
		}
		if got := stream.sent[1].GetEvents()[0].GetSeq(); got != 2 {
			t.Fatalf("second batch seq = %d, want 2", got)
		}
	})
}

func TestSendEventBatchChunks_OversizedEventDoesNotSendEarlierPartialBatch(t *testing.T) {
	stream := &fakeBatchStream{ctx: context.Background()}
	err := sendEventBatchChunks([]core.OutboundEvent{
		largeCallbackEvent(40, 16),
		largeCallbackEvent(41, maxEventBatchSerializedSize),
	}, stream)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("sendEventBatchChunks error = %v (code %v), want ResourceExhausted", err, status.Code(err))
	}
	if len(stream.sent) != 0 {
		t.Fatalf("sent %d partial batches before rejecting oversized event, want 0", len(stream.sent))
	}
}

func TestSendEventBatchChunks_PropagatesSendError(t *testing.T) {
	want := errors.New("batch send failed")
	stream := &fakeBatchStream{ctx: context.Background(), sendErr: want}
	err := sendEventBatchChunks([]core.OutboundEvent{largeCallbackEvent(1, 16)}, stream)
	if !errors.Is(err, want) {
		t.Fatalf("sendEventBatchChunks error = %v, want %v", err, want)
	}
}

func TestSendEventBatchChunks_StopsAfterSecondChunkSendError(t *testing.T) {
	first := callbackEventAtSerializedBatchSize(t, 1, maxEventBatchSerializedSize)
	second := largeCallbackEvent(2, 16)
	want := errors.New("second batch send failed")
	stream := &fakeBatchStream{ctx: context.Background(), sendErr: want, failAt: 2}
	err := sendEventBatchChunks([]core.OutboundEvent{first, second}, stream)
	if !errors.Is(err, want) {
		t.Fatalf("sendEventBatchChunks error = %v, want %v", err, want)
	}
	if stream.sendCalls != 2 {
		t.Fatalf("Send called %d times, want 2", stream.sendCalls)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetEvents()[0].GetSeq() != 1 {
		t.Fatalf("successful batches = %v, want only seq 1", stream.sent)
	}
}

func largeCallbackEvent(seq uint64, payloadSize int) core.OutboundEvent {
	return &fakeOutboundEvent{pb: &rtiv1.FederateEvent{
		Seq: seq,
		Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
			Parameters: map[uint64][]byte{1: make([]byte, payloadSize)},
		}},
	}}
}

func callbackEventAtSerializedBatchSize(tb testing.TB, seq uint64, target int) core.OutboundEvent {
	tb.Helper()
	payloadSize := target
	for range 8 {
		event := largeCallbackEvent(seq, payloadSize)
		pb, err := toFederateEvent(event)
		if err != nil {
			tb.Fatal(err)
		}
		size := proto.Size(&rtiv1.FederateEventBatch{Events: []*rtiv1.FederateEvent{pb}})
		if size == target {
			return event
		}
		payloadSize += target - size
		if payloadSize < 0 {
			break
		}
	}
	tb.Fatalf("could not construct callback batch with serialized size %d", target)
	return nil
}

func benchmarkCallbackBatch(size int) []core.OutboundEvent {
	batch := make([]core.OutboundEvent, 0, size)
	for i := 0; i < size; i++ {
		batch = append(batch, largeCallbackEvent(uint64(i+1), 64))
	}
	return batch
}

func BenchmarkSendEventBatchChunks(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{name: "events_1", size: 1},
		{name: "events_32", size: 32},
		{name: "events_256", size: 256},
	} {
		b.Run(tc.name, func(b *testing.B) {
			batch := benchmarkCallbackBatch(tc.size)
			stream := &discardBatchStream{ctx: context.Background()}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := sendEventBatchChunks(batch, stream); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSendEventBatchChunksMarshal(b *testing.B) {
	cases := []struct {
		name  string
		batch []core.OutboundEvent
	}{
		{name: "events_32", batch: benchmarkCallbackBatch(32)},
		{name: "events_256", batch: benchmarkCallbackBatch(256)},
		{name: "near_limit_split", batch: []core.OutboundEvent{
			callbackEventAtSerializedBatchSize(b, 1, maxEventBatchSerializedSize),
			largeCallbackEvent(2, 16),
		}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			stream := &marshalBatchStream{ctx: context.Background()}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := sendEventBatchChunks(tc.batch, stream); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestStreamService_Events_SendError_Propagates(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	stream := newFakeStream(context.Background())
	stream.sendErr = errors.New("stream broken")

	outbox.push(&fakeOutboundEvent{pb: &rtiv1.FederateEvent{Seq: 1}})

	err := svc.Events(&rtiv1.EventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed",
		FederateHandle: 1,
	}, stream)
	if err == nil {
		t.Fatal("want error from broken Send, got nil")
	}
	if outbox.cancelled != 1 {
		t.Errorf("cancel called %d times, want 1 even on Send error", outbox.cancelled)
	}
}

func TestStreamService_Events_ContextCancellation_ExitsCleanly(t *testing.T) {
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	ctx, cancel := context.WithCancel(context.Background())
	stream := newFakeStream(ctx)

	done := make(chan error, 1)
	go func() {
		done <- svc.Events(&rtiv1.EventsRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: "fed",
			FederateHandle: 1,
		}, stream)
	}()

	// Wait for handler to enter loop, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Events returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Events did not return within 1s of context cancellation")
	}
	if outbox.cancelled != 1 {
		t.Errorf("cancel called %d times, want 1", outbox.cancelled)
	}
}

func TestStreamService_Events_NonInnerOutboundEvent_ReturnsInternal(t *testing.T) {
	// An OutboundEvent that does NOT expose Inner() *FederateEvent must
	// fail the conversion path: handler returns an Internal status rather
	// than panicking.
	outbox := newFakeSubscribableOutbox()
	svc := newStreamService(outbox)
	stream := newFakeStream(context.Background())

	outbox.push(seqOnlyEvent(7))

	err := svc.Events(&rtiv1.EventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: "fed",
		FederateHandle: 1,
	}, stream)
	if err == nil {
		t.Fatal("want error for non-Inner OutboundEvent")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("code = %v, want Internal", st.Code())
	}
}

// seqOnlyEvent is a core.OutboundEvent that does NOT expose Inner().
type seqOnlyEvent uint64

func (s seqOnlyEvent) Seq() uint64 { return uint64(s) }

// ----------------------------------------------------------------------------
// M21 TASK-204b — toFederateEvent translates time-mgr events
// ----------------------------------------------------------------------------

// 204b.1 — toFederateEvent(*time.TimeAdvanceGrant) → FederateEvent_Grant.
func TestToFederateEvent_TimeAdvanceGrant(t *testing.T) {
	g := &timepkg.TimeAdvanceGrant{Time: 5.0}
	fe, err := toFederateEvent(g)
	if err != nil {
		t.Fatalf("toFederateEvent: %v", err)
	}
	if fe == nil {
		t.Fatal("got nil FederateEvent")
	}
	gv := fe.GetGrant()
	if gv == nil {
		t.Fatalf("oneof is not Grant: %+v", fe.GetEvent())
	}
	if gv.GetLogicalTime() != 5.0 {
		t.Errorf("LogicalTime = %v, want 5.0", gv.GetLogicalTime())
	}
}

// 204b.2 — toFederateEvent(*time.FederationHalted) → FederateEvent_Halted (oneof tag 99).
func TestToFederateEvent_FederationHalted(t *testing.T) {
	h := &timepkg.FederationHalted{Cause: "stall"}
	fe, err := toFederateEvent(h)
	if err != nil {
		t.Fatalf("toFederateEvent: %v", err)
	}
	hv := fe.GetHalted()
	if hv == nil {
		t.Fatalf("oneof is not Halted: %+v", fe.GetEvent())
	}
	if hv.GetCause() != "stall" {
		t.Errorf("Cause = %q, want %q", hv.GetCause(), "stall")
	}
}
