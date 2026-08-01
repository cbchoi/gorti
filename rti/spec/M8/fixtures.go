package m8spec

import (
	"context"
	"errors"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeOutbox + permissiveEventLog mirror the pattern from rti/spec/M3
// + M5 + M7. Duplicated so M8 spec tests stay independent.

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
