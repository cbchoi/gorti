package grpc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type deliveryBatchSink struct {
	fakeBatchStream
	onSend func(*rtiv1.FederateEventBatch) error
}

func (s *deliveryBatchSink) Send(batch *rtiv1.FederateEventBatch) error {
	if s.onSend != nil {
		if err := s.onSend(batch); err != nil {
			return err
		}
	}
	return s.fakeBatchStream.Send(batch)
}

func deliveryBatchFixture(t *testing.T, count int) (*CallbackReceipts, *rtiv1.FederateEventBatch) {
	t.Helper()
	r, _, _ := requiredCallbackFixture(t, count+8)
	batch := &rtiv1.FederateEventBatch{}
	for i := 0; i < count; i++ {
		event := batchDataEvent()
		event.Seq = uint64(i + 1)
		event.GetReceive().Parameters[1] = make([]byte, 64)
		ticket, _, err := r.PrepareRequired(context.Background(), "f", 1, event)
		if err != nil {
			t.Fatal(err)
		}
		event.CallbackReceipt = ticket
		entry := r.entries[string(ticket)]
		entry.admitted, entry.checkpointPublished, entry.retry = true, true, true
		batch.Events = append(batch.Events, event)
	}
	return r, batch
}

func TestDataDeliveryBatchSharesAdmissionWithoutHoldingSend(t *testing.T) {
	r, batch := deliveryBatchFixture(t, 32)
	acquired, held := 0, 0
	r.opts.AcquireService = func(context.Context, core.FederationName) (func(), error) {
		acquired++
		held++
		return func() { held-- }, nil
	}
	sink := &deliveryBatchSink{fakeBatchStream: fakeBatchStream{ctx: context.Background()}, onSend: func(*rtiv1.FederateEventBatch) error {
		if held != 0 || !r.mu.TryLock() {
			t.Fatal("admission or receipt lock held across Send")
		}
		r.mu.Unlock()
		return nil
	}}
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); err != nil {
		t.Fatal(err)
	}
	if acquired != 1 || held != 0 || r.pending != 32 || len(sink.sent) != 1 {
		t.Fatalf("admissions=%d held=%d pending=%d", acquired, held, r.pending)
	}
	for _, event := range batch.Events {
		entry := r.entries[string(event.CallbackReceipt)]
		if !entry.sent || entry.retry || entry.applied || !entry.checkpointPublished {
			t.Fatal("incorrect delivery state")
		}
	}
}

func TestDataDeliveryBatchFailedTransportRetainsRetryOrder(t *testing.T) {
	r, batch := deliveryBatchFixture(t, 4)
	failure := errors.New("lost transport")
	sink := &fakeBatchStream{ctx: context.Background(), sendErr: failure}
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	retry := r.pendingRedelivery("f", 1)
	if len(retry) != 4 || r.pending != 4 {
		t.Fatalf("retry=%d pending=%d", len(retry), r.pending)
	}
	for i, event := range retry {
		if !proto.Equal(event, batch.Events[i]) {
			t.Fatal("retry changed body, identity or order")
		}
	}
	sink.sendErr = nil
	if err := stream.Send(&rtiv1.FederateEventBatch{Events: retry}); err != nil {
		t.Fatal(err)
	}
	if len(r.pendingRedelivery("f", 1)) != 0 || r.pending != 4 {
		t.Fatal("successful retry lost pending invocation")
	}
}

func TestDataDeliveryBatchEarlyReportsSurviveSendResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			r, batch := deliveryBatchFixture(t, 4)
			failure := errors.New("ambiguous Send after authenticated reports")
			sink := &deliveryBatchSink{fakeBatchStream: fakeBatchStream{ctx: context.Background()}, onSend: func(batch *rtiv1.FederateEventBatch) error {
				for i, event := range batch.Events[:3] {
					if err := r.ReportEntry(context.Background(), "f", 1, event.CallbackReceipt); err != nil {
						t.Fatal(err)
					}
					if i < 2 {
						if err := r.Report(context.Background(), "f", 1, event.CallbackReceipt, true, ""); err != nil {
							t.Fatal(err)
						}
					}
				}
				if failed {
					return failure
				}
				return nil
			}}
			stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
			err := stream.Send(batch)
			if (failed && !errors.Is(err, failure)) || (!failed && err != nil) {
				t.Fatal(err)
			}
			if r.pending != 2 || len(r.entries) != 4 {
				t.Fatalf("pending=%d retained=%d; send result revoked authenticated reports", r.pending, len(r.entries))
			}
			for _, event := range batch.Events[:2] {
				if err := r.Report(context.Background(), "f", 1, event.CallbackReceipt, true, ""); err != nil {
					t.Fatal("completed outcome retry failed", err)
				}
			}
			image, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeCallbackCheckpoint(image)
			if err != nil || len(decoded.Deliveries) != 4 {
				t.Fatalf("checkpoint=%s err=%v", image, err)
			}
			for i, phase := range []string{callbackPhaseCompleted, callbackPhaseCompleted, callbackPhaseOutcome, callbackPhasePending} {
				if decoded.Deliveries[i].Phase != phase {
					t.Fatalf("phase[%d]=%s", i, decoded.Deliveries[i].Phase)
				}
			}
			if failed {
				retry := r.pendingRedelivery("f", 1)
				if len(retry) != 1 || retry[0].Seq != 4 {
					t.Fatal("invoked callback scheduled for reinvocation")
				}
			}
		})
	}
}

func TestDataDeliveryBatchInvalidMiddleRollsBackPreparedPrefix(t *testing.T) {
	for _, kind := range []string{"payload", "ticket", "scope", "unadmitted", "stale-source"} {
		t.Run(kind, func(t *testing.T) {
			r, batch := deliveryBatchFixture(t, 3)
			entry := r.entries[string(batch.Events[1].CallbackReceipt)]
			want := codes.FailedPrecondition
			switch kind {
			case "payload":
				batch.Events[1].GetReceive().Parameters[1][0]++
				want = codes.InvalidArgument
			case "ticket":
				batch.Events[1].CallbackReceipt = make([]byte, 32)
			case "scope":
				entry.h = 2
			case "unadmitted":
				entry.admitted = false
			case "stale-source":
				guard := &streamKnowledgeGuard{}
				guard.stale.Store(true)
				entry.source = delivery.GuardEvent(&object.CallbackEvent{}, guard)
			}
			sink := &fakeBatchStream{ctx: context.Background()}
			stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
			if err := stream.Send(batch); status.Code(err) != want {
				t.Fatalf("err=%v", err)
			}
			if sink.sendCalls != 0 || r.pending != 3 {
				t.Fatal("invalid batch reached wire or lost evidence")
			}
			for _, entry := range r.entries {
				if !entry.retry || entry.sent {
					t.Fatal("prefix rollback/suffix isolation failed")
				}
			}
		})
	}
}

func TestDataDeliveryBatchSkipsEnteredAndPreservesReady(t *testing.T) {
	for _, ready := range []bool{false, true} {
		r, batch := deliveryBatchFixture(t, 3)
		for _, event := range batch.Events {
			r.entries[string(event.CallbackReceipt)].entryApplied = true
		}
		batch.Ready = ready
		sink := &fakeBatchStream{ctx: context.Background()}
		stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
		if err := stream.Send(batch); err != nil {
			t.Fatal(err)
		}
		if (sink.sendCalls == 1) != ready || len(batch.Events) != 0 || r.pending != 3 {
			t.Fatal("skip/ready behavior changed")
		}
	}
	r, batch := deliveryBatchFixture(t, 3)
	r.entries[string(batch.Events[0].CallbackReceipt)].entryApplied = true
	sink := &fakeBatchStream{ctx: context.Background()}
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 2 || batch.Events[0].Seq != 2 || batch.Events[1].Seq != 3 {
		t.Fatal("compaction changed order")
	}
}

func TestDataDeliveryBatchTeardownDuringSendDoesNotReviveEntries(t *testing.T) {
	for _, kind := range []string{"resign", "destroy", "restore"} {
		t.Run(kind, func(t *testing.T) {
			r, batch := deliveryBatchFixture(t, 3)
			sink := &deliveryBatchSink{fakeBatchStream: fakeBatchStream{ctx: context.Background()}, onSend: func(*rtiv1.FederateEventBatch) error {
				switch kind {
				case "resign":
					r.ClearFederate("f", 1)
				case "destroy":
					r.ClearFederation("f")
				case "restore":
					r.restoreCommitted("f", "saved", map[core.FederateHandle]core.FederateHandle{1: 2})
				}
				return nil
			}}
			stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
			if err := stream.Send(batch); err != nil {
				t.Fatal(err)
			}
			if r.pending != 0 || len(r.entries) != 0 {
				t.Fatal("retired lifecycle revived by late commit")
			}
		})
	}
}

func TestDataDeliveryBatchAdmissionFailureInvalidatesOnce(t *testing.T) {
	for _, nilRelease := range []bool{false, true} {
		r, batch := deliveryBatchFixture(t, 3)
		invalid, pending := 0, 3
		r.opts.Pending = func(_ core.FederationName, _ core.FederateHandle, delta int) { pending += delta }
		r.opts.OnUnavailable = func(core.FederationName, core.FederateHandle, error) { invalid++ }
		r.opts.AcquireService = func(context.Context, core.FederationName) (func(), error) {
			if nilRelease {
				return nil, nil
			}
			return nil, errors.New("lifecycle unavailable")
		}
		stream := &receiptBatchesStream{StreamService_EventBatchesServer: &fakeBatchStream{ctx: context.Background()}, receipts: r, fed: "f", handle: 1}
		if err := stream.Send(batch); err != nil {
			t.Fatal("transport result changed", err)
		}
		if r.pending != 0 || pending != 0 || invalid != 3 || len(r.entries) != 0 {
			t.Fatal("failed accounting admission leaked or double-retired evidence")
		}
	}
}

func TestDataDeliveryCompletedReportsSurviveAdmissionFailure(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			r, batch := deliveryBatchFixture(t, count)
			r.opts.AcquireService = func(context.Context, core.FederationName) (func(), error) {
				return nil, errors.New("admission closed after authenticated completion")
			}
			r.opts.OnUnavailable = func(core.FederationName, core.FederateHandle, error) {
				t.Fatal("completed callback invalidated")
			}
			sink := &deliveryBatchSink{fakeBatchStream: fakeBatchStream{ctx: context.Background()}, onSend: func(batch *rtiv1.FederateEventBatch) error {
				for _, event := range batch.Events {
					if err := r.ReportEntry(context.Background(), "f", 1, event.CallbackReceipt); err != nil {
						t.Fatal(err)
					}
					if err := r.Report(context.Background(), "f", 1, event.CallbackReceipt, true, ""); err != nil {
						t.Fatal(err)
					}
				}
				return nil
			}}
			stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
			if err := stream.Send(batch); err != nil {
				t.Fatal(err)
			}
			if r.pending != 0 || len(r.entries) != count {
				t.Fatal("completed receipt lost or pending retired twice")
			}
			for _, event := range batch.Events {
				if err := r.Report(context.Background(), "f", 1, event.CallbackReceipt, true, ""); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDataDeliveryBatchTransientAdmissionFailureKeepsSuffix(t *testing.T) {
	r, batch := deliveryBatchFixture(t, 3)
	calls, invalid := 0, 0
	r.opts.AcquireService = func(context.Context, core.FederationName) (func(), error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient admission timeout")
		}
		return func() {}, nil
	}
	r.opts.OnUnavailable = func(core.FederationName, core.FederateHandle, error) { invalid++ }
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: &fakeBatchStream{ctx: context.Background()}, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || invalid != 1 || r.pending != 2 || len(r.entries) != 2 {
		t.Fatalf("calls=%d invalid=%d pending=%d entries=%d", calls, invalid, r.pending, len(r.entries))
	}
	if r.entries[string(batch.Events[0].CallbackReceipt)] != nil {
		t.Fatal("failed first admission was hidden")
	}
	for _, event := range batch.Events[1:] {
		if entry := r.entries[string(event.CallbackReceipt)]; entry == nil || !entry.sent {
			t.Fatal("valid suffix invalidated by preceding admission failure")
		}
	}
}

func TestRequiredControlLateSendFailureRetainsCompletion(t *testing.T) {
	r, batch := deliveryBatchFixture(t, 1)
	event := batch.Events[0]
	event.Event = &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "barrier"}}
	entry := r.entries[string(event.CallbackReceipt)]
	entry.event = proto.Clone(event).(*rtiv1.FederateEvent)
	entry.event.CallbackReceipt = nil
	failure := errors.New("late control Send failure")
	sink := &deliveryBatchSink{fakeBatchStream: fakeBatchStream{ctx: context.Background()}, onSend: func(*rtiv1.FederateEventBatch) error {
		if err := r.ReportEntry(context.Background(), "f", 1, event.CallbackReceipt); err != nil {
			t.Fatal(err)
		}
		if err := r.Report(context.Background(), "f", 1, event.CallbackReceipt, true, ""); err != nil {
			t.Fatal(err)
		}
		return failure
	}}
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if r.pending != 0 || r.entries[string(event.CallbackReceipt)] != entry || !entry.applied || entry.retry {
		t.Fatal("completed control receipt lost")
	}
	if err := r.Report(context.Background(), "f", 1, event.CallbackReceipt, true, ""); err != nil {
		t.Fatal(err)
	}
}

func TestDataDeliveryBatchRestoreHandleFallback(t *testing.T) {
	r, batch := deliveryBatchFixture(t, 3)
	batch.Events[0].Event = &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{FederateHandle: proto.Uint64(2)}}
	for _, event := range batch.Events {
		entry := r.entries[string(event.CallbackReceipt)]
		entry.h = 2
		entry.event = proto.Clone(event).(*rtiv1.FederateEvent)
		entry.event.CallbackReceipt = nil
	}
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: &fakeBatchStream{ctx: context.Background()}, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); err != nil {
		t.Fatal(err)
	}
	if stream.handle != 2 {
		t.Fatal("restore handle transition lost")
	}
	for _, entry := range r.entries {
		if !entry.sent {
			t.Fatal("post-restore data not committed")
		}
	}
}

func TestDataDeliveryBatchObsoleteGuardRetiresOnlyAffectedCallback(t *testing.T) {
	r, batch := deliveryBatchFixture(t, 3)
	guard := &streamKnowledgeGuard{}
	guard.stale.Store(true)
	r.entries[string(batch.Events[1].CallbackReceipt)].source = delivery.GuardCancellableEvent(&object.CallbackEvent{}, guard, object.ErrStaleObjectKnowledge)
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: &fakeBatchStream{ctx: context.Background()}, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); err != nil {
		t.Fatal(err)
	}
	if r.pending != 2 || len(batch.Events) != 2 || batch.Events[0].Seq != 1 || batch.Events[1].Seq != 3 {
		t.Fatal("obsolete callback not isolated")
	}
}

func TestDataDeliveryBatchCancellationRetainsEvidence(t *testing.T) {
	r, batch := deliveryBatchFixture(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink := &deliveryBatchSink{fakeBatchStream: fakeBatchStream{ctx: ctx}, onSend: func(*rtiv1.FederateEventBatch) error { return ctx.Err() }}
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: sink, receipts: r, fed: "f", handle: 1}
	if err := stream.Send(batch); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.pending != 3 || len(r.pendingRedelivery("f", 1)) != 3 {
		t.Fatal("canceled transport lost replay evidence")
	}
}

func TestDataDeliveryBatchFallbackBoundaries(t *testing.T) {
	for _, kind := range []string{"single", "oversize", "control", "TSO", "region", "recovery", "ownership", "optional"} {
		t.Run(kind, func(t *testing.T) {
			count := 3
			if kind == "single" {
				count = 1
			}
			if kind == "oversize" {
				count = 65
			}
			r, batch := deliveryBatchFixture(t, count)
			event := batch.Events[0]
			switch kind {
			case "control":
				event.Event = &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "ready"}}
			case "TSO":
				event.GetReceive().LogicalTime = proto.Float64(1)
			case "region":
				event.GetReceive().Metadata.ReceivedRegions = &rtiv1.ReceivedRegions{}
			case "recovery":
				event.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: make([]byte, 32)}
			case "ownership":
				event.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{CallbackId: 1}
			case "optional":
				delete(r.entries, string(event.CallbackReceipt))
				r.pending--
				event.CallbackReceipt = nil
			}
			if entry := r.entries[string(event.CallbackReceipt)]; entry != nil {
				entry.event = proto.Clone(event).(*rtiv1.FederateEvent)
				entry.event.CallbackReceipt = nil
			}
			acquired := 0
			r.opts.AcquireService = func(context.Context, core.FederationName) (func(), error) { acquired++; return func() {}, nil }
			stream := &receiptBatchesStream{StreamService_EventBatchesServer: &fakeBatchStream{ctx: context.Background()}, receipts: r, fed: "f", handle: 1}
			if err := stream.Send(batch); err != nil {
				t.Fatal(err)
			}
			want := count
			if kind == "optional" {
				want++
			}
			if acquired != want {
				t.Fatalf("fallback admissions=%d want=%d", acquired, want)
			}
		})
	}
}

func BenchmarkDataDeliveryReceiptBatch(b *testing.B) {
	r := &CallbackReceipts{entries: map[string]*callbackReceipt{}, opts: CallbackReceiptOptions{
		AcquireService: func(context.Context, core.FederationName) (func(), error) { return func() {}, nil },
	}}
	batch := &rtiv1.FederateEventBatch{}
	for i := range 32 {
		event := batchDataEvent()
		event.GetReceive().Parameters[1] = make([]byte, 64)
		ticket := make([]byte, 32)
		ticket[0] = byte(i + 1)
		saved := proto.Clone(event).(*rtiv1.FederateEvent)
		event.CallbackReceipt = ticket
		r.entries[string(ticket)] = &callbackReceipt{fed: "f", h: 1, event: saved, required: true, admitted: true, checkpointPublished: true}
		batch.Events = append(batch.Events, event)
	}
	stream := &receiptBatchesStream{StreamService_EventBatchesServer: &discardBatchStream{ctx: context.Background()}, receipts: r, fed: "f", handle: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := stream.Send(batch); err != nil {
			b.Fatal(err)
		}
	}
}
