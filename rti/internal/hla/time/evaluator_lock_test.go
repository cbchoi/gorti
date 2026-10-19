package time

import (
	"context"
	"errors"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

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
	outbox := &recordingOutbox{}
	eventLog := &recordingGrantLog{}
	mgr, err := New(Options{
		Clock:    core.NewManualClock(zeroTime()),
		Outbox:   outbox,
		EventLog: eventLog,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { managerExt.Delete(mgr) })

	ctx := context.Background()
	for _, h := range []core.FederateHandle{1, 2} {
		if err := mgr.EnableRegulation(ctx, "fed", h, 1); err != nil {
			t.Fatalf("EnableRegulation(%d): %v", h, err)
		}
		if err := mgr.EnableConstrained(ctx, "fed", h); err != nil {
			t.Fatalf("EnableConstrained(%d): %v", h, err)
		}
	}

	// The idle low-handle peer holds LBTS below the first constrained request. Its return
	// is the recording barrier; the second request then makes both grantable.
	request := func(h core.FederateHandle) {
		t.Helper()
		result := make(chan error, 1)
		go func() { result <- mgr.TimeAdvanceRequest(ctx, "fed", h, 5) }()
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("TAR(%d): %v", h, err)
			}
		case <-stdtime.After(stdtime.Second):
			t.Fatalf("TAR(%d) did not return", h)
		}
	}
	request(2)
	if got := eventLog.snapshot(); len(got) != 0 {
		t.Fatalf("first request must remain pending, WAL = %+v", got)
	}
	if got := outbox.snapshot(); len(got) != 0 {
		t.Fatalf("first request must remain pending, outbox = %+v", got)
	}
	request(1)

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
