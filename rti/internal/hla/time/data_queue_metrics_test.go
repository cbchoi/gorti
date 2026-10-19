package time

import (
	"context"
	"errors"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type queueMetricData struct{ order core.OrderType }

func (*queueMetricData) Seq() uint64                                 { return 0 }
func (e *queueMetricData) DataReceivedOrder() (core.OrderType, bool) { return e.order, true }
func (e *queueMetricData) SetReceivedOrder(order core.OrderType)     { e.order = order }

type queueReadyObservation func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error)

func (f queueReadyObservation) PendingDataCounts(ctx context.Context, fed core.FederationName, h core.FederateHandle) (core.DataQueueCounts, error) {
	return f(ctx, fed, h)
}

func testDataQueueCounts[T core.LogicalTimeScalar](t *testing.T) {
	t.Helper()
	m, err := NewOf[T](OptionsOf[T]{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	ext := extOfOf[T](m)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("f", 1)
	ns.roBuffer = []bufferedTSOEventOf[T]{{event: &queueMetricData{core.OrderReceive}}, {event: &queueMetricData{core.OrderReceive}}, {event: &TimeAdvanceGrantOf[T]{}}}
	ns.tsoBuffer = []bufferedTSOEventOf[T]{{event: &queueMetricData{core.OrderTimestamp}}, {event: &queueMetricData{core.OrderReceive}}}
	ext.mu.Unlock()
	ready := queueReadyObservation(func(_ context.Context, fed core.FederationName, h core.FederateHandle) (core.DataQueueCounts, error) {
		if fed != "f" || h != 1 {
			t.Fatal("observer scope changed")
		}
		if !m.states.mu.TryLock() {
			t.Fatal("state lock held during external observation")
		}
		m.states.mu.Unlock()
		if !ext.mu.TryLock() {
			t.Fatal("extension lock held during external observation")
		}
		ext.mu.Unlock()
		if ext.evaluatorLock(fed).TryLock() {
			ext.evaluatorLock(fed).Unlock()
			t.Fatal("observation omitted evaluator lease")
		}
		return core.DataQueueCounts{RO: 3, TSO: 4}, nil
	})
	got, err := m.DataQueueMetrics(context.Background(), "f", 1, ready)
	if err != nil || got != (core.DataQueueCounts{RO: 6, TSO: 5}) {
		t.Fatalf("counts=%+v err=%v", got, err)
	}
	if len(ns.roBuffer) != 3 || len(ns.tsoBuffer) != 2 {
		t.Fatal("observation mutated queue")
	}
}

func TestDataQueueMetricsCountsFinalReceivedOrder(t *testing.T) {
	t.Run("Float64", testDataQueueCounts[core.LogicalTime])
	t.Run("Integer64", testDataQueueCounts[int64])
}

func TestDataQueueMetricsErrorsNeverFabricateZeroCoverage(t *testing.T) {
	m, _ := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	ctx := context.Background()
	if _, err := m.DataQueueMetrics(ctx, "f", 1, nil); err == nil {
		t.Fatal("missing observer fabricated coverage")
	}
	cause := errors.New("entry capability absent")
	if _, err := m.DataQueueMetrics(ctx, "f", 1, queueReadyObservation(func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
		return core.DataQueueCounts{}, cause
	})); !errors.Is(err, cause) {
		t.Fatalf("coverage failure lost: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.DataQueueMetrics(cancelled, "f", 1, queueReadyObservation(func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
		t.Fatal("cancelled observation called ready source")
		return core.DataQueueCounts{}, nil
	})); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observation: %v", err)
	}
	ext := extOf(m)
	ext.mu.Lock()
	ext.getOrCreateLocked("f", 1).roBuffer = []bufferedTSOEvent{{event: &queueMetricData{core.OrderUnspecified}}}
	ext.mu.Unlock()
	ready := queueReadyObservation(func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
		return core.DataQueueCounts{}, nil
	})
	if _, err := m.DataQueueMetrics(ctx, "f", 1, ready); err == nil {
		t.Fatal("unknown received order fabricated coverage")
	}
	ext.mu.Lock()
	ext.getOrCreateLocked("f", 1).roBuffer[0].event = &queueMetricData{core.OrderReceive}
	ext.mu.Unlock()
	ready = queueReadyObservation(func(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
		return core.DataQueueCounts{RO: ^uint64(0)}, nil
	})
	if _, err := m.DataQueueMetrics(ctx, "f", 1, ready); err == nil {
		t.Fatal("overflow fabricated coverage")
	}
}

type queueHandoffOutbox struct {
	mu               sync.Mutex
	counts           core.DataQueueCounts
	started, release chan struct{}
	once             sync.Once
	fail             bool
}
type queueHandoffReservation struct {
	out    *queueHandoffOutbox
	counts core.DataQueueCounts
}

func (o *queueHandoffOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	r, err := o.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: e}})
	if err != nil {
		return err
	}
	defer r.Release()
	return r.Commit()
}
func (o *queueHandoffOutbox) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	r := &queueHandoffReservation{out: o}
	for _, d := range ds {
		if e, ok := d.Event.(core.DataQueueMessage); ok {
			if order, data := e.DataReceivedOrder(); data {
				if order == core.OrderReceive {
					r.counts.RO++
				} else {
					r.counts.TSO++
				}
			}
		}
	}
	return r, nil
}
func (r *queueHandoffReservation) Commit() error {
	if r.counts == (core.DataQueueCounts{}) {
		return nil
	}
	r.out.mu.Lock()
	if !r.out.fail {
		r.out.counts.RO += r.counts.RO
		r.out.counts.TSO += r.counts.TSO
	}
	r.out.mu.Unlock()
	r.out.once.Do(func() { close(r.out.started) })
	<-r.out.release
	if r.out.fail {
		return core.ErrFederateOverflow
	}
	return nil
}
func (*queueHandoffReservation) Release() {}
func (o *queueHandoffOutbox) PendingDataCounts(context.Context, core.FederationName, core.FederateHandle) (core.DataQueueCounts, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.counts, nil
}

func testDataQueueHandoff[T core.LogicalTimeScalar](t *testing.T) {
	for _, tso := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			name := "RO"
			if tso {
				name = "TSO"
			}
			if fail {
				name += "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				out := &queueHandoffOutbox{started: make(chan struct{}), release: make(chan struct{}), fail: fail}
				m, err := NewOf[T](OptionsOf[T]{Clock: core.NewManualClock(zeroTime()), Outbox: out})
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				if err := m.EnableConstrained(ctx, "f", 1); err != nil {
					t.Fatal(err)
				}
				event := &queueMetricData{core.OrderReceive}
				if tso {
					event.order = core.OrderTimestamp
				}
				r := m.ReserveTSO("f", []core.MessageDeliveryOf[T]{{Recipient: 1, Sender: 2, Timestamp: 5, ReceiveOrder: !tso, Event: event}})
				r.Commit(ctx)
				want := core.DataQueueCounts{RO: 1}
				if tso {
					want = core.DataQueueCounts{TSO: 1}
				}
				if got, err := m.DataQueueMetrics(ctx, "f", 1, out); err != nil || got != want {
					t.Fatalf("waiting=%+v err=%v", got, err)
				}
				transfer := make(chan error, 1)
				go func() {
					if tso {
						transfer <- m.TimeAdvanceRequest(ctx, "f", 1, 5)
					} else {
						transfer <- m.EnableAsynchronousDelivery(ctx, "f", 1)
					}
				}()
				select {
				case <-out.started:
				case <-stdtime.After(stdtime.Second):
					t.Fatal("production drain did not reach commit")
				}
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(out.release) }) }
				defer release()
				observation := make(chan core.DataQueueCounts, 1)
				observed := make(chan error, 1)
				go func() { counts, err := m.DataQueueMetrics(ctx, "f", 1, out); observation <- counts; observed <- err }()
				select {
				case <-observed:
					t.Fatal("observed transient handoff inside evaluator")
				case <-stdtime.After(20 * stdtime.Millisecond):
				}
				release()
				select {
				case <-transfer:
				case <-stdtime.After(stdtime.Second):
					t.Fatal("drain did not finish")
				}
				select {
				case err := <-observed:
					if err != nil {
						t.Fatal(err)
					}
				case <-stdtime.After(stdtime.Second):
					t.Fatal("observation deadlocked")
				}
				if got := <-observation; got != want {
					t.Fatalf("handoff=%+v want=%+v", got, want)
				}
			})
		}
	}
}

func TestDataQueueMetricsDisjointProductionHandoff(t *testing.T) {
	t.Run("Float64", testDataQueueHandoff[core.LogicalTime])
	t.Run("Integer64", testDataQueueHandoff[int64])
}
