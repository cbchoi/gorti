package grpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

func batchDataEvent() *rtiv1.FederateEvent {
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	transport := rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
		InteractionClassHandle: 9, Parameters: map[uint64][]byte{1: make([]byte, 4096)},
		Metadata: &rtiv1.ObjectCallbackMetadata{SentOrder: &order, ReceivedOrder: &order, TransportationType: &transport},
	}}}
}

func batchReportPair(t *testing.T, r *CallbackReceipts, event *rtiv1.FederateEvent) []*rtiv1.ReportCallbackInvocationRequest {
	t.Helper()
	ticket, commit, err := r.PrepareRequired(context.Background(), "f", 1, event)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.entries[string(ticket)].admitted = true
	r.mu.Unlock()
	commit(true)
	entry := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, CallbackReceipt: ticket, InvocationEntry: true, Success: true}
	outcome := proto.Clone(entry).(*rtiv1.ReportCallbackInvocationRequest)
	outcome.InvocationEntry = false
	return []*rtiv1.ReportCallbackInvocationRequest{entry, outcome}
}

func TestCallbackDataBatchAuthenticatesOnceAndDeduplicates(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 64)
	r.opts.LocalDataEntry = true
	acquired, released, applied, entered, completed := 0, 0, 0, 0, 0
	r.opts.DataReports = &DataCallbackReportBatch{
		Acquire: func(context.Context, core.FederationName, core.FederateHandle) (func(), error) {
			acquired++
			return func() { released++ }, nil
		},
		Apply: func(_ core.FederationName, _ core.FederateHandle, events []*rtiv1.FederateEvent) bool {
			applied++
			for _, event := range events {
				if event == nil {
					completed++
				} else {
					entered++
				}
			}
			return true
		},
	}
	var reports []*rtiv1.ReportCallbackInvocationRequest
	for range 16 {
		pair := batchReportPair(t, r, batchDataEvent())
		reports = append(reports, pair[0], pair[0], pair[1])
	}
	if got := r.tryDataReportPrefix(context.Background(), reports); got != 48 {
		t.Fatal(got)
	}
	if acquired != 1 || released != 1 || applied != 1 || entered != 16 || completed != 16 || r.pending != 0 {
		t.Fatalf("scope=%d/%d apply=%d entry=%d complete=%d pending=%d", acquired, released, applied, entered, completed, r.pending)
	}
	for _, entry := range r.entries {
		if !entry.entryApplied || !entry.applied || !entry.reported || entry.event != nil {
			t.Fatal("receipt state not finalized")
		}
	}
	service := &supportService{callbackReceipts: r}
	for _, report := range reports {
		if _, err := service.ReportCallbackInvocation(context.Background(), report); err != nil {
			t.Fatal(err)
		}
	}
	if r.pending != 0 {
		t.Fatal("retry double-decremented pending")
	}
}

func TestCallbackDataBatchDoesNotAcceptEvictedRetry(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 8)
	r.opts.LocalDataEntry, r.opts.MaxCompleted = true, 1
	r.opts.DataReports = &DataCallbackReportBatch{
		Acquire: func(context.Context, core.FederationName, core.FederateHandle) (func(), error) { return func() {}, nil },
		Apply:   func(core.FederationName, core.FederateHandle, []*rtiv1.FederateEvent) bool { return true },
	}
	first, second := batchReportPair(t, r, batchDataEvent()), batchReportPair(t, r, batchDataEvent())
	reports := append(append(first, second...), first[1])
	ctx, client := reportStreamTestClient(t, &supportService{callbackReceipts: r})
	stream, err := client.Exchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTransportCleanup(t, stream.CloseSend)
	if err := stream.Send(&rtiv1.CallbackReportBatch{FirstSequence: 1, Reports: reports}); err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil || ack.AcknowledgedSequence != 4 || ack.FailedSequence != 5 || ack.ErrorCode != uint32(codes.FailedPrecondition) {
		t.Fatalf("evicted retry accepted: %v / %v", ack, err)
	}
}

func TestCallbackDataBatchFallbackPreservesReceipt(t *testing.T) {
	for _, kind := range []string{"auth", "cancelled", "reporting", "unconfigured", "outcome-before-entry", "failure", "TSO", "region", "recovery", "control", "wire"} {
		t.Run(kind, func(t *testing.T) {
			r, _, _ := requiredCallbackFixture(t, 8)
			r.opts.LocalDataEntry = true
			calls := 0
			r.opts.DataReports = &DataCallbackReportBatch{
				Acquire: func(context.Context, core.FederationName, core.FederateHandle) (func(), error) {
					if kind == "auth" {
						return nil, errors.New("denied")
					}
					return func() {}, nil
				},
				Apply: func(core.FederationName, core.FederateHandle, []*rtiv1.FederateEvent) bool { calls++; return false },
			}
			event := batchDataEvent()
			switch kind {
			case "TSO":
				value := 1.0
				event.GetReceive().LogicalTime = &value
			case "region":
				event.GetReceive().Metadata.ReceivedRegions = &rtiv1.ReceivedRegions{}
			case "recovery":
				event.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: make([]byte, 32)}
			case "control":
				event = &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "ready"}}}
			case "unconfigured":
				r.opts.DataReports = nil
			}
			reports := batchReportPair(t, r, event)
			switch kind {
			case "outcome-before-entry":
				reports = reports[1:]
			case "failure":
				reports[0].Success = false
			case "wire":
				reports[0].WireVersion = 99
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			if got := r.tryDataReportPrefix(ctx, reports); got != 0 {
				t.Fatal(got)
			}
			entry := r.entries[string(reports[0].CallbackReceipt)]
			if r.pending != 1 || entry.entryApplied || entry.applied || entry.event == nil {
				t.Fatal("declined batch mutated receipt")
			}
			if (calls == 1) != (kind == "reporting") {
				t.Fatalf("unexpected apply calls: %d", calls)
			}
		})
	}
}

func TestCallbackDataBatchStreamKeepsExactFailurePrefix(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 8)
	r.opts.LocalDataEntry = true
	var leases atomic.Int32
	r.opts.DataReports = &DataCallbackReportBatch{
		Acquire: func(context.Context, core.FederationName, core.FederateHandle) (func(), error) {
			leases.Add(1)
			return func() { leases.Add(-1) }, nil
		},
		Apply: func(core.FederationName, core.FederateHandle, []*rtiv1.FederateEvent) bool { return true },
	}
	first, second := batchReportPair(t, r, batchDataEvent()), batchReportPair(t, r, batchDataEvent())
	bad := proto.Clone(first[1]).(*rtiv1.ReportCallbackInvocationRequest)
	bad.CallbackReceipt = make([]byte, 32)
	reports := append(append(first, bad), second...)
	ctx, client := reportStreamTestClient(t, &supportService{callbackReceipts: r})
	stream, err := client.Exchange(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTransportCleanup(t, stream.CloseSend)
	if err := stream.Send(&rtiv1.CallbackReportBatch{FirstSequence: 1, Reports: reports}); err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil || ack.AcknowledgedSequence != 2 || ack.FailedSequence != 3 || ack.ErrorCode != uint32(codes.FailedPrecondition) {
		t.Fatalf("%v / %v", ack, err)
	}
	if leases.Load() != 0 {
		t.Fatal("lifecycle lease retained across ACK")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending != 1 || r.entries[string(second[0].CallbackReceipt)].entryApplied {
		t.Fatal("invalid suffix affected state")
	}
}
