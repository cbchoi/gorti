package m7spec

import (
	"context"
	"errors"
	"sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// ===========================================================================
// fakeOutbox + permissiveEventLog — same pattern as rti/spec/M3/fixtures.go.
// Duplicated here so M7 spec tests stay independent of M3.
// ===========================================================================

type sentRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

type fakeOutbox struct {
	mu   sync.Mutex
	sent []sentRecord
}

func newFakeOutbox() *fakeOutbox { return &fakeOutbox{} }

func (o *fakeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{fed, h, evt})
	return nil
}

func (o *fakeOutbox) Sent() []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]sentRecord, len(o.sent))
	copy(out, o.sent)
	return out
}

func (o *fakeOutbox) SentTo(fed core.FederationName, h core.FederateHandle) []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []sentRecord
	for _, s := range o.sent {
		if s.Federation == fed && s.Federate == h {
			out = append(out, s)
		}
	}
	return out
}

type permissiveAppend struct {
	Federation core.FederationName
	Seq        uint64
	Event      core.EventRecord
}

type permissiveEventLog struct {
	mu       sync.Mutex
	nextSeq  uint64
	appended []permissiveAppend
}

func newPermissiveEventLog() *permissiveEventLog { return &permissiveEventLog{} }

func (l *permissiveEventLog) Append(_ context.Context, fed core.FederationName, evt core.EventRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextSeq++
	l.appended = append(l.appended, permissiveAppend{fed, l.nextSeq, evt})
	return nil
}

func (*permissiveEventLog) Sync(_ context.Context, _ core.FederationName) error { return nil }

func (*permissiveEventLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("permissiveEventLog: OpenReader not supported in fixtures")
}

// newTestTimeManager builds a Manager with FakeClock + fixtures.
func newTestTimeManager(t interface {
	Helper()
	Logf(string, ...any)
}) (*timepkg.Manager, *fakeOutbox, *permissiveEventLog) {
	t.Helper()
	outbox := newFakeOutbox()
	log := newPermissiveEventLog()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:        core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox:       outbox,
		EventLog:     log,
		StallTimeout: 60 * stdtime.Second,
	})
	if err != nil {
		t.Logf("time.New returned: %v (expected during pre-dispatch)", err)
	}
	return mgr, outbox, log
}
