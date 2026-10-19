package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	hlatime "github.com/cbchoi/gorti/rti/internal/hla/time"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type retrySourceStub struct {
	pending func() []core.FederationName
	retry   func(context.Context, core.FederationName) error
}

func (s retrySourceStub) PendingDeliveryRetries() []core.FederationName { return s.pending() }
func (s retrySourceStub) RetryPendingDeliveries(ctx context.Context, fed core.FederationName) error {
	return s.retry(ctx, fed)
}

type retryMembershipStub struct {
	generation func(core.FederationName) (uint64, bool)
	acquire    func(core.FederationName, uint64) (func(), error)
}

func (s retryMembershipStub) GenerationFor(fed core.FederationName) (uint64, bool) {
	return s.generation(fed)
}
func (s retryMembershipStub) AcquireGeneration(fed core.FederationName, generation uint64) (func(), error) {
	return s.acquire(fed, generation)
}

type retryAdmissionStub func(core.FederationName) (func(), error)

func (s retryAdmissionStub) AcquireService(fed core.FederationName) (func(), error) {
	return s(fed)
}

func retryMembership() retryMembershipStub {
	return retryMembershipStub{
		generation: func(core.FederationName) (uint64, bool) { return 7, true },
		acquire:    func(core.FederationName, uint64) (func(), error) { return func() {}, nil },
	}
}

func TestDeliveryRetryAdmissionOrderAndGenerationFence(t *testing.T) {
	for _, failure := range []string{auditReplayDisabledName, testCheckpointLabel, testRestoreLabel, "missing", testGenerationLabel, "retry"} {
		t.Run(failure, func(t *testing.T) {
			var calls []string
			add := func(name string) { calls = append(calls, name) }
			admission := retryAdmissionStub(func(core.FederationName) (func(), error) {
				add(testCheckpointLabel)
				if failure == testCheckpointLabel {
					return nil, core.ErrSaveAlreadyInProgress
				}
				if failure == testRestoreLabel {
					return nil, core.ErrRestoreAlreadyInProgress
				}
				return func() { add("release-checkpoint") }, nil
			})
			membership := retryMembershipStub{
				generation: func(core.FederationName) (uint64, bool) { add(testGenerationLabel); return 7, failure != "missing" },
				acquire: func(_ core.FederationName, generation uint64) (func(), error) {
					add("lease")
					if generation != 7 {
						t.Fatalf("wrong generation: %d", generation)
					}
					if failure == testGenerationLabel {
						return nil, core.ErrFederationGenerationMismatch
					}
					return func() { add("release-generation") }, nil
				},
			}
			source := retrySourceStub{retry: func(context.Context, core.FederationName) error {
				add("retry")
				if failure == "retry" {
					return core.ErrFederateOverflow
				}
				return nil
			}}
			worker := newDeliveryRetryWorker(source, membership, admission)
			err := worker.retryFederation(context.Background(), "fed")
			if (err != nil) != (failure != auditReplayDisabledName) {
				t.Fatalf("failure=%s error=%v", failure, err)
			}
			want := []string{testCheckpointLabel, testGenerationLabel, "lease", "retry", "release-generation", "release-checkpoint"}
			switch failure {
			case testCheckpointLabel, testRestoreLabel:
				want = []string{testCheckpointLabel}
			case "missing":
				want = []string{testCheckpointLabel, testGenerationLabel, "release-checkpoint"}
			case testGenerationLabel:
				want = []string{testCheckpointLabel, testGenerationLabel, "lease", "release-checkpoint"}
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v want=%v", calls, want)
			}
		})
	}
}

func TestDeliveryRetryWorkerLazyStartAndJoinedClose(t *testing.T) {
	var polls atomic.Int32
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	source := retrySourceStub{
		pending: func() []core.FederationName { polls.Add(1); return []core.FederationName{"marked"} },
		retry: func(ctx context.Context, fed core.FederationName) error {
			if fed != "marked" {
				t.Errorf("unmarked federation retried: %q", fed)
			}
			once.Do(func() { close(entered) })
			<-ctx.Done()
			close(cancelled)
			<-release
			return ctx.Err()
		},
	}
	worker := newDeliveryRetryWorker(source, retryMembership(), nil)
	if worker.done != nil || polls.Load() != 0 {
		t.Fatal("construction started the worker")
	}
	worker.Start(context.Background())
	worker.Start(context.Background())
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		worker.Close()
		t.Fatal("worker did not retry")
	}
	closed := make(chan struct{})
	go func() { worker.Close(); close(closed) }()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Close did not cancel the in-flight retry")
	}
	select {
	case <-closed:
		t.Fatal("Close returned before the retry completed")
	default:
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not join its worker")
	}
	worker.Close()
	before := polls.Load()
	worker.Start(context.Background())
	if polls.Load() != before {
		t.Fatal("closed worker restarted")
	}
}

func TestDeliveryRetryCancellationReleasesAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var released bool
	var generations int
	admission := retryAdmissionStub(func(core.FederationName) (func(), error) {
		cancel()
		return func() { released = true }, nil
	})
	membership := retryMembership()
	membership.generation = func(core.FederationName) (uint64, bool) { generations++; return 7, true }
	worker := newDeliveryRetryWorker(retrySourceStub{}, membership, admission)
	if err := worker.retryFederation(ctx, "fed"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if !released || generations != 0 {
		t.Fatalf("cancelled admission continued: released=%v generations=%d", released, generations)
	}
}

func TestDeliveryRetryCancellationDoesNotAcquireStaleGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var acquired bool
	membership := retryMembershipStub{
		generation: func(core.FederationName) (uint64, bool) { cancel(); return 7, true },
		acquire: func(core.FederationName, uint64) (func(), error) {
			acquired = true
			return func() {}, nil
		},
	}
	worker := newDeliveryRetryWorker(retrySourceStub{}, membership, nil)
	if err := worker.retryFederation(ctx, "fed"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if acquired {
		t.Fatal("shutdown acquired a generation lease after its lookup was cancelled")
	}
}

func TestDeliveryRetryWorkerRecoversGrantAfterRPCError(t *testing.T) {
	srv, err := newRTID(rtidConfig{OutboxEventCapacity: 2, OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.deliveryRetries.Close(); srv.sessions.Close(); _ = srv.plugins.Close() })
	if srv.deliveryRetries == nil || srv.deliveryRetries.done != nil {
		t.Fatal("constructor must compose, but not start, delivery retries")
	}
	listener := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(listener) }()
	t.Cleanup(func() { srv.grpcS.Stop(); _ = listener.Close(); <-done })
	conn, err := stdgrpc.NewClient("passthrough:///delivery-retry", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const fed core.FederationName = "retry"
	if _, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), Mode: rtiv1.Mode_MODE_VERBOSE}); err != nil {
		t.Fatal(err)
	}
	h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "receiver"})
	if err != nil {
		t.Fatal(err)
	}
	channel, unsubscribe, err := srv.outbox.Subscribe(ctx, fed, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unsubscribe() })
	drainScheduledEvents(channel)
	if err := srv.timeMgr.EnableConstrained(ctx, fed, h); err != nil {
		t.Fatal(err)
	}
	requireRetryConstraintEnabled(ctx, t, channel)
	for i := 0; i < 2; i++ {
		if err := srv.outbox.Send(ctx, fed, h, &fakeOutboundEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	times := rtiv1.NewTimeServiceClient(conn)
	_, err = times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h), LogicalTime: 10})
	if err == nil {
		t.Fatal("full transport queue did not fail TAR")
	}
	mgr := srv.timeMgr.(*hlatime.Manager)
	if state := mgr.Metrics(fed, h); !state.Advancing || state.CurrentTime != 0 {
		t.Fatalf("failed grant lost the accepted advance: %+v", state)
	}
	if pending := mgr.PendingDeliveryRetries(); !reflect.DeepEqual(pending, []core.FederationName{fed}) {
		t.Fatalf("retry markers=%v", pending)
	}
	// No second TAR or private time-manager reevaluation is used for recovery.
	drainScheduledEvents(channel)
	srv.deliveryRetries.Start(ctx)
	select {
	case events := <-channel:
		if len(events) != 1 {
			t.Fatalf("recovered events=%v", events)
		}
		wire, err := grpcadapter.EncodeOutboundEvent(events[0])
		if err != nil || wire.GetGrant() == nil || wire.GetGrant().LogicalTime != 10 {
			t.Fatalf("expected recovered TAG(10), got %#v", events[0])
		}
	case <-ctx.Done():
		t.Fatal("public worker did not recover the failed grant")
	}
	srv.deliveryRetries.Close()
	if state := mgr.Metrics(fed, h); state.Advancing || state.CurrentTime != 10 {
		t.Fatalf("recovered state=%+v", state)
	}
	if pending := mgr.PendingDeliveryRetries(); len(pending) != 0 {
		t.Fatalf("successful retry retained markers: %v", pending)
	}
	if extra := drainScheduledEvents(channel); len(extra) != 0 {
		t.Fatalf("duplicate grant: %v", extra)
	}
}

func TestDeliveryRetryWorkerReleasesROBacklogWithBoundedOutbox(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const fed core.FederationName = "ro-retry"
	out := delivery.NewWithBatch(1, 1, 0)
	channel, unsubscribe, err := out.Subscribe(ctx, fed, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unsubscribe(); err != nil {
			t.Error(err)
		}
	}()
	mgr, err := hlatime.New(hlatime.Options{Outbox: out, Clock: core.NewRealClock()})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnableConstrained(ctx, fed, 1); err != nil {
		t.Fatal(err)
	}
	requireRetryConstraintEnabled(ctx, t, channel)
	first, second := &fakeOutboundEvent{seq: 1}, &fakeOutboundEvent{seq: 2}
	r := mgr.ReserveTSO(fed, []core.MessageDelivery{
		{Recipient: 1, ReceiveOrder: true, Event: first},
		{Recipient: 1, ReceiveOrder: true, Event: second},
	})
	r.Commit(ctx)
	if err := mgr.EnableAsynchronousDelivery(ctx, fed, 1); err != nil {
		t.Fatalf("backpressured async enable rejected: %v", err)
	}
	if state := mgr.Metrics(fed, 1); !state.AsynchronousDelivery || state.ROQueued == 0 {
		t.Fatalf("expected accepted async mode and retained RO suffix: %+v", state)
	}
	worker := newDeliveryRetryWorker(mgr, retryMembership(), nil)
	worker.Start(ctx)
	defer worker.Close()
	for _, want := range []core.OutboundEvent{first, second} {
		select {
		case events := <-channel:
			if len(events) != 1 || events[0] != want {
				t.Fatalf("RO recovery reordered or duplicated delivery: %v want=%v", events, want)
			}
		case <-ctx.Done():
			t.Fatal("RO backlog failed to recover")
		}
	}
	worker.Close()
	if state := mgr.Metrics(fed, 1); state.ROQueued != 0 || !state.AsynchronousDelivery {
		t.Fatalf("recovered RO state=%+v", state)
	}
	if pending := mgr.PendingDeliveryRetries(); len(pending) != 0 {
		t.Fatalf("RO recovery retained retry marker: %v", pending)
	}
}

func TestDeliveryRetryPublicAdvanceRecoversROAndTSO(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const fed core.FederationName = "mixed-retry"
	out := delivery.NewWithBatch(2, 1, 0)
	channel, unsubscribe, err := out.Subscribe(ctx, fed, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unsubscribe(); err != nil {
			t.Error(err)
		}
	}()
	mgr, err := hlatime.New(hlatime.Options{Outbox: out, Clock: core.NewRealClock()})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnableConstrained(ctx, fed, 1); err != nil {
		t.Fatal(err)
	}
	requireRetryConstraintEnabled(ctx, t, channel)
	var queued []core.MessageDelivery
	var expected []core.OutboundEvent
	for i := uint64(1); i <= 7; i++ {
		event := &fakeOutboundEvent{seq: i}
		message := core.MessageDelivery{Recipient: 1, Event: event, ReceiveOrder: i <= 5}
		if i > 5 {
			message.Timestamp = core.LogicalTime(i)
		}
		queued = append(queued, message)
		expected = append(expected, event)
	}
	r := mgr.ReserveTSO(fed, queued)
	r.Commit(ctx)
	for i := 0; i < 2; i++ {
		if err := out.Send(ctx, fed, 1, &fakeOutboundEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.TimeAdvanceRequest(ctx, fed, 1, 10); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("public TAR error=%v", err)
	}
	if state := mgr.Metrics(fed, 1); !state.Advancing || state.ROQueued != 5 || state.TSOQueued != 2 || state.CurrentTime != 0 {
		t.Fatalf("overflow lost accepted work: %+v", state)
	}
	drainScheduledEvents(channel)
	worker := newDeliveryRetryWorker(mgr, retryMembership(), nil)
	worker.Start(ctx)
	defer worker.Close()
	for index := 0; index <= len(expected); index++ {
		select {
		case events := <-channel:
			if len(events) != 1 {
				t.Fatalf("unexpected batch=%v", events)
			}
			if index < len(expected) {
				if events[0] != expected[index] {
					t.Fatalf("message %d lost, reordered, or overtaken by TAG: %v", index, events[0])
				}
			} else if grant, ok := events[0].(*hlatime.TimeAdvanceGrant); !ok || grant.Time != 10 {
				t.Fatalf("missing final TAG(10): %v", events[0])
			}
		case <-ctx.Done():
			t.Fatal("worker did not recover the public advance")
		}
	}
	worker.Close()
	if state := mgr.Metrics(fed, 1); state.Advancing || state.ROQueued != 0 || state.TSOQueued != 0 || state.CurrentTime != 10 {
		t.Fatalf("recovery incomplete: %+v", state)
	}
	if len(mgr.PendingDeliveryRetries()) != 0 || len(drainScheduledEvents(channel)) != 0 {
		t.Fatal("recovery retained retry markers or emitted duplicate callbacks")
	}
}

func requireRetryConstraintEnabled(ctx context.Context, t *testing.T, channel <-chan []core.OutboundEvent) {
	t.Helper()
	select {
	case events := <-channel:
		if len(events) != 1 {
			t.Fatalf("activation callbacks=%v", events)
		}
		wire, err := grpcadapter.EncodeOutboundEvent(events[0])
		if err != nil || wire.GetTimeConstrainedEnabled() == nil {
			t.Fatalf("activation callback=%T", events[0])
		}
	case <-ctx.Done():
		t.Fatal("time constraint activation did not complete")
	}
}

func TestDeliveryRetryServeLifecycle(t *testing.T) {
	for _, failListen := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "listen-failure"}[failListen], func(t *testing.T) {
			srv, err := newRTID(rtidConfig{ListenAddr: "127.0.0.1:0", MetricsListenAddr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { srv.deliveryRetries.Close(); srv.sessions.Close(); _ = srv.plugins.Close() })
			polled := make(chan struct{})
			var once sync.Once
			srv.deliveryRetries = newDeliveryRetryWorker(retrySourceStub{
				pending: func() []core.FederationName { once.Do(func() { close(polled) }); return nil },
			}, retryMembership(), nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failListen {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				srv.cfg.ListenAddr = listener.Addr().String()
				if err := srv.Serve(ctx); err == nil {
					t.Fatal("occupied listener did not fail")
				}
				if srv.deliveryRetries.done != nil {
					t.Fatal("listener failure started the delivery worker")
				}
				return
			}
			done := make(chan error, 1)
			go func() { done <- srv.Serve(ctx) }()
			select {
			case <-polled:
			case err := <-done:
				t.Fatalf("Serve returned before worker start: %v", err)
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatal("Serve did not start delivery retries")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Serve error=%v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Serve did not stop")
			}
			select {
			case <-srv.deliveryRetries.done:
			default:
				t.Fatal("Serve returned without joining delivery retries")
			}
		})
	}
}
