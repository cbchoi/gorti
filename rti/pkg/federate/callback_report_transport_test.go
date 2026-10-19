package federate

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/tests/testkit/grpctest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sdkReportServer struct {
	rtiv1.UnimplementedSupportServiceServer
	rtiv1.UnimplementedCallbackReportServiceServer
	report     func(context.Context, *rtiv1.ReportCallbackInvocationRequest) error
	query      func() error
	loseACK    atomic.Bool
	localEntry bool
	exchange   func(rtiv1.CallbackReportService_ExchangeServer) error
}

func (s *sdkReportServer) GetOrderName(context.Context, *rtiv1.GetOrderNameRequest) (*rtiv1.GetOrderNameResponse, error) {
	if s.query != nil {
		if err := s.query(); err != nil {
			return nil, err
		}
	}
	return &rtiv1.GetOrderNameResponse{OrderName: "Receive"}, nil
}

func (s *sdkReportServer) ReportCallbackInvocation(ctx context.Context, r *rtiv1.ReportCallbackInvocationRequest) (*rtiv1.Empty, error) {
	return &rtiv1.Empty{}, s.report(ctx, r)
}

func (s *sdkReportServer) Exchange(stream rtiv1.CallbackReportService_ExchangeServer) error {
	if s.exchange != nil {
		return s.exchange(stream)
	}
	header := metadata.Pairs("gorti-callback-report-stream", "1")
	if s.localEntry {
		header.Set("gorti-local-ro-callback-entry", "1")
	}
	if err := stream.SendHeader(header); err != nil {
		return err
	}
	var sequence uint64
	for {
		batch, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if batch.FirstSequence != sequence+1 {
			return status.Error(codes.InvalidArgument, "out of order")
		}
		ack := &rtiv1.CallbackReportAck{AcknowledgedSequence: sequence}
		for _, r := range batch.Reports {
			if err := s.report(stream.Context(), r); err != nil {
				st := status.Convert(err)
				ack.FailedSequence, ack.ErrorCode, ack.ErrorMessage = sequence+1, uint32(st.Code()), st.Message()
				break
			}
			sequence++
			ack.AcknowledgedSequence = sequence
		}
		if s.loseACK.CompareAndSwap(true, false) {
			return status.Error(codes.Unavailable, "lost ACK")
		}
		if err := stream.Send(ack); err != nil {
			return err
		}
		if ack.FailedSequence != 0 {
			return nil
		}
	}
}

func newSDKReportFixture(t *testing.T, server *sdkReportServer, streaming bool, options ...grpc.DialOption) (*Federate, context.Context) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	rtiv1.RegisterSupportServiceServer(s, server)
	if streaming {
		rtiv1.RegisterCallbackReportServiceServer(s, server)
	}
	grpctest.Serve(t, s, listener)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	options = append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, options...)
	cc, err := grpc.NewClient(listener.Addr().String(), options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	f := &Federate{conn: WrapGRPCClientConn(cc), federationName: "f", federateHandle: 7, sessionToken: []byte("test-session"), callbackEntrySupported: true, interactionContext: ctx}
	t.Cleanup(f.closeCallbackReports)
	return f, ctx
}

func reportTestData() Event {
	order := OrderReceive
	return ReceiveInteraction{CallbackInfo: CallbackInfo{receipt: make([]byte, 32), federate: 7}, Metadata: &ObjectCallbackMetadata{SentOrder: &order, ReceivedOrder: &order}}
}

func TestCallbackReportDrainWaitsForLocalBookkeeping(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("asynchronous report drain")
	}
	f, ctx := newSDKReportFixture(t, &sdkReportServer{report: func(context.Context, *rtiv1.ReportCallbackInvocationRequest) error { return nil }}, true)
	q := f.callbackReportTransport()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	task, err := q.enqueue(ctx, &rtiv1.ReportCallbackInvocationRequest{CallbackReceipt: make([]byte, 32)}, func(error) { <-release })
	if err != nil {
		t.Fatal(err)
	}
	for {
		q.mu.Lock()
		empty, changed := len(q.queue) == 0, q.changed
		q.mu.Unlock()
		if empty {
			break
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	drainCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := f.flushLocalCallbackEntries(drainCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain returned before local bookkeeping completed: %v", err)
	}
	unblock()
	select {
	case <-task.done.wait():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := f.flushLocalCallbackEntries(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackReportProfilesOutcomeBoundaryAndDrain(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	server := &sdkReportServer{report: func(ctx context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
		if md, _ := metadata.FromIncomingContext(ctx); len(md.Get("gorti-session-token-bin")) == 0 {
			return status.Error(codes.Unauthenticated, "missing token")
		}
		if !r.InvocationEntry {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	var bodies atomic.Int32
	done := make(chan error, 1)
	go func() {
		a, r := f.DispatchEvent(ctx, reportTestData(), func(Event) error { bodies.Add(1); return nil })
		if a != nil {
			done <- a
		} else {
			done <- r
		}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if callbackprofile.Async {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("async outcome blocked callback return")
		}
	} else {
		select {
		case err := <-done:
			t.Fatalf("sync returned before ACK: %v", err)
		default:
		}
	}
	flushed := make(chan error, 1)
	go func() { flushed <- f.FlushCallbackInvocationReports(ctx) }()
	select {
	case err := <-flushed:
		t.Fatalf("flush passed blocked outcome: %v", err)
	default:
	}
	unblock()
	if !callbackprofile.Async {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if bodies.Load() != 1 {
		t.Fatal("application callback replayed")
	}
	f.genericCallbackMu.Lock()
	entry := f.genericCallbacks[genericReceipt(reportTestData())]
	reported := entry != nil && entry.reported
	f.genericCallbackMu.Unlock()
	if !reported {
		t.Fatal("outcome not retained as acknowledged")
	}
	stats := f.CallbackReportTransportStats()
	if callbackprofile.Async && (stats.StreamReports < 2 || stats.AsyncOutcomes != 1) {
		t.Fatalf("stream not exercised: %+v", stats)
	}
	if !callbackprofile.Async && stats != (CallbackReportTransportStats{}) {
		t.Fatal("sync build used stream")
	}
}

func TestCallbackReportProfilesEntryRejectionAndLegacyFallback(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "stream"}[streaming], func(t *testing.T) {
			var reject atomic.Bool
			reject.Store(true)
			server := &sdkReportServer{report: func(_ context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
				if r.InvocationEntry && reject.Load() {
					return status.Error(codes.FailedPrecondition, "entry deferred")
				}
				return nil
			}}
			f, ctx := newSDKReportFixture(t, server, streaming)
			bodies := 0
			body := func(Event) error { bodies++; return nil }
			if a, r := f.DispatchEvent(ctx, reportTestData(), body); a != nil || r == nil || bodies != 0 {
				t.Fatalf("rejected entry invoked body: %v/%v/%d", a, r, bodies)
			}
			reject.Store(false)
			if a, r := f.DispatchEvent(ctx, reportTestData(), body); a != nil || r != nil {
				t.Fatalf("entry retry: %v/%v", a, r)
			}
			if err := f.FlushCallbackInvocationReports(ctx); err != nil {
				t.Fatal(err)
			}
			if a, r := f.DispatchEvent(ctx, reportTestData(), body); a != nil || r != nil || bodies != 1 {
				t.Fatalf("callback replay: %v/%v/%d", a, r, bodies)
			}
			if !streaming && f.CallbackReportTransportStats().AsyncOutcomes != 0 {
				t.Fatal("legacy server admitted async outcomes")
			}
		})
	}
}

func TestCallbackReportStreamLostACKRetainsIdentity(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("stream build only")
	}
	var calls atomic.Int32
	server := &sdkReportServer{report: func(context.Context, *rtiv1.ReportCallbackInvocationRequest) error { calls.Add(1); return nil }}
	server.loseACK.Store(true)
	f, ctx := newSDKReportFixture(t, server, true)
	if err := f.ReportCallbackEntry(ctx, reportTestData()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("lost ACK did not replay the report: %d", calls.Load())
	}
}

func TestCallbackReportOrderedInvocationsAndTailDrain(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("async outcome build")
	}
	var mu sync.Mutex
	var observed []uint64
	server := &sdkReportServer{report: func(_ context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
		id := binary.BigEndian.Uint64(r.CallbackReceipt)
		if !r.InvocationEntry {
			id++
		}
		mu.Lock()
		observed = append(observed, id)
		mu.Unlock()
		return nil
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	const count = 64
	for i := 0; i < count; i++ {
		event := reportTestData().(ReceiveInteraction)
		binary.BigEndian.PutUint64(event.receipt, uint64(i*2))
		if a, r := f.DispatchEvent(ctx, event, func(Event) error { return nil }); a != nil || r != nil {
			t.Fatalf("dispatch %d: %v/%v", i, a, r)
		}
	}
	// The last asynchronous outcome must progress without a later entry or flush.
	q := f.callbackReportTransport()
	q.mu.Lock()
	pending := append([]*callbackReportTask(nil), q.queue...)
	q.mu.Unlock()
	for _, task := range pending {
		select {
		case <-task.done.wait():
			if task.err != nil {
				t.Fatal(task.err)
			}
		case <-ctx.Done():
			t.Fatal("isolated tail outcome never completed")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 2*count {
		t.Fatalf("reports lost or duplicated: %v", observed)
	}
	for i, value := range observed {
		if value != uint64(i) {
			t.Fatalf("report %d out of order: %d", i, value)
		}
	}
}

func TestAsyncOutcomeEligibilityExcludesLifecycleAndRegionalData(t *testing.T) {
	if !asyncDataOutcome(reportTestData()) {
		t.Fatal("ordinary data excluded")
	}
	e := reportTestData().(ReceiveInteraction)
	e.Metadata.ReceivedRegions = []uint64{9}
	if asyncDataOutcome(e) || asyncDataOutcome(TimeAdvanceGrant{}) || asyncDataOutcome(FederationSaved{}) {
		t.Fatal("lifecycle-sensitive callback admitted asynchronously")
	}
}

func TestCallbackReportQueueIsBoundedAndCancellationReleasesWaiters(t *testing.T) {
	server := &sdkReportServer{report: func(ctx context.Context, _ *rtiv1.ReportCallbackInvocationRequest) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	q := f.callbackReportTransport()
	request := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 7, CallbackReceipt: make([]byte, 32), Success: true}
	var tasks []*callbackReportTask
	for i := 0; i < callbackReportQueueLimit; i++ {
		task, err := q.enqueue(ctx, request, func(error) {})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := q.enqueue(bounded, request, nil); err != context.DeadlineExceeded {
		t.Fatalf("unbounded queue: %v", err)
	}
	q.cancel()
	for _, task := range tasks {
		select {
		case <-task.done.wait():
			if task.err == nil {
				t.Fatal("canceled report acknowledged")
			}
		case <-ctx.Done():
			t.Fatal("canceled queue retained waiter")
		}
	}
}

func TestCallbackReportAsyncFailureRetainsOutcomeWithoutReinvocation(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("async outcome build")
	}
	var reject atomic.Bool
	reject.Store(true)
	server := &sdkReportServer{report: func(_ context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
		if !r.InvocationEntry && reject.Load() {
			return status.Error(codes.FailedPrecondition, "outcome deferred")
		}
		return nil
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	bodies := 0
	event := reportTestData()
	if a, r := f.DispatchEvent(ctx, event, func(Event) error { bodies++; return nil }); a != nil || r != nil {
		t.Fatalf("dispatch: %v/%v", a, r)
	}
	if err := f.FlushCallbackInvocationReports(ctx); err == nil {
		t.Fatal("asynchronous failure hidden")
	}
	f.genericCallbackMu.Lock()
	entry := f.genericCallbacks[genericReceipt(event)]
	pending := entry != nil && entry.complete && !entry.reported
	f.genericCallbackMu.Unlock()
	if !pending {
		t.Fatal("failed outcome lost")
	}
	for i := 0; i < 2; i++ {
		if _, err := f.GetOrderName(ctx, OrderReceive); err == nil {
			t.Fatal("service passed an unresolved asynchronous failure")
		}
	}
	reject.Store(false)
	// An already completed asynchronous failure may still be waiting to be
	// surfaced; the following explicit retry sends only the retained outcome.
	if err := f.FlushCallbackInvocationReports(ctx); err != nil {
		if err := f.FlushCallbackInvocationReports(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if a, r := f.DispatchEvent(ctx, event, func(Event) error { bodies++; return nil }); a != nil || r != nil || bodies != 1 {
		t.Fatalf("replayed body: %v/%v/%d", a, r, bodies)
	}
	if _, err := f.GetOrderName(ctx, OrderReceive); err != nil {
		t.Fatalf("successful outcome retry did not release service barrier: %v", err)
	}
}

func TestCallbackReportServiceBarrierPreventsOvertakeWithoutCheckpointDeadlock(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("async outcome build")
	}
	started, release := make(chan struct{}), make(chan struct{})
	var startOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var applied atomic.Bool
	var queries atomic.Int32
	server := &sdkReportServer{
		report: func(ctx context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
			if !r.InvocationEntry {
				startOnce.Do(func() { close(started) })
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				applied.Store(true)
			}
			return nil
		},
		query: func() error {
			queries.Add(1)
			if !applied.Load() {
				return status.Error(codes.Internal, "query overtook callback outcome")
			}
			return nil
		},
	}
	f, ctx := newSDKReportFixture(t, server, true)
	if a, r := f.DispatchEvent(ctx, reportTestData(), func(Event) error { return nil }); a != nil || r != nil {
		t.Fatalf("dispatch: %v/%v", a, r)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := f.GetOrderName(short, OrderReceive); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query did not wait for outcome: %v", err)
	}
	if queries.Load() != 0 {
		t.Fatal("query transmitted before outcome ACK")
	}
	done, locked := make(chan error, 1), make(chan struct{})
	go func() {
		f.checkpointMu.Lock()
		defer f.checkpointMu.Unlock()
		close(locked)
		_, err := f.GetOrderName(ctx, OrderReceive)
		done <- err
	}()
	<-locked
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("checkpoint lock deadlocked report progress")
	}
	if err := f.FlushCallbackInvocationReports(ctx); err != nil {
		t.Fatal(err)
	}
	if queries.Load() != 1 {
		t.Fatalf("query count %d", queries.Load())
	}
}

func TestCallbackReportBarrierDoesNotBlockAnotherAuthenticatedFederate(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("async outcome build")
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once, cleanup sync.Once
	unblock := func() { cleanup.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	server := &sdkReportServer{report: func(ctx context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
		if !r.InvocationEntry {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	if a, r := f.DispatchEvent(ctx, reportTestData(), func(Event) error { return nil }); a != nil || r != nil {
		t.Fatalf("dispatch: %v/%v", a, r)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	other := &Federate{conn: f.conn, federationName: "f", federateHandle: 8, sessionToken: []byte("other-session")}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := other.GetOrderName(bounded, OrderReceive); err != nil {
		t.Fatalf("unrelated federate blocked: %v", err)
	}
	unblock()
	if err := f.FlushCallbackInvocationReports(ctx); err != nil {
		t.Fatal(err)
	}
}
