package time

import (
	"context"
	"math"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type scheduledTimeManager interface {
	ReserveScheduledSave(core.FederationName, core.LogicalTime, []core.FederateHandle) (func() (bool, error), func(), error)
}

func scheduleTM(t *testing.T, tm *Manager, target core.LogicalTime, members ...core.FederateHandle) (func() (bool, error), func(), error) {
	t.Helper()
	coordinator, ok := any(tm).(scheduledTimeManager)
	if !ok {
		t.Fatal("time manager has no scheduled checkpoint coordinator")
	}
	return coordinator.ReserveScheduledSave("fed", target, members)
}

func newScheduledTM(t *testing.T) (*Manager, *recordingOutbox) {
	t.Helper()
	out := &recordingOutbox{}
	m, err := New(Options{Clock: core.NewManualClock(stdtime.Unix(0, 0)), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := m.EnableRegulation(context.Background(), "fed", h, 1); err != nil {
			t.Fatal(err)
		}
		if err := m.EnableConstrained(context.Background(), "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	return m, out
}

func assertScheduleReady(t *testing.T, ready func() (bool, error), want bool) {
	t.Helper()
	got, err := ready()
	if err != nil || got != want {
		t.Fatalf("readiness %v, %v; want %v", got, err, want)
	}
}

func TestScheduledCheckpointLogicalTimePendingAndTSO(t *testing.T) {
	m, _ := newScheduledTM(t)
	ctx := context.Background()
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	assertScheduleReady(t, ready, false)
	if err := m.BufferTSO(ctx, "fed", 2, 10, &TimeAdvanceGrant{Time: 10}); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	assertScheduleReady(t, ready, false)
	if err := m.TimeAdvanceRequest(ctx, "fed", 2, 10); err != nil {
		t.Fatal(err)
	}
	assertScheduleReady(t, ready, true)
	// Safe same-time callbacks are drained before readiness is returned.
	ext := extOf(m)
	ext.mu.Lock()
	ns := ext.getLocked("fed", 2)
	ns.tsoBuffer = append(ns.tsoBuffer, bufferedTSOEvent{timestamp: 10, event: &TimeAdvanceGrant{Time: 10}})
	ext.mu.Unlock()
	assertScheduleReady(t, ready, true)
}

func TestScheduledCheckpointCeilingAllAdvanceModesAndRelease(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m, _ := newScheduledTM(t)
			m.opts.LBTSStrategy = scheduledFixedHorizon(30)
			ready, release, err := scheduleTM(t, m, 10, 1, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.recordAdvance("fed", h, 20, mode); err != nil {
					t.Fatal(err)
				}
			}
			assertScheduleReady(t, ready, true)
			for _, st := range m.Snapshot("fed").Federates {
				if !st.HasPendingRequest || st.CurrentTime != 0 {
					t.Fatalf("crossing request consumed: %+v", st)
				}
			}
			release()
			release()
			if err := m.ResumeScheduledSave(context.Background(), "fed"); err != nil {
				t.Fatal(err)
			}
			for _, st := range m.Snapshot("fed").Federates {
				if st.HasPendingRequest || st.CurrentTime != 20 {
					t.Fatalf("request not resumed: %+v", st)
				}
			}
		})
	}
}

func TestScheduledCheckpointRejectsUnsupportedTimesAndZeroLookahead(t *testing.T) {
	for _, target := range []core.LogicalTime{-1, core.LogicalTime(math.NaN()), core.LogicalTime(math.Inf(1)), core.LogicalTime(math.Inf(-1))} {
		m, _ := newScheduledTM(t)
		_, release, err := scheduleTM(t, m, target, 1, 2)
		if release != nil {
			release()
		}
		if err == nil {
			t.Fatalf("accepted invalid checkpoint time %v", target)
		}
	}
	m, _ := newScheduledTM(t)
	if err := m.ModifyLookahead(context.Background(), "fed", 1, 0); err != nil {
		t.Fatal(err)
	}
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	assertScheduleReady(t, ready, false)
	for _, h := range []core.FederateHandle{1, 2} {
		if err := m.TimeAdvanceRequest(context.Background(), "fed", h, 20); err != nil {
			t.Fatal(err)
		}
	}
	assertScheduleReady(t, ready, true)
}

func TestScheduledCheckpointStateChangeAndStaleRelease(t *testing.T) {
	m, _ := newScheduledTM(t)
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DisableRegulation(context.Background(), "fed", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := ready(); err == nil {
		t.Fatal("mode change did not invalidate schedule")
	}
	m.OnFederationDestroyed("fed")
	if _, err := ready(); err == nil {
		t.Fatal("destroyed federation retained readiness")
	}
	if err := m.EnableRegulation(context.Background(), "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableConstrained(context.Background(), "fed", 1); err != nil {
		t.Fatal(err)
	}
	_, releaseNew, err := scheduleTM(t, m, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseNew()
	release()
	if err := m.TimeAdvanceRequest(context.Background(), "fed", 1, 6); err != nil {
		t.Fatal(err)
	}
	if !m.Snapshot("fed").Federates[0].HasPendingRequest {
		t.Fatal("stale release removed new-generation barrier")
	}
}

type scheduledFixedHorizon core.LogicalTime

func (scheduledFixedHorizon) Name() string                { return "scheduled-test" }
func (scheduledFixedHorizon) DeterminismPreserving() bool { return true }

func (h scheduledFixedHorizon) LBTS([]RegulatingFederate) core.LogicalTime {
	return core.LogicalTime(h)
}

func TestScheduledCheckpointRequiresStrictActualHorizon(t *testing.T) {
	m, _ := newScheduledTM(t)
	ready, release, err := scheduleTM(t, m, 10, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	m.opts.LBTSStrategy = scheduledFixedHorizon(10)
	for _, h := range []core.FederateHandle{1, 2} {
		if err := m.TimeAdvanceRequestAvailable(context.Background(), "fed", h, 20); err != nil {
			t.Fatal(err)
		}
	}
	assertScheduleReady(t, ready, false)
	m.opts.LBTSStrategy = scheduledFixedHorizon(20)
	assertScheduleReady(t, ready, true)
}

func TestScheduledCheckpointRejectsPastAndBeyondBarrierPending(t *testing.T) {
	ctx := context.Background()
	for _, pending := range []bool{false, true} {
		m, _ := newScheduledTM(t)
		if err := m.TimeAdvanceRequest(ctx, "fed", 1, 20); err != nil {
			t.Fatal(err)
		}
		if !pending {
			if err := m.TimeAdvanceRequest(ctx, "fed", 2, 20); err != nil {
				t.Fatal(err)
			}
		}
		_, release, err := scheduleTM(t, m, 10, 1, 2)
		if release != nil {
			release()
		}
		if pending && err != nil {
			t.Fatalf("existing crossing request rejected: %v", err)
		}
		if !pending && err == nil {
			t.Fatal("accepted save in already granted past")
		}
	}
}
