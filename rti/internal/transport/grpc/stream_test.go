package grpc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
