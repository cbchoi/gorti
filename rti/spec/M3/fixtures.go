package m3spec

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ===========================================================================
// fakeOutbox — records every Send for assertion. Goroutine-safe.
// Mirrors rti/spec/M2/fixtures.go's fakeOutbox; duplicated rather than
// shared to keep M3's spec package independent of M2's.
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

// SentTo returns recordings filtered to (fed, h). Useful when tests want
// to assert "this specific federate received N grants in this order."
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

// ===========================================================================
// permissiveEventLog — multi-federation core.EventLog that records every
// Append and accepts any federation name. Mirrors M2's fixture; M3 spec
// tests use it for time-management event recording.
// ===========================================================================

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
	l.appended = append(l.appended, permissiveAppend{Federation: fed, Seq: l.nextSeq, Event: evt})
	return nil
}

func (*permissiveEventLog) Sync(_ context.Context, _ core.FederationName) error { return nil }

func (*permissiveEventLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("permissiveEventLog: OpenReader not supported in fixtures")
}

func (l *permissiveEventLog) Appended() []permissiveAppend {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]permissiveAppend, len(l.appended))
	copy(out, l.appended)
	return out
}

// AppendedFor returns events filtered to a single federation.
func (l *permissiveEventLog) AppendedFor(fed core.FederationName) []permissiveAppend {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []permissiveAppend
	for _, a := range l.appended {
		if a.Federation == fed {
			out = append(out, a)
		}
	}
	return out
}

// ===========================================================================
// sortedHandles — small helper for tests that build "subscriber-like" lists.
// ===========================================================================

func sortedHandles(in []core.FederateHandle) []core.FederateHandle {
	out := append([]core.FederateHandle(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
