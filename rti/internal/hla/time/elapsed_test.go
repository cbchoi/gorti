package time

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func elapsedManager(t *testing.T, out core.Outbox) (*Manager, *core.ManualClock) {
	t.Helper()
	c := core.NewManualClock(stdtime.Unix(10, 0))
	m, err := New(Options{Clock: c, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.OnFederateJoined("elapsed", 1); err != nil {
		t.Fatal(err)
	}
	return m, c
}

func requireElapsed(t *testing.T, m *Manager, granted, advancing stdtime.Duration) ElapsedMeasurement {
	t.Helper()
	x, err := m.MeasureElapsed("elapsed", 1)
	if err != nil {
		t.Fatal(err)
	}
	if x.Granted != granted || x.Advancing != advancing {
		t.Fatalf("elapsed granted=%v advancing=%v; want %v, %v", x.Granted, x.Advancing, granted, advancing)
	}
	return x
}

func TestElapsedJoinReadsDoNotConsume(t *testing.T) {
	m, c := elapsedManager(t, nopOutbox{})
	c.Advance(1500 * stdtime.Microsecond)
	for i := 0; i < 3; i++ {
		requireElapsed(t, m, 1500*stdtime.Microsecond, 0)
	}
	if err := m.OnFederateJoined("elapsed", 1); err != nil {
		t.Fatal(err)
	}
	c.Advance(500 * stdtime.Microsecond)
	requireElapsed(t, m, 2*stdtime.Millisecond, 0)
}

func TestElapsedAdvanceModesAndIndependentPublication(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, c := elapsedManager(t, nopOutbox{})
			ctx := context.Background()
			if err := m.EnableConstrained(ctx, "elapsed", 1); err != nil {
				t.Fatal(err)
			}
			if err := m.EnableRegulation(ctx, "elapsed", 2, 0); err != nil {
				t.Fatal(err)
			}
			c.Advance(2250 * stdtime.Microsecond)
			// Measure time in an admitted request before evaluation. FQR
			// does not wait for a stationary peer once it is evaluated.
			if err := m.recordAdvance("elapsed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			c.Advance(3500 * stdtime.Microsecond)
			first := requireElapsed(t, m, 2250*stdtime.Microsecond, 3500*stdtime.Microsecond)
			if err := m.AcknowledgeElapsed("elapsed", 1, first, ElapsedAttributes{Granted: true}); err != nil {
				t.Fatal(err)
			}
			requireElapsed(t, m, 250*stdtime.Microsecond, 3500*stdtime.Microsecond)
			if err := m.DisableRegulation(ctx, "elapsed", 2); err != nil {
				t.Fatal(err)
			}
			c.Advance(2 * stdtime.Millisecond)
			second := requireElapsed(t, m, 2250*stdtime.Microsecond, 3500*stdtime.Microsecond)
			if err := m.AcknowledgeElapsed("elapsed", 1, second, ElapsedAttributes{Granted: true, Advancing: true}); err != nil {
				t.Fatal(err)
			}
			if err := m.AcknowledgeElapsed("elapsed", 1, first, ElapsedAttributes{Granted: true, Advancing: true}); err != nil {
				t.Fatal(err)
			}
			requireElapsed(t, m, 250*stdtime.Microsecond, 500*stdtime.Microsecond)
		})
	}
}

func TestElapsedRestoreDurationsRebaseAndFenceStaleTokens(t *testing.T) {
	m, c := elapsedManager(t, nopOutbox{})
	ctx := context.Background()
	if err := m.EnableConstrained(ctx, "elapsed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "elapsed", 2, 0); err != nil {
		t.Fatal(err)
	}
	c.Advance(2500 * stdtime.Microsecond)
	first := requireElapsed(t, m, 2500*stdtime.Microsecond, 0)
	if err := m.AcknowledgeElapsed("elapsed", 1, first, ElapsedAttributes{Granted: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "elapsed", 1, 20); err != nil {
		t.Fatal(err)
	}
	c.Advance(3750 * stdtime.Microsecond)
	state, err := m.ExportState("elapsed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Federates[0].Elapsed == nil || state.Federates[0].Elapsed.GrantedPublished != 2*stdtime.Millisecond {
		t.Fatalf("missing duration baseline: %+v", state.Federates[0])
	}
	old := requireElapsed(t, m, 500*stdtime.Microsecond, 3750*stdtime.Microsecond)
	commit, err := m.PrepareImportState("elapsed", state, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The prepared image is detached from the caller's state.
	state.Federates[0].Elapsed.Granted = 99 * stdtime.Second
	c.Advance(10 * stdtime.Hour)
	commit()
	commit()
	if err := m.AcknowledgeElapsed("elapsed", 1, old, ElapsedAttributes{Granted: true, Advancing: true}); !errors.Is(err, ErrElapsedStaleMeasurement) {
		t.Fatal(err)
	}
	requireElapsed(t, m, 500*stdtime.Microsecond, 3750*stdtime.Microsecond)
	c.Advance(1250 * stdtime.Microsecond)
	requireElapsed(t, m, 500*stdtime.Microsecond, 5*stdtime.Millisecond)
	// Restart onto a clock with a different origin also excludes offline time.
	saved, err := m.ExportState("elapsed", nil)
	if err != nil {
		t.Fatal(err)
	}
	newClock := core.NewManualClock(stdtime.Unix(2000000, 0))
	restarted, err := New(Options{Clock: newClock, Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.ImportState("elapsed", saved, nil); err != nil {
		t.Fatal(err)
	}
	newClock.Advance(stdtime.Millisecond)
	requireElapsed(t, restarted, 500*stdtime.Microsecond, 6*stdtime.Millisecond)
	if err := restarted.AcknowledgeElapsed("elapsed", 1, old, ElapsedAttributes{Advancing: true}); !errors.Is(err, ErrElapsedStaleMeasurement) {
		t.Fatal(err)
	}
}

func TestElapsedLegacyUnknownResignDestroyAndFreshJoin(t *testing.T) {
	m, c := elapsedManager(t, nopOutbox{})
	c.Advance(stdtime.Millisecond)
	old := requireElapsed(t, m, stdtime.Millisecond, 0)
	saved, err := m.ExportState("elapsed", nil)
	if err != nil {
		t.Fatal(err)
	}
	saved.Federates[0].Elapsed = nil
	if err := m.ImportState("elapsed", saved, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.MeasureElapsed("elapsed", 1); !errors.Is(err, ErrElapsedCoverage) {
		t.Fatal(err)
	}
	if err := m.OnFederateJoined("elapsed", 1); !errors.Is(err, ErrElapsedCoverage) {
		t.Fatal("legacy history backfilled", err)
	}
	m.OnFederateResign(context.Background(), "elapsed", 1)
	c.Advance(2 * stdtime.Hour)
	if _, err := m.MeasureElapsed("elapsed", 1); !errors.Is(err, ErrElapsedCoverage) {
		t.Fatal(err)
	}
	if err := m.OnFederateJoined("elapsed", 1); err != nil {
		t.Fatal(err)
	}
	c.Advance(3 * stdtime.Millisecond)
	requireElapsed(t, m, 3*stdtime.Millisecond, 0)
	if err := m.AcknowledgeElapsed("elapsed", 1, old, ElapsedAttributes{Granted: true}); !errors.Is(err, ErrElapsedStaleMeasurement) {
		t.Fatal(err)
	}
	m.OnFederationDestroyed("elapsed")
	if _, err := m.MeasureElapsed("elapsed", 1); !errors.Is(err, ErrElapsedCoverage) {
		t.Fatal(err)
	}
	if state, err := m.ExportState("elapsed", nil); err != nil || state != nil {
		t.Fatal("read created state", state, err)
	}
}

func TestElapsedFractionalTransitionsAccumulateAndRejectedRequestIsGranted(t *testing.T) {
	m, c := elapsedManager(t, nopOutbox{})
	ctx := context.Background()
	if err := m.EnableConstrained(ctx, "elapsed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "elapsed", 2, 0); err != nil {
		t.Fatal(err)
	}
	c.Advance(100 * stdtime.Microsecond)
	if err := m.TimeAdvanceRequest(ctx, "elapsed", 1, -1); err == nil {
		t.Fatal("invalid request accepted")
	}
	requireElapsed(t, m, 100*stdtime.Microsecond, 0)
	for i := 0; i < 20; i++ {
		c.Advance(100 * stdtime.Microsecond)
		if err := m.TimeAdvanceRequest(ctx, "elapsed", 1, 10); err != nil {
			t.Fatal(err)
		}
		c.Advance(150 * stdtime.Microsecond)
		if err := m.emitGrant(ctx, "elapsed", 1, 0, false); err != nil {
			t.Fatal(err)
		}
		c.Advance(50 * stdtime.Microsecond)
		if err := m.TimeAdvanceRequest(ctx, "elapsed", 1, 11); !errors.Is(err, ErrDuplicateNER) {
			t.Fatal(err)
		}
		if err := m.emitGrant(ctx, "elapsed", 1, 10, true); err != nil {
			t.Fatal(err)
		}
	}
	requireElapsed(t, m, 2100*stdtime.Microsecond, 4*stdtime.Millisecond)
}

func TestElapsedOverflowClockRegressionAndImportValidation(t *testing.T) {
	m, c := elapsedManager(t, nopOutbox{})
	c.Advance((stdtime.Duration(math.MaxInt32) + 1) * stdtime.Millisecond)
	x, err := m.MeasureElapsed("elapsed", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.AcknowledgeElapsed("elapsed", 1, x, ElapsedAttributes{Granted: true}); err == nil {
		t.Fatal("wire overflow acknowledged")
	}
	requireElapsed(t, m, x.Granted, 0)
	state, err := m.ExportState("elapsed", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []ElapsedTimeState{
		{Granted: -1}, {Advancing: -1}, {GrantedPublished: stdtime.Millisecond},
		{Granted: stdtime.Millisecond, GrantedPublished: 1}, {AdvancingPublished: stdtime.Millisecond},
	} {
		state.Federates[0].Elapsed = &invalid
		if _, err := m.PrepareImportState("elapsed", state, nil); err == nil {
			t.Fatalf("invalid duration state accepted: %+v", invalid)
		}
	}
	state.Federates[0].Elapsed = &ElapsedTimeState{Granted: stdtime.Duration(math.MaxInt64) - 1}
	if err := m.ImportState("elapsed", state, nil); err != nil {
		t.Fatal(err)
	}
	c.Advance(2)
	if _, err := m.MeasureElapsed("elapsed", 1); !errors.Is(err, ErrElapsedCoverage) {
		t.Fatal("duration overflow", err)
	}
	m.OnFederateResign(context.Background(), "elapsed", 1)
	if err := m.OnFederateJoined("elapsed", 1); err != nil {
		t.Fatal(err)
	}
	c.Advance(-stdtime.Second)
	if _, err := m.MeasureElapsed("elapsed", 1); !errors.Is(err, ErrElapsedCoverage) {
		t.Fatal("clock regressed", err)
	}
	saved, err := m.ExportState("elapsed", nil)
	if err != nil || !saved.Federates[0].Elapsed.Invalid {
		t.Fatal("coverage failure not persisted", saved, err)
	}
}

func TestElapsedConcurrentReadAcknowledgeAndRestore(t *testing.T) {
	m, c := elapsedManager(t, nopOutbox{})
	var wg sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				c.Advance(50 * stdtime.Microsecond)
				x, err := m.MeasureElapsed("elapsed", 1)
				if err != nil {
					t.Error(err)
					return
				}
				if err := m.AcknowledgeElapsed("elapsed", 1, x, ElapsedAttributes{Granted: true}); err != nil && !errors.Is(err, ErrElapsedStaleMeasurement) {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		s, err := m.ExportState("elapsed", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.ImportState("elapsed", s, nil); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if _, err := m.MeasureElapsed("elapsed", 1); err != nil {
		t.Fatal(err)
	}
}

type elapsedSendOutbox struct {
	clock *core.ManualClock
	fail  bool
}

func (o *elapsedSendOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) error {
	if _, ok := e.(*TimeAdvanceGrant); ok {
		o.clock.Advance(3 * stdtime.Millisecond)
		if o.fail {
			return core.ErrOutboxUnavailable
		}
	}
	return nil
}

type elapsedReservationOutbox struct{ elapsedSendOutbox }
type elapsedReservation struct{ out *elapsedReservationOutbox }

func (o *elapsedReservationOutbox) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	if r, ok := enableFixtureAdmission(ds); ok {
		return r, nil
	}
	o.clock.Advance(stdtime.Millisecond)
	return &elapsedReservation{o}, nil
}
func (r *elapsedReservation) Commit() error {
	return r.out.Send(context.Background(), "elapsed", 1, &TimeAdvanceGrant{})
}
func (*elapsedReservation) Release() {}

func TestElapsedGrantQueueBoundaryAndFailedEnqueue(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "send", true: "reserved"}[reserved], func(t *testing.T) {
			out := &elapsedSendOutbox{fail: true}
			var port core.Outbox = out
			if reserved {
				port = &elapsedReservationOutbox{}
				out = &port.(*elapsedReservationOutbox).elapsedSendOutbox
				out.fail = true
			}
			m, c := elapsedManager(t, port)
			out.clock = c
			ctx := context.Background()
			if err := m.EnableConstrained(ctx, "elapsed", 1); err != nil {
				t.Fatal(err)
			}
			c.Advance(2 * stdtime.Millisecond)
			if err := m.TimeAdvanceRequest(ctx, "elapsed", 1, 10); !errors.Is(err, core.ErrOutboxUnavailable) {
				t.Fatal(err)
			}
			delay := 3 * stdtime.Millisecond
			if reserved {
				delay += stdtime.Millisecond
			}
			requireElapsed(t, m, 2*stdtime.Millisecond, delay)
			if !m.Metrics("elapsed", 1).Advancing {
				t.Fatal("failed grant changed state")
			}
			c.Advance(stdtime.Millisecond)
			out.fail = false
			if err := m.tryGrantPending(ctx, "elapsed"); err != nil {
				t.Fatal(err)
			}
			c.Advance(7 * stdtime.Millisecond)
			requireElapsed(t, m, 9*stdtime.Millisecond, 2*delay+stdtime.Millisecond)
			if m.Metrics("elapsed", 1).Advancing {
				t.Fatal("successful grant remained advancing")
			}
		})
	}
}
