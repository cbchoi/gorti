package time

import (
	"context"
	"errors"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type stageReviewOutbox struct {
	mu              sync.Mutex
	ready, attempts uint64
	failed          bool
	paused, release chan struct{}
}

type stageReviewReservation struct {
	out  *stageReviewOutbox
	data uint64
}

func (o *stageReviewOutbox) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	var n uint64
	for _, d := range ds {
		if marker, ok := d.Event.(core.DataQueueMessage); ok {
			if _, data := marker.DataReceivedOrder(); data {
				n++
			}
		}
	}
	// Exercise the production single-message fallback after batch rejection.
	if n > 1 {
		return nil, core.ErrFederateOverflow
	}
	return &stageReviewReservation{out: o, data: n}, nil
}

func (o *stageReviewOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	r, err := o.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: event}})
	if err != nil {
		return err
	}
	defer r.Release()
	return r.Commit()
}

func (r *stageReviewReservation) Commit() error {
	if r.data == 0 {
		return nil
	}
	r.out.mu.Lock()
	r.out.attempts++
	fail := r.out.attempts == 2 && !r.out.failed
	if fail {
		r.out.failed = true
	} else {
		r.out.ready += r.data
	}
	r.out.mu.Unlock()
	if fail {
		close(r.out.paused)
		<-r.out.release
		return core.ErrFederateOverflow
	}
	return nil
}

func (*stageReviewReservation) Release() {}

func (o *stageReviewOutbox) PendingDataCounts(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return core.DataQueueCounts{RO: o.ready}, nil
}

func TestDataQueueStageReviewPartialHandoffRetryConservesCount(t *testing.T) {
	out := &stageReviewOutbox{paused: make(chan struct{}), release: make(chan struct{})}
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	var deliveries []core.MessageDeliveryOf[core.LogicalTime]
	for i := 0; i < 3; i++ {
		deliveries = append(deliveries, core.MessageDeliveryOf[core.LogicalTime]{Recipient: 1, Sender: 2, ReceiveOrder: true, Event: &queueMetricData{core.OrderReceive}})
	}
	m.ReserveTSO("f", deliveries).Commit(ctx)
	assertCount := func(phase string) {
		t.Helper()
		got, err := m.DataQueueMetrics(ctx, "f", 1, out)
		if err != nil || got != (core.DataQueueCounts{RO: 3}) {
			t.Fatalf("%s: counts=%+v err=%v", phase, got, err)
		}
	}
	assertCount("all waiting")
	transfer := make(chan error, 1)
	go func() { transfer <- m.EnableAsynchronousDelivery(ctx, "f", 1) }()
	var once sync.Once
	release := func() { once.Do(func() { close(out.release) }) }
	defer release()
	select {
	case <-out.paused:
	case <-stdtime.After(stdtime.Second):
		t.Fatal("single-message failure boundary not reached")
	}
	type result struct {
		counts core.DataQueueCounts
		err    error
	}
	observed := make(chan result, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		counts, err := m.DataQueueMetrics(ctx, "f", 1, out)
		observed <- result{counts, err}
	}()
	<-started
	select {
	case got := <-observed:
		t.Fatalf("observed partial handoff before suffix restore: %+v", got)
	case <-stdtime.After(20 * stdtime.Millisecond):
	}
	release()
	select {
	case <-transfer:
	case <-stdtime.After(stdtime.Second):
		t.Fatal("handoff did not finish")
	}
	select {
	case got := <-observed:
		if got.err != nil || got.counts != (core.DataQueueCounts{RO: 3}) {
			t.Fatalf("one ready plus restored suffix: %+v", got)
		}
	case <-stdtime.After(stdtime.Second):
		t.Fatal("queue observation did not resume")
	}
	if err := m.RetryPendingDeliveries(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	assertCount("after retry")
	out.mu.Lock()
	defer out.mu.Unlock()
	if out.ready != 3 || out.attempts != 4 {
		t.Fatalf("prefix replay or suffix loss: ready=%d attempts=%d", out.ready, out.attempts)
	}
}

func TestDataQueueStageReviewInvalidOrderFailsBeforeReadyObservation(t *testing.T) {
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range []core.OrderType{core.OrderUnspecified, core.OrderType(255)} {
		ext := extOf(m)
		ext.mu.Lock()
		ns := ext.getOrCreateLocked("f", 1)
		ns.roBuffer = []bufferedTSOEvent{{event: &queueMetricData{core.OrderReceive}}, {event: &queueMetricData{order}}}
		ext.mu.Unlock()
		called := false
		ready := queueReadyObservation(func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
			called = true
			return core.DataQueueCounts{RO: 9}, nil
		})
		got, err := m.DataQueueMetrics(context.Background(), "f", 1, ready)
		if !errors.Is(err, core.ErrDataQueueCoverageUnavailable) || got != (core.DataQueueCounts{}) || called {
			t.Fatalf("invalid order=%d: counts=%+v err=%v readyCalled=%t", order, got, err, called)
		}
		if len(ns.roBuffer) != 2 {
			t.Fatal("failed observation mutated waiting data")
		}
	}
}

func TestDataQueueStageReviewMissingCoverageNeverSucceedsWithZero(t *testing.T) {
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.DataQueueMetrics(context.Background(), "f", 1, nil); !errors.Is(err, core.ErrDataQueueCoverageUnavailable) || got != (core.DataQueueCounts{}) {
		t.Fatalf("missing observer: counts=%+v err=%v", got, err)
	}
	ready := queueReadyObservation(func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
		return core.DataQueueCounts{RO: 9, TSO: 11}, core.ErrDataQueueCoverageUnavailable
	})
	if got, err := m.DataQueueMetrics(context.Background(), "f", 1, ready); !errors.Is(err, core.ErrDataQueueCoverageUnavailable) || got != (core.DataQueueCounts{}) {
		t.Fatalf("failed observer partial counts presented as coverage: counts=%+v err=%v", got, err)
	}
}
