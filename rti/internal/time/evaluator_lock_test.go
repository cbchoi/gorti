package time

import (
	"context"
	"errors"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

type tarBarrierClock struct {
	mu         sync.Mutex
	calls      int
	firstCall  chan struct{}
	secondCall chan struct{}
}

func (c *tarBarrierClock) Now() stdtime.Time {
	c.mu.Lock()
	c.calls++
	first := c.calls == 1
	second := c.calls == 2
	c.mu.Unlock()
	if first {
		close(c.firstCall)
	}
	if second {
		close(c.secondCall)
	}
	return zeroTime()
}

type recordingGrantLog struct {
	mu      sync.Mutex
	records []recordedSend
}

func (l *recordingGrantLog) Append(_ context.Context, fed core.FederationName, evt core.EventRecord) error {
	rec, ok := evt.(*timeAdvanceGrantedRecord)
	if !ok {
		return errors.New("recordingGrantLog: unexpected event type")
	}
	l.mu.Lock()
	l.records = append(l.records, recordedSend{fed: fed, h: rec.Federate, t: rec.Time})
	l.mu.Unlock()
	return nil
}

func (*recordingGrantLog) Sync(context.Context, core.FederationName) error { return nil }

func (*recordingGrantLog) OpenReader(context.Context, string) (core.EventLogReader, error) {
	return nil, nil
}

func (l *recordingGrantLog) snapshot() []recordedSend {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]recordedSend, len(l.records))
	copy(out, l.records)
	return out
}

func TestTryGrantPending_ConcurrentTARExactlyOnceAndSorted(t *testing.T) {
	clock := &tarBarrierClock{
		firstCall:  make(chan struct{}),
		secondCall: make(chan struct{}),
	}
	outbox := &recordingOutbox{}
	eventLog := &recordingGrantLog{}
	mgr, err := New(Options{
		Clock:    clock,
		Outbox:   outbox,
		EventLog: eventLog,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for _, h := range []core.FederateHandle{1, 2} {
		if err := mgr.EnableRegulation(ctx, "fed", h, 1); err != nil {
			t.Fatalf("EnableRegulation(%d): %v", h, err)
		}
	}

	// Hold the federation evaluator while both requests are recorded. The
	// per-call clock barriers make the high-handle-first arrival order and
	// concurrent overlap deterministic without sleeps.
	evaluationBarrier := extOf(mgr).evaluatorLock("fed")
	evaluationBarrier.Lock()
	results := make(chan error, 2)
	go func() { results <- mgr.TimeAdvanceRequest(ctx, "fed", 2, 5) }()
	<-clock.firstCall
	go func() { results <- mgr.TimeAdvanceRequest(ctx, "fed", 1, 5) }()
	<-clock.secondCall
	evaluationBarrier.Unlock()

	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent TAR: %v", err)
		}
	}

	assertGrantEffects := func(name string, got []recordedSend) {
		t.Helper()
		if len(got) != 2 {
			t.Fatalf("%s effects = %+v, want exactly one per federate", name, got)
		}
		for i, want := range []core.FederateHandle{1, 2} {
			if got[i].fed != "fed" || got[i].h != want || got[i].t != 5 {
				t.Errorf("%s effect[%d] = %+v, want fed=%q h=%d t=5", name, i, got[i], "fed", want)
			}
		}
	}

	assertGrantEffects("WAL", eventLog.snapshot())
	assertGrantEffects("outbox", outbox.snapshot())
}
