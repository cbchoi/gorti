// TASK-205½ (M21) — Federate SDK foundation tests.
//
// Uses bufconn to bring up a real grpcsvc.Server with the cut-1
// service set, then dials it via the SDK's Connect/JoinFederation
// path. Verifies the lifecycle invariants the plan §2.7.0 + §6
// pinned (TASK-205½.1 ... 205½.6).

package federate

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/federation"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// ----------------------------------------------------------------------------
// Bufconn fixture
// ----------------------------------------------------------------------------

type testRtid struct {
	listener *bufconn.Listener
	server   *grpc.Server
	stopOnce sync.Once
}

func newTestRtid(t *testing.T) *testRtid {
	t.Helper()
	return newTestRtidShared(t, false)
}

// newTestRtidWithTime is the time-aware variant: composes a real
// *time.Manager so the gRPC TimeService handlers don't return Unimplemented.
// W3A's time-method tests use this path; the base foundation tests use
// newTestRtid which leaves timeService nil.
func newTestRtidWithTime(t *testing.T) *testRtid {
	t.Helper()
	return newTestRtidShared(t, true)
}

func newTestRtidShared(t *testing.T, withTime bool) *testRtid {
	t.Helper()
	clock := core.NewFakeClock(time.Unix(0, 0))
	declMgr := declaration.New()
	outbox := newTestOutbox()
	objReg, err := object.New(object.Options{
		Clock:        clock,
		Outbox:       outbox,
		Declarations: declMgr,
		FOMs:         &nopFOMRepo{},
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	fedMgr, err := federation.New(federation.Options{
		Clock:    clock,
		EventLog: nil,
		FOMs:     &nopFOMRepo{},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	opts := grpcsvc.Options{
		Federations:             fedMgr,
		Declarations:            declMgr,
		Objects:                 objReg,
		Outbox:                  outbox,
		EnableInteractionStream: true,
	}
	if withTime {
		mgr, mErr := timepkg.New(timepkg.Options{Clock: clock, Outbox: outbox})
		if mErr != nil {
			t.Fatalf("time.New: %v", mErr)
		}
		opts.Time = mgr
	}
	srv, err := grpcsvc.NewServer(opts)
	if err != nil {
		t.Fatalf("grpcsvc.NewServer: %v", err)
	}
	gs := grpc.NewServer()
	if err := srv.Register(gs); err != nil {
		t.Fatalf("Register: %v", err)
	}
	lis := bufconn.Listen(1 << 20)
	rtid := &testRtid{listener: lis, server: gs}
	go func() {
		_ = gs.Serve(lis)
	}()
	t.Cleanup(rtid.stop)
	return rtid
}

func (r *testRtid) stop() {
	r.stopOnce.Do(func() {
		r.server.GracefulStop()
		_ = r.listener.Close()
	})
}

func (r *testRtid) connect(t *testing.T) *Connection {
	t.Helper()
	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return r.listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	return &Connection{
		cc:     cc,
		fed:    rtiv1.NewFederationServiceClient(cc),
		decl:   rtiv1.NewDeclarationServiceClient(cc),
		obj:    rtiv1.NewObjectServiceClient(cc),
		stream: rtiv1.NewStreamServiceClient(cc),
		tm:     rtiv1.NewTimeServiceClient(cc),
		ddm:    rtiv1.NewDDMServiceClient(cc),
	}
}

// ----------------------------------------------------------------------------
// Test stubs
// ----------------------------------------------------------------------------

// testOutbox is the smallest core.Outbox + SubscribableOutbox impl
// needed for the streamService to bind. Records nothing.
type testOutbox struct {
	mu   sync.Mutex
	subs map[subKey]chan []core.OutboundEvent
}

type subKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

func newTestOutbox() *testOutbox {
	return &testOutbox{subs: map[subKey]chan []core.OutboundEvent{}}
}

func (o *testOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	ch, ok := o.subs[subKey{fed, h}]
	o.mu.Unlock()
	if !ok {
		return nil
	}
	select {
	case ch <- []core.OutboundEvent{evt}:
	default:
	}
	return nil
}

func (o *testOutbox) Subscribe(ctx context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error) {
	ch := make(chan []core.OutboundEvent, 64)
	key := subKey{fed, h}
	o.mu.Lock()
	o.subs[key] = ch
	o.mu.Unlock()
	cancel := func() error {
		o.mu.Lock()
		delete(o.subs, key)
		o.mu.Unlock()
		return nil
	}
	go func() {
		<-ctx.Done()
		_ = cancel()
		close(ch)
	}()
	return ch, cancel, nil
}

// nopFOMRepo satisfies core.FOMRepository for the bufconn fixture.
// Returns an empty FOM handle for any module set; tests that need
// real handles supply their own FOM XML to JoinFederation and rely
// on the SDK's local fomTables for client-side resolution.
type nopFOMRepo struct{}

func (n *nopFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return emptyFOMHandle{}, nil
}
func (n *nopFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return emptyFOMHandle{}, nil
}
func (n *nopFOMRepo) RememberFor(_ core.FederationName, _ core.FOMHandle) {}
func (n *nopFOMRepo) Forget(_ core.FederationName)                        {}

type emptyFOMHandle struct{}

func (emptyFOMHandle) IsValid() bool { return true }
func (emptyFOMHandle) LookupInteractionClass(_ string) (core.InteractionClassHandle, bool) {
	return core.InvalidInteractionClassHandle, false
}
func (emptyFOMHandle) LookupObjectClass(_ string) (core.ObjectClassHandle, bool) {
	return core.InvalidObjectClassHandle, false
}
func (emptyFOMHandle) LookupAttribute(_ core.ObjectClassHandle, _ string) (core.AttributeHandle, bool) {
	return core.InvalidAttributeHandle, false
}
func (emptyFOMHandle) LookupParameter(_ core.InteractionClassHandle, _ string) (core.ParameterHandle, bool) {
	return core.InvalidParameterHandle, false
}

type compensatingFederationClient struct {
	rtiv1.FederationServiceClient
	generation       uint64
	handle           uint64
	resignErr        error
	resignCalls      int
	resignRequest    *rtiv1.ResignFederationRequest
	resignContextErr error
}

func (c *compensatingFederationClient) CreateFederation(
	context.Context,
	*rtiv1.CreateFederationRequest,
	...grpc.CallOption,
) (*rtiv1.CreateFederationResponse, error) {
	return &rtiv1.CreateFederationResponse{FederationGeneration: c.generation}, nil
}

func (c *compensatingFederationClient) JoinFederation(
	context.Context,
	*rtiv1.JoinFederationRequest,
	...grpc.CallOption,
) (*rtiv1.JoinFederationResponse, error) {
	return &rtiv1.JoinFederationResponse{FederateHandle: c.handle}, nil
}

func (c *compensatingFederationClient) ResignFederation(
	ctx context.Context,
	req *rtiv1.ResignFederationRequest,
	_ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.resignCalls++
	c.resignRequest = req
	c.resignContextErr = ctx.Err()
	if c.resignErr != nil {
		return nil, c.resignErr
	}
	return &rtiv1.Empty{}, nil
}

type callbackOpenFailureClient struct {
	rtiv1.StreamServiceClient
	batchErr      error
	legacyErr     error
	batchRequest  *rtiv1.EventsRequest
	legacyRequest *rtiv1.EventsRequest
}

func (c *callbackOpenFailureClient) EventBatches(
	_ context.Context,
	req *rtiv1.EventsRequest,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[rtiv1.FederateEventBatch], error) {
	c.batchRequest = req
	return nil, c.batchErr
}

func (c *callbackOpenFailureClient) Events(
	_ context.Context,
	req *rtiv1.EventsRequest,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[rtiv1.FederateEvent], error) {
	c.legacyRequest = req
	return nil, c.legacyErr
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// 205½.1 — Connect + Close cycle; no goroutine leak across -count=10.
func TestConnectCloseLeakFree(t *testing.T) {
	rtid := newTestRtid(t)
	baseline := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		conn := rtid.connect(t)
		if err := conn.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	final := runtime.NumGoroutine()
	if final > baseline+3 {
		t.Errorf("goroutine leak: baseline=%d final=%d", baseline, final)
	}
}

// 205½.2 — JoinFederation on a fresh rtid.
func TestJoinFederationFresh(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if fed.Handle() == 0 {
		t.Errorf("Handle = 0, want non-zero")
	}
	if fed.Name() != "alpha" {
		t.Errorf("Name = %q, want %q", fed.Name(), "alpha")
	}
	if fed.Events() == nil {
		t.Errorf("Events() returned nil channel")
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("Resign: %v", err)
	}
}

// A successful join is compensated if neither callback transport can open.
func TestJoinFederation_CallbackOpenFailurePreservesAllErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resignErr error
	}{
		{name: "compensation succeeds"},
		{name: "compensation failure is preserved", resignErr: errors.New("resign failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batchErr := status.Error(codes.Unimplemented, "batch callback unsupported")
			legacyErr := errors.New("legacy open failed")
			fedClient := &compensatingFederationClient{
				generation: 17,
				handle:     23,
				resignErr:  tc.resignErr,
			}
			streamClient := &callbackOpenFailureClient{batchErr: batchErr, legacyErr: legacyErr}
			conn := &Connection{
				cc:     &grpc.ClientConn{},
				fed:    fedClient,
				stream: streamClient,
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			joined, err := conn.JoinFederation(ctx, FederationSpec{Name: "fed"}, "alpha")
			if joined != nil {
				t.Fatalf("JoinFederation returned federate %+v on callback open failure", joined)
			}
			if !errors.Is(err, batchErr) || !errors.Is(err, legacyErr) {
				t.Fatalf("JoinFederation error = %v, want both callback open errors", err)
			}
			if tc.resignErr != nil && !errors.Is(err, tc.resignErr) {
				t.Fatalf("JoinFederation error = %v, want compensation error %v", err, tc.resignErr)
			}
			if fedClient.resignCalls != 1 {
				t.Fatalf("ResignFederation called %d times, want 1", fedClient.resignCalls)
			}
			if fedClient.resignContextErr != nil {
				t.Fatalf("compensation context error = %v, want active cleanup context", fedClient.resignContextErr)
			}
			if req := fedClient.resignRequest; req.GetFederationName() != "fed" || req.GetFederateHandle() != 23 ||
				req.GetAction() != rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES {
				t.Fatalf("compensating ResignFederation request = %+v", req)
			}
			for name, req := range map[string]*rtiv1.EventsRequest{
				"batch": streamClient.batchRequest, "legacy": streamClient.legacyRequest,
			} {
				if req == nil || req.ExpectedFederationGeneration == nil || req.GetExpectedFederationGeneration() != 17 {
					t.Fatalf("%s callback request generation = %+v, want 17", name, req)
				}
			}
		})
	}
}

// 205½.3 — JoinFederation when the federation already exists (idempotent).
func TestJoinFederationIdempotent(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed1, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("first JoinFederation: %v", err)
	}
	fed2, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "beta")
	if err != nil {
		t.Fatalf("second JoinFederation (already-exists path): %v", err)
	}
	if fed1.Handle() == fed2.Handle() {
		t.Errorf("federate handles should be distinct: both %d", fed1.Handle())
	}
	_ = fed1.Resign(ctx)
	_ = fed2.Resign(ctx)
}

// 205½.4 — Resign closes the events channel within reasonable time.
func TestResignClosesEvents(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("Resign: %v", err)
	}
	// Channel must be closed after Resign returns.
	select {
	case _, ok := <-fed.Events():
		if ok {
			t.Errorf("Events() returned a value after Resign; want closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Events() channel not closed within 2s of Resign")
	}
}

// 205½.5 — events-drain goroutine leak-free across many join/resign cycles.
func TestEventsGoroutineLeakFree(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	// Warm-up cycle so any one-time initialization happens.
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("warm-up join: %v", err)
	}
	_ = fed.Resign(ctx)
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "f")
		if err != nil {
			t.Fatalf("join cycle %d: %v", i, err)
		}
		_ = fed.Resign(ctx)
	}
	time.Sleep(50 * time.Millisecond)
	final := runtime.NumGoroutine()
	if final > baseline+5 {
		t.Errorf("goroutine leak: baseline=%d final=%d", baseline, final)
	}
}

// 205½.6 — Resign is idempotent.
func TestResignIdempotent(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("first Resign: %v", err)
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("second Resign should be no-op, got: %v", err)
	}
}

type scriptedCallbackBatchStream struct {
	grpc.ClientStream

	batches     []*rtiv1.FederateEventBatch
	next        int
	recvCalls   int
	terminalErr error
}

func (s *scriptedCallbackBatchStream) Context() context.Context { return context.Background() }

func (s *scriptedCallbackBatchStream) Recv() (*rtiv1.FederateEventBatch, error) {
	s.recvCalls++
	if s.next < len(s.batches) {
		batch := s.batches[s.next]
		s.next++
		return batch, nil
	}
	if s.terminalErr != nil {
		return nil, s.terminalErr
	}
	return nil, io.EOF
}

type scriptedCallbackEventStream struct {
	grpc.ClientStream

	events []*rtiv1.FederateEvent
	next   int
}

func (s *scriptedCallbackEventStream) Context() context.Context { return context.Background() }

func (s *scriptedCallbackEventStream) Recv() (*rtiv1.FederateEvent, error) {
	if s.next >= len(s.events) {
		return nil, io.EOF
	}
	event := s.events[s.next]
	s.next++
	return event, nil
}

type callbackFallbackClient struct {
	rtiv1.StreamServiceClient

	batch             grpc.ServerStreamingClient[rtiv1.FederateEventBatch]
	batchErr          error
	legacy            grpc.ServerStreamingClient[rtiv1.FederateEvent]
	legacyErr         error
	eventBatchesCalls int
	eventsCalls       int
}

func (c *callbackFallbackClient) EventBatches(
	context.Context,
	*rtiv1.EventsRequest,
	...grpc.CallOption,
) (grpc.ServerStreamingClient[rtiv1.FederateEventBatch], error) {
	c.eventBatchesCalls++
	return c.batch, c.batchErr
}

func (c *callbackFallbackClient) Events(
	context.Context,
	*rtiv1.EventsRequest,
	...grpc.CallOption,
) (grpc.ServerStreamingClient[rtiv1.FederateEvent], error) {
	c.eventsCalls++
	return c.legacy, c.legacyErr
}

func TestJoinFederation_LazyCallbackReadyFailureCompensatesResign(t *testing.T) {
	for _, tc := range []struct {
		name string
		code codes.Code
	}{
		{name: "generation mismatch", code: codes.FailedPrecondition},
		{name: "permission denied", code: codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readyErr := status.Error(tc.code, tc.name)
			batchStream := &scriptedCallbackBatchStream{terminalErr: readyErr}
			streamClient := &callbackFallbackClient{batch: batchStream}
			fedClient := &compensatingFederationClient{generation: 31, handle: uint64(31)<<32 | 4}
			conn := &Connection{
				cc:     &grpc.ClientConn{},
				fed:    fedClient,
				stream: streamClient,
			}

			joined, err := conn.JoinFederation(context.Background(), FederationSpec{Name: "fed"}, "alpha")
			if joined != nil {
				t.Fatalf("JoinFederation returned federate %+v after lazy ready failure", joined)
			}
			if !errors.Is(err, readyErr) || status.Code(err) != tc.code {
				t.Fatalf("JoinFederation error = %v (code %v), want %v", err, status.Code(err), tc.code)
			}
			if fedClient.resignCalls != 1 {
				t.Fatalf("compensating ResignFederation calls = %d, want 1", fedClient.resignCalls)
			}
			if streamClient.eventBatchesCalls != 1 || batchStream.recvCalls != 1 {
				t.Fatalf("EventBatches calls=%d Recv calls=%d, want 1/1",
					streamClient.eventBatchesCalls, batchStream.recvCalls)
			}
			if streamClient.eventsCalls != 0 {
				t.Fatalf("legacy Events called %d times for %v, want 0", streamClient.eventsCalls, tc.code)
			}
		})
	}
}

func TestJoinFederation_LegacyFallbackRequiresUnimplementedBeforeReady(t *testing.T) {
	callback := func(time float64) *rtiv1.FederateEvent {
		return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{
			Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: time},
		}}
	}
	unimplemented := status.Error(codes.Unimplemented, "EventBatches unsupported")

	for _, tc := range []struct {
		name              string
		batchStream       *scriptedCallbackBatchStream
		legacyEvents      []*rtiv1.FederateEvent
		wantFallbackCalls int
		wantTime          float64
	}{
		{
			name:              "first receive falls back",
			batchStream:       &scriptedCallbackBatchStream{terminalErr: unimplemented},
			legacyEvents:      []*rtiv1.FederateEvent{callback(11)},
			wantFallbackCalls: 1,
			wantTime:          11,
		},
		{
			name: "consumed batch never falls back",
			batchStream: &scriptedCallbackBatchStream{
				batches: []*rtiv1.FederateEventBatch{
					{Ready: true},
					{Events: []*rtiv1.FederateEvent{callback(22)}},
				},
				terminalErr: unimplemented,
			},
			legacyEvents:      []*rtiv1.FederateEvent{callback(99)},
			wantFallbackCalls: 0,
			wantTime:          22,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &callbackFallbackClient{
				batch:  tc.batchStream,
				legacy: &scriptedCallbackEventStream{events: tc.legacyEvents},
			}
			fedClient := &compensatingFederationClient{generation: 41, handle: uint64(41)<<32 | 2}
			conn := &Connection{
				cc:     &grpc.ClientConn{},
				fed:    fedClient,
				stream: client,
			}
			fed, err := conn.JoinFederation(context.Background(), FederationSpec{Name: "fed"}, "alpha")
			if err != nil {
				t.Fatalf("JoinFederation: %v", err)
			}
			if client.eventsCalls != tc.wantFallbackCalls {
				t.Fatalf("Events fallback calls = %d, want %d", client.eventsCalls, tc.wantFallbackCalls)
			}
			var event Event
			select {
			case received, ok := <-fed.Events():
				if !ok {
					t.Fatal("callback channel closed without expected event")
				}
				event = received
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for callback")
			}
			grant, ok := event.(TimeAdvanceGrant)
			if !ok || grant.Time != tc.wantTime {
				t.Fatalf("callback = %#v, want TimeAdvanceGrant(%v)", event, tc.wantTime)
			}
			select {
			case _, ok := <-fed.Events():
				if ok {
					t.Fatal("callback channel contains an unexpected extra event")
				}
			case <-time.After(time.Second):
				t.Fatal("callback channel did not close after terminal stream status")
			}
			if fedClient.resignCalls != 0 {
				t.Fatalf("Join initialization unexpectedly compensated %d times", fedClient.resignCalls)
			}
			if err := fed.Resign(context.Background()); err != nil {
				t.Fatalf("Resign: %v", err)
			}
		})
	}
}

// Compile-time guard: errors.Is unwraps the typed errors correctly.
var _ = errors.Is
