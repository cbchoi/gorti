package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

func requiredCallbackFixture(t *testing.T, capacity int) (*CallbackReceipts, *delivery.MultiOutbox, *MandatoryCallbackOutbox) {
	t.Helper()
	r, err := NewCallbackReceipts(CallbackReceiptOptions{
		MaxPending:   capacity,
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := delivery.NewWithBatch(8, 1, 0)
	for _, h := range []core.FederateHandle{1, 2} {
		raw.Bind("f", h)
	}
	t.Cleanup(func() { raw.UnbindFederation("f") })
	return r, raw, NewMandatoryCallbackOutbox(raw, func() *CallbackReceipts { return r })
}

func TestMandatoryCallbackReservationRollsBackAllRecipientEvidence(t *testing.T) {
	r, raw, out := requiredCallbackFixture(t, 1)
	event := &syncpkg.SynchronizedEvent{Sequence: 7}
	if _, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: event}, {Recipient: 2, Event: event}}); err == nil {
		t.Fatal("partial receipt admission succeeded")
	}
	if r.pending != 0 || len(r.entries) != 0 {
		t.Fatal("partial receipt reservation leaked")
	}
	for _, stat := range raw.OutboxStats() {
		if stat.QueueDepth != 0 {
			t.Fatal("callback escaped failed atomic reservation")
		}
	}
	reservation, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: event}})
	if err != nil {
		t.Fatal(err)
	}
	reservation.Release()
	reservation.Release()
	if r.pending != 0 {
		t.Fatal("idempotent release failed")
	}
}

type failingCallbackQueue struct{ failure error }

func (q *failingCallbackQueue) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return q.failure
}
func (q *failingCallbackQueue) Reserve(context.Context, core.FederationName, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return q, nil
}
func (q *failingCallbackQueue) Commit() error { return q.failure }
func (*failingCallbackQueue) Release()        {}

func TestMandatoryCallbackCommitFailureRemainsFailedAndReleasesEvidence(t *testing.T) {
	receipts, _, _ := requiredCallbackFixture(t, 8)
	failure := errors.New("injected queue commit failure")
	outbox := NewMandatoryCallbackOutbox(&failingCallbackQueue{failure: failure}, func() *CallbackReceipts { return receipts })
	reservation, err := outbox.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: &syncpkg.SynchronizedEvent{Sequence: 7}}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := reservation.Commit(); !errors.Is(err, failure) {
			t.Fatalf("commit error=%v", err)
		}
	}
	reservation.Release()
	if receipts.pending != 0 || len(receipts.entries) != 0 {
		t.Fatal("failed queue commit retained invocation evidence")
	}
}

func TestMandatoryCallbackFailedStreamRetainsReceiptAndReconnectOrder(t *testing.T) {
	r, raw, out := requiredCallbackFixture(t, 8)
	ch, cancel, err := raw.Subscribe(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	for _, seq := range []uint64{2, 1} {
		if err := out.Send(context.Background(), "f", 1, &syncpkg.SynchronizedEvent{Sequence: seq}); err != nil {
			t.Fatal(err)
		}
	}
	var wire []*rtiv1.FederateEvent
	for range 2 {
		batch := <-ch
		event, err := toDispatchFederateEvent(batch[0])
		if err != nil || len(event.GetCallbackReceipt()) != 32 {
			t.Fatal("pretracked callback missing", err)
		}
		commit, skip, err := r.preparedDelivery("f", 1, event)
		if err != nil || skip {
			t.Fatal("initial callback suppressed", err)
		}
		commit(false)
		wire = append(wire, event)
	}
	retry := r.pendingRedelivery("f", 1)
	if len(retry) != 2 || retry[0].Seq != 2 || retry[1].Seq != 1 {
		t.Fatalf("retry=%v", retry)
	}
	if string(retry[0].CallbackReceipt) != string(wire[0].CallbackReceipt) {
		t.Fatal("retry changed callback identity")
	}
	commit, skip, err := r.preparedDelivery("f", 1, retry[0])
	if err != nil || skip {
		t.Fatal(err)
	}
	commit(true)
	if err := r.Report(context.Background(), "f", 1, retry[0].CallbackReceipt, true, ""); err == nil {
		t.Fatal("required completion accepted before handler entry")
	}
	if err := r.ReportEntry(context.Background(), "f", 1, retry[0].CallbackReceipt); err != nil {
		t.Fatal(err)
	}
	r.markDisconnected("f", 1)
	if retry := r.pendingRedelivery("f", 1); len(retry) != 1 || retry[0].Seq != 1 {
		t.Fatal("entered callback replayed")
	}
	if _, skip, err := r.preparedDelivery("f", 1, wire[0]); err != nil || !skip {
		t.Fatal("duplicate entered callback not suppressed")
	}
	if err := r.Report(context.Background(), "f", 1, retry[0].CallbackReceipt, true, ""); err != nil {
		t.Fatal(err)
	}
	if r.pending != 1 {
		t.Fatal("entry/completion accounting mismatch")
	}
}

func TestRequiredCallbackRecipientIsNotAuthenticatedAsProducer(t *testing.T) {
	denied := errors.New("producer token is not recipient token")
	r, err := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate:              func(context.Context, core.FederationName, core.FederateHandle) error { return denied },
		ValidateRequiredRecipient: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		AcquireService: func(context.Context, core.FederationName) (func(), error) {
			t.Fatal("recursively acquired producer's admission")
			return nil, denied
		},
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "ready"}}}
	ticket, abort, err := r.PrepareRequired(context.Background(), "f", 2, event)
	if err != nil || len(ticket) != 32 {
		t.Fatal("recipient admission failed", err)
	}
	abort(false)
	if err := r.ReportEntry(context.Background(), "f", 2, ticket); !errors.Is(err, denied) {
		t.Fatal("transport entry authentication bypassed", err)
	}
}

func TestMandatoryCallbackDisconnectUsesRestoredHandle(t *testing.T) {
	for _, batching := range []bool{false, true} {
		t.Run(map[bool]string{false: "events", true: "batches"}[batching], func(t *testing.T) {
			r, _, _ := requiredCallbackFixture(t, 8)
			event := &rtiv1.FederateEvent{Seq: 9, Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "restored"}}}
			ticket, _, err := r.PrepareRequired(context.Background(), "f", 2, event)
			if err != nil {
				t.Fatal(err)
			}
			r.entries[string(ticket)].admitted = true
			handle := uint64(2)
			outbox := newFakeSubscribableOutbox()
			outbox.push(&requiredCallbackEvent{event: &rtiv1.FederateEvent{Seq: 8, Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{FederateHandle: &handle}}}})
			outbox.closeChan()
			service := newStreamService(outbox)
			service.receipts = r
			request := &rtiv1.EventsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1}
			if batching {
				err = service.EventBatches(request, &fakeBatchStream{ctx: context.Background()})
			} else {
				err = service.Events(request, newFakeStream(context.Background()))
			}
			if err != nil {
				t.Fatal(err)
			}
			retry := r.pendingRedelivery("f", 2)
			if len(retry) != 1 || string(retry[0].CallbackReceipt) != string(ticket) {
				t.Fatal("disconnect lost callback obligation for restored handle")
			}
		})
	}
}

func TestSelectiveMandatoryCallbacksPreserveMixedBatchOrder(t *testing.T) {
	r, raw, _ := requiredCallbackFixture(t, 1)
	ch, cancel, err := raw.Subscribe(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	out := NewSelectiveCallbackOutbox(raw, func() *CallbackReceipts { return r }, func(e core.OutboundEvent) bool { return e.Seq() == 7 })
	ordinary := &syncpkg.SynchronizedEvent{Sequence: 8}
	reservation, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{
		{Recipient: 1, Event: &syncpkg.SynchronizedEvent{Sequence: 7}},
		{Recipient: 1, Event: ordinary},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	if r.pending != 1 {
		t.Fatal("ordinary callback consumed mandatory receipt capacity")
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	first, second := <-ch, <-ch
	if len(first) != 1 || len(second) != 1 || first[0].Seq() != 7 || second[0] != ordinary {
		t.Fatal("selective receipt adapter changed event identity or order")
	}
	wire, err := toDispatchFederateEvent(first[0])
	if err != nil || len(wire.CallbackReceipt) != 32 {
		t.Fatal("required callback lost receipt", err)
	}
}
