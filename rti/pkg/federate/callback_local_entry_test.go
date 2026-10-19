package federate

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func localEntryTestEvent(f *Federate, id uint64) Event {
	receipt := make([]byte, 32)
	binary.BigEndian.PutUint64(receipt, id)
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	wire := &rtiv1.FederateEvent{CallbackReceipt: receipt, Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
		Metadata: &rtiv1.ObjectCallbackMetadata{SentOrder: &order, ReceivedOrder: &order},
	}}}
	f.checkpointMu.Lock()
	info := f.trackRuntimeCallback(wire, f.callbackInfo(wire))
	f.checkpointMu.Unlock()
	localOrder := OrderReceive
	return ReceiveInteraction{CallbackInfo: info, Metadata: &ObjectCallbackMetadata{SentOrder: &localOrder, ReceivedOrder: &localOrder}}
}

func TestLocalCallbackEntryQueuesBeforeACKAndDrainsBeforeService(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("negotiated async profile")
	}
	blocked, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var mu sync.Mutex
	var observed []uint64
	var queryCalls atomic.Int32
	server := &sdkReportServer{localEntry: true, report: func(ctx context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
		id := binary.BigEndian.Uint64(r.CallbackReceipt)
		if id == 1 && r.InvocationEntry {
			once.Do(func() { close(blocked) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if id > 0 {
			value := id * 2
			if !r.InvocationEntry {
				value++
			}
			mu.Lock()
			observed = append(observed, value)
			mu.Unlock()
		}
		return nil
	}, query: func() error { queryCalls.Add(1); return nil }}
	f, ctx := newSDKReportFixture(t, server, true)
	// The first confirmed report establishes authenticated capability support.
	if err := f.ReportCallbackEntry(ctx, reportTestData()); err != nil {
		t.Fatal(err)
	}
	const count = 64
	var bodies atomic.Int32
	finished := make(chan error, 1)
	go func() {
		for i := uint64(1); i <= count; i++ {
			a, r := f.DispatchEvent(ctx, localEntryTestEvent(f, i), func(Event) error { bodies.Add(1); return nil })
			if err := errors.Join(a, r); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	}()
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("local callback entry waited for the remote ACK")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetOrderName(f.sessionContext(queryCtx), &rtiv1.GetOrderNameRequest{WireVersion: 1})
	if !errors.Is(err, context.DeadlineExceeded) || queryCalls.Load() != 0 {
		t.Fatalf("service overtook local entry reports: %v, calls=%d", err, queryCalls.Load())
	}
	unblock()
	if err := f.FlushCallbackInvocationReports(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := rtiv1.NewSupportServiceClient(f.conn.rpcConnection()).GetOrderName(f.sessionContext(ctx), &rtiv1.GetOrderNameRequest{WireVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if bodies.Load() != count {
		t.Fatalf("callback count=%d", bodies.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	// Retries may duplicate reports, but an outcome must never precede its entry.
	entered := make(map[uint64]bool)
	for _, value := range observed {
		if value%2 == 0 {
			entered[value/2] = true
		} else if !entered[value/2] {
			t.Fatalf("outcome overtook entry: %v", observed)
		}
	}
	if len(entered) != count {
		t.Fatalf("missing entry reports: %d", len(entered))
	}
}

func TestLocalCallbackEntryFailureRetriesReportsNotApplication(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("negotiated async profile")
	}
	var reject atomic.Bool
	reject.Store(true)
	var remoteEntered atomic.Bool
	server := &sdkReportServer{localEntry: true, report: func(_ context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
		if binary.BigEndian.Uint64(r.CallbackReceipt) == 0 {
			return nil
		}
		if r.InvocationEntry {
			if reject.Load() {
				return status.Error(codes.Unavailable, "entry transport unavailable")
			}
			remoteEntered.Store(true)
		} else if !remoteEntered.Load() {
			return status.Error(codes.FailedPrecondition, "entry missing")
		}
		return nil
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	if err := f.ReportCallbackEntry(ctx, reportTestData()); err != nil {
		t.Fatal(err)
	}
	event := localEntryTestEvent(f, 1)
	bodies := 0
	a, _ := f.DispatchEvent(ctx, event, func(Event) error { bodies++; return errors.New(testApplicationFailure) })
	if a == nil || bodies != 1 {
		t.Fatalf("local execution missing: %v, %d", a, bodies)
	}
	if err := f.FlushCallbackInvocationReports(ctx); err == nil {
		t.Fatal("unacknowledged entry reported success")
	}
	reject.Store(false)
	if err := f.FlushCallbackInvocationReports(ctx); err != nil {
		t.Fatal(err)
	}
	if a, r := f.DispatchEvent(ctx, event, func(Event) error { bodies++; return nil }); a != nil || r != nil || bodies != 1 {
		t.Fatalf("report recovery replayed application: %v/%v/%d", a, r, bodies)
	}
}

func TestLocalCallbackEntryEligibilityAndFallback(t *testing.T) {
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	base := &rtiv1.FederateEvent{CallbackReceipt: make([]byte, 32), Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
		Metadata: &rtiv1.ObjectCallbackMetadata{SentOrder: &order, ReceivedOrder: &order},
	}}}
	for _, tc := range []struct {
		name string
		edit func(*rtiv1.FederateEvent)
	}{
		{"missing-receipt", func(w *rtiv1.FederateEvent) { w.CallbackReceipt = nil }},
		{"missing-order", func(w *rtiv1.FederateEvent) { w.GetReceive().Metadata.SentOrder = nil }},
		{"timestamp", func(w *rtiv1.FederateEvent) { value := 1.0; w.GetReceive().LogicalTime = &value }},
		{"exact-time", func(w *rtiv1.FederateEvent) { w.GetReceive().LogicalTimeValue = &rtiv1.LogicalTimeValue{} }},
		{"received-tso", func(w *rtiv1.FederateEvent) {
			value := rtiv1.OrderType_ORDER_TYPE_TIMESTAMP
			w.GetReceive().Metadata.ReceivedOrder = &value
		}},
		{"sent-tso", func(w *rtiv1.FederateEvent) {
			value := rtiv1.OrderType_ORDER_TYPE_TIMESTAMP
			w.GetReceive().Metadata.SentOrder = &value
		}},
		{"regions", func(w *rtiv1.FederateEvent) {
			w.GetReceive().Metadata.ReceivedRegions = &rtiv1.ReceivedRegions{RegionHandles: []uint64{1}}
		}},
		{"ownership", func(w *rtiv1.FederateEvent) { w.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{} }},
		{"retraction", func(w *rtiv1.FederateEvent) {
			value := uint64(1)
			w.GetReceive().Metadata.MessageRetractionHandle = &value
		}},
		{"recovery", func(w *rtiv1.FederateEvent) {
			w.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: make([]byte, 32)}
		}},
		{"control", func(w *rtiv1.FederateEvent) {
			w.Event = &rtiv1.FederateEvent_SaveInitiate{SaveInitiate: &rtiv1.InitiateFederateSave{}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := proto.Clone(base).(*rtiv1.FederateEvent)
			tc.edit(wire)
			if localEntryWireEligible(wire) {
				t.Fatal("unsafe callback admitted locally")
			}
		})
	}
	if !localEntryWireEligible(base) {
		t.Fatal("ordinary RO callback not eligible")
	}
	for _, negotiated := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "untracked"}[negotiated], func(t *testing.T) {
			server := &sdkReportServer{localEntry: negotiated, report: func(_ context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
				if binary.BigEndian.Uint64(r.CallbackReceipt) != 0 && r.InvocationEntry {
					return status.Error(codes.FailedPrecondition, "not admitted")
				}
				return nil
			}}
			f, ctx := newSDKReportFixture(t, server, true)
			if err := f.ReportCallbackEntry(ctx, reportTestData()); err != nil {
				t.Fatal(err)
			}
			event := localEntryTestEvent(f, 1).(ReceiveInteraction)
			if negotiated {
				event.CallbackInfo.runtimeOwner = nil
			}
			bodies := 0
			if a, r := f.DispatchEvent(ctx, event, func(Event) error { bodies++; return nil }); a != nil || r == nil || bodies != 0 {
				t.Fatalf("fallback did not preserve admission: %v/%v/%d", a, r, bodies)
			}
		})
	}
}

func TestLocalCallbackEntryBackpressureHonorsCallerDeadline(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("negotiated async profile")
	}
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	server := &sdkReportServer{localEntry: true, report: func(ctx context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
		if binary.BigEndian.Uint64(r.CallbackReceipt) != 0 {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	f, ctx := newSDKReportFixture(t, server, true)
	if err := f.ReportCallbackEntry(ctx, reportTestData()); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= callbackReportQueueLimit/2; i++ {
		if a, r := f.DispatchEvent(ctx, localEntryTestEvent(f, uint64(i)), func(Event) error { return nil }); a != nil || r != nil {
			t.Fatalf("fill queue: %v/%v", a, r)
		}
	}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	bodies := 0
	if a, r := f.DispatchEvent(short, localEntryTestEvent(f, 1000), func(Event) error { bodies++; return nil }); a != nil || !errors.Is(r, context.DeadlineExceeded) || bodies != 0 {
		t.Fatalf("full queue ignored deadline: %v/%v/%d", a, r, bodies)
	}
	unblock()
	if err := f.FlushCallbackInvocationReports(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLocalCallbackEntryFencesStaleAndDisconnectedEvents(t *testing.T) {
	for _, kind := range []string{"unauthenticated", "stale-epoch", "disconnected"} {
		t.Run(kind, func(t *testing.T) {
			server := &sdkReportServer{localEntry: true, report: func(_ context.Context, r *rtiv1.ReportCallbackInvocationRequest) error {
				if kind == "unauthenticated" || binary.BigEndian.Uint64(r.CallbackReceipt) != 0 {
					return status.Error(codes.Unauthenticated, "session expired")
				}
				return nil
			}}
			f, ctx := newSDKReportFixture(t, server, true)
			if kind != "unauthenticated" {
				if err := f.ReportCallbackEntry(ctx, reportTestData()); err != nil {
					t.Fatal(err)
				}
			}
			event := localEntryTestEvent(f, 1)
			switch kind {
			case "stale-epoch":
				f.runtimeEpoch++
			case "disconnected":
				f.drainDone = make(chan struct{})
				close(f.drainDone)
			}
			bodies := 0
			if a, r := f.DispatchEvent(ctx, event, func(Event) error { bodies++; return nil }); a != nil || r == nil || bodies != 0 {
				t.Fatalf("stale callback executed: %v/%v/%d", a, r, bodies)
			}
		})
	}
}
