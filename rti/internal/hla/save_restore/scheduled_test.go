package savepoint

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type scheduleFixture struct {
	m        *Manager
	out      *fakeOutbox
	store    *memStore
	members  []core.FederateHandle
	ready    bool
	released int
	reserved int
	target   core.LogicalTime
}

func newScheduleFixture(t *testing.T) *scheduleFixture {
	t.Helper()
	f := &scheduleFixture{out: &fakeOutbox{}, store: newMemStore(), members: []core.FederateHandle{1, 2}}
	var err error
	f.m, err = New(Options{
		Outbox: f.out, BundleStore: f.store,
		Members: func(core.FederationName) []core.FederateHandle {
			return append([]core.FederateHandle(nil), f.members...)
		},
		ScheduledTime: func(_ core.FederationName, target core.LogicalTime, _ []core.FederateHandle) (func() (bool, error), func(), error) {
			f.reserved++
			f.target = target
			return func() (bool, error) { return f.ready, nil }, func() { f.released++ }, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func progressSchedule(t *testing.T, m *Manager) error {
	t.Helper()
	p, ok := any(m).(interface {
		ProgressScheduledSave(context.Context, core.FederationName) error
	})
	if !ok {
		t.Fatal("scheduled logical-time progression is missing")
	}
	return p.ProgressScheduledSave(context.Background(), "fed")
}

func TestScheduledSaveDefersCallbacksAndCheckpointAdmission(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	target := core.LogicalTime(10)
	if err := f.m.RequestFederationSave(ctx, "fed", "scheduled", &target); err != nil {
		t.Fatal(err)
	}
	target = 99
	if f.reserved != 1 || f.target != 10 {
		t.Fatalf("reservation = %d, target %v", f.reserved, f.target)
	}
	if len(f.out.Sent()) != 0 {
		t.Fatal("callbacks before logical-time readiness")
	}
	if err := progressSchedule(t, f.m); err != nil {
		t.Fatal(err)
	}
	if len(f.out.Sent()) != 0 {
		t.Fatal("progress initiated an unready save")
	}
	release, err := f.m.AcquireService("fed")
	if err != nil {
		t.Fatalf("waiting save blocks time progression: %v", err)
	}
	release()
	_, statuses, err := f.m.QueryFederationSaveStatus("fed")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st.Status != NoSaveInProgress {
			t.Fatalf("premature participant state: %+v", st)
		}
	}
	if err := f.m.FederateSaveBegun(ctx, "fed", 1); !errors.Is(err, ErrSaveNotInitiated) {
		t.Fatalf("early begun: %v", err)
	}
	if err := f.m.FederateSaveComplete(ctx, "fed", 1); !errors.Is(err, core.ErrFederateNotInSave) {
		t.Fatalf("early complete: %v", err)
	}
	f.ready = true
	if err := progressSchedule(t, f.m); err != nil {
		t.Fatal(err)
	}
	if err := progressSchedule(t, f.m); err != nil {
		t.Fatal(err)
	}
	events := f.out.Sent()
	if len(events) != len(f.members) {
		t.Fatalf("initiation callbacks: %d, want %d", len(events), len(f.members))
	}
	recipients := make(map[core.FederateHandle]int, len(f.members))
	for _, h := range f.members {
		recipients[h] = 0
	}
	for _, sent := range events {
		count, joined := recipients[sent.Federate]
		if sent.Federation != "fed" || !joined || count != 0 {
			t.Fatalf("unexpected or duplicate initiation recipient: %+v", sent)
		}
		evt, ok := sent.Event.(*InitiateSaveEvent)
		if !ok || evt.Label() != "scheduled" || evt.SaveTime() == nil || *evt.SaveTime() != 10 {
			t.Fatalf("initiation event: %#v", sent.Event)
		}
		recipients[sent.Federate]++
	}
	for h, count := range recipients {
		if count != 1 {
			t.Fatalf("participant %d initiation count: %d, want 1", h, count)
		}
	}
	if release, err := f.m.AcquireService("fed"); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		if release != nil {
			release()
		}
		t.Fatalf("active save admission: %v", err)
	}
	for _, h := range f.members {
		if err := f.m.FederateSaveBegun(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
		if err := f.m.FederateSaveComplete(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	if f.m.QuerySaveState("fed", "scheduled") != StateSaved || !f.store.Exists("fed", "scheduled") || f.released != 1 {
		t.Fatalf("completion did not persist/release barrier: %d", f.released)
	}
}

func TestScheduledSaveAbortAndRepeat(t *testing.T) {
	f := newScheduleFixture(t)
	ctx := context.Background()
	for _, label := range []string{"first", "second"} {
		target := core.LogicalTime(10)
		if err := f.m.RequestFederationSave(ctx, "fed", label, &target); err != nil {
			t.Fatal(err)
		}
		if err := f.m.AbortSave(ctx, "fed"); err != nil {
			t.Fatal(err)
		}
		if err := progressSchedule(t, f.m); err != nil {
			t.Fatal(err)
		}
		if f.m.QuerySaveState("fed", label) != StateNotSaved {
			t.Fatal("aborted label not retained")
		}
	}
	if f.released != 2 {
		t.Fatalf("released %d barriers", f.released)
	}
	for _, sent := range f.out.Sent() {
		if _, ok := sent.Event.(*InitiateSaveEvent); ok {
			t.Fatal("aborted schedule initiated")
		}
	}
}

func TestScheduledSaveParticipantChangeCancels(t *testing.T) {
	for _, members := range [][]core.FederateHandle{{1}, {1, 2, 3}, {1, 3}} {
		t.Run("changed", func(t *testing.T) {
			f := newScheduleFixture(t)
			target := core.LogicalTime(10)
			if err := f.m.RequestFederationSave(context.Background(), "fed", "scheduled", &target); err != nil {
				t.Fatal(err)
			}
			f.members, f.ready = members, true
			if err := progressSchedule(t, f.m); err == nil {
				t.Fatal("roster change must surface failure")
			}
			if f.m.QuerySaveState("fed", "scheduled") != StateNotSaved || f.released != 1 {
				t.Fatal("roster failure did not release barrier")
			}
			for _, sent := range f.out.Sent() {
				if _, ok := sent.Event.(*InitiateSaveEvent); ok {
					t.Fatal("changed roster initiated")
				}
			}
		})
	}
}

func TestScheduledSaveInvalidTimesDoNotReserve(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
		f := newScheduleFixture(t)
		target := core.LogicalTime(value)
		if err := f.m.RequestFederationSave(context.Background(), "fed", "bad", &target); err == nil {
			t.Fatalf("accepted invalid target %v", value)
		}
		if f.reserved != 0 || len(f.out.Sent()) != 0 {
			t.Fatal("invalid target changed coordination state")
		}
	}
}

func TestScheduledSaveFailureReleasesTimeBarrier(t *testing.T) {
	for _, failure := range []string{"storage", "participant", "delivery"} {
		t.Run(failure, func(t *testing.T) {
			f := newScheduleFixture(t)
			ctx := context.Background()
			target := core.LogicalTime(10)
			if err := f.m.RequestFederationSave(ctx, "fed", "failed", &target); err != nil {
				t.Fatal(err)
			}
			f.ready = true
			if failure == "delivery" {
				f.m.opts.Outbox = &scheduledFailOutbox{fallback: f.out}
			}
			err := progressSchedule(t, f.m)
			if failure == "delivery" {
				if !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("initiation delivery failure: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				f.store.failOnWrite = failure == "storage"
				for _, h := range f.members {
					if err := f.m.FederateSaveBegun(ctx, "fed", h); err != nil {
						t.Fatal(err)
					}
					if failure == "participant" {
						err = f.m.FederateSaveNotComplete(ctx, "fed", h)
					} else {
						err = f.m.FederateSaveComplete(ctx, "fed", h)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if f.released != 1 || f.m.QuerySaveState("fed", "failed") != StateNotSaved || f.store.Exists("fed", "failed") {
				t.Fatal("failed save did not release ceiling without persisting")
			}
			if err := progressSchedule(t, f.m); err != nil {
				t.Fatal(err)
			}
			if f.released != 1 {
				t.Fatal("stale progression released barrier twice")
			}
		})
	}
}

type scheduledFailOutbox struct{ fallback *fakeOutbox }

func (o *scheduledFailOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	if _, ok := evt.(*InitiateSaveEvent); ok {
		return io.ErrClosedPipe
	}
	return o.fallback.Send(ctx, fed, h, evt)
}

func TestScheduledSaveGenerationChangeCancels(t *testing.T) {
	for _, initiated := range []bool{false, true} {
		t.Run(map[bool]string{false: "waiting", true: "initiated"}[initiated], func(t *testing.T) {
			f := newScheduleFixture(t)
			generation := uint64(1)
			f.m.opts.Generation = func(core.FederationName) (uint64, bool) { return generation, true }
			target := core.LogicalTime(10)
			ctx := context.Background()
			if err := f.m.RequestFederationSave(ctx, "fed", "stale", &target); err != nil {
				t.Fatal(err)
			}
			f.ready = true
			if initiated {
				if err := progressSchedule(t, f.m); err != nil {
					t.Fatal(err)
				}
			}
			generation = 2
			before := len(f.out.Sent())
			var err error
			if initiated {
				if err := f.m.FederateSaveBegun(ctx, "fed", 1); err != nil {
					t.Fatal(err)
				}
				err = f.m.FederateSaveComplete(ctx, "fed", 1)
			} else {
				err = progressSchedule(t, f.m)
			}
			if !errors.Is(err, ErrSaveBundleIncompatible) {
				t.Fatalf("generation change: %v", err)
			}
			if f.released != 1 || f.m.QuerySaveState("fed", "stale") != StateNotSaved {
				t.Fatal("stale save retained barrier")
			}
			if len(f.out.Sent()) != before {
				t.Fatal("stale generation received old save callbacks")
			}
		})
	}
}

func TestScheduledSaveConcurrentProgressAndAbort(t *testing.T) {
	for round := 0; round < 30; round++ {
		f := newScheduleFixture(t)
		f.ready = true
		target := core.LogicalTime(10)
		if err := f.m.RequestFederationSave(context.Background(), "fed", "concurrent", &target); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errors := make(chan error, 5)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); errors <- f.m.ProgressScheduledSave(context.Background(), "fed") }()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := f.m.AcquireCheckpoint("fed")
			defer release()
			errors <- f.m.AbortSave(context.Background(), "fed")
		}()
		wg.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		if f.released != 1 || f.m.HasPendingScheduledSave("fed") {
			t.Fatal("concurrent transition leaked reservation")
		}
		initiations := 0
		for _, send := range f.out.Sent() {
			if _, ok := send.Event.(*InitiateSaveEvent); ok {
				initiations++
			}
		}
		if initiations != 0 && initiations != len(f.members) {
			t.Fatalf("partial/repeated initiation: %d", initiations)
		}
	}
}

func TestScheduledSaveCancelledRequestAndDestroyRelease(t *testing.T) {
	f := newScheduleFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := core.LogicalTime(10)
	if err := f.m.RequestFederationSave(ctx, "fed", "cancelled", &target); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if f.reserved != 0 {
		t.Fatal("cancelled request reserved time")
	}
	if err := f.m.RequestFederationSave(context.Background(), "fed", "destroyed", &target); err != nil {
		t.Fatal(err)
	}
	f.m.OnFederationDestroyed("fed")
	f.ready = true
	if err := progressSchedule(t, f.m); err != nil {
		t.Fatal(err)
	}
	if len(f.out.Sent()) != 0 || f.released != 1 || f.m.HasPendingScheduledSave("fed") {
		t.Fatal("destroy retained scheduled work")
	}
}
