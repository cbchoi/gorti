package m2spec

import (
	"context"
	"errors"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ===========================================================================
// fakeFOMRepo — minimal core.FOMRepository implementation for tests.
// Tests configure it to either accept all FOMs or reject with a canned error.
// ===========================================================================

type fakeFOMRepo struct {
	mu       sync.Mutex
	loadErr  error
	loadedBy map[core.FederationName]core.FOMHandle
	stub     *fakeFOMHandle
}

func newFakeFOMRepo() *fakeFOMRepo {
	return &fakeFOMRepo{
		loadedBy: map[core.FederationName]core.FOMHandle{},
		stub:     &fakeFOMHandle{},
	}
}

func (r *fakeFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return r.stub, nil
}

func (r *fakeFOMRepo) Get(_ context.Context, fed core.FederationName) (core.FOMHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.loadedBy[fed]; ok {
		return h, nil
	}
	return nil, core.ErrFederationNotFound
}

// fakeFOMHandle answers all Lookup* with handle 1, true. Sufficient for
// M2 tests that don't exercise FOM resolution.
type fakeFOMHandle struct{}

func (*fakeFOMHandle) IsValid() bool                                           { return true }
func (*fakeFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) { return 1, true }
func (*fakeFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (*fakeFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*fakeFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

// ===========================================================================
// fakeOutbox — records every Send for assertion. Goroutine-safe.
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

// ===========================================================================
// fakeEventRecord — minimal core.EventRecord for tests that don't need the
// full Protobuf Event. Tracks an assigned seq.
// ===========================================================================

type fakeEventRecord struct {
	seq uint64
	tag string // for test identification
}

func (e *fakeEventRecord) Seq() uint64 { return e.seq }

// errCanned is a sentinel for "any error" assertions in spec tests.
var errCanned = errors.New("canned test error")

// ===========================================================================
// permissiveEventLog — multi-federation core.EventLog that records every
// Append and accepts any federation name. Real eventlog.Writer enforces
// single-federation, which is correct for production but inconvenient for
// fixture tests that exercise multiple federations through one Manager.
// ===========================================================================

type permissiveEventLog struct {
	mu       sync.Mutex
	nextSeq  uint64
	appended []permissiveAppend
}

type permissiveAppend struct {
	Federation core.FederationName
	Seq        uint64
	Event      core.EventRecord
}

func newPermissiveEventLog() *permissiveEventLog {
	return &permissiveEventLog{}
}

// Append assigns the next monotonic seq, records the call, and returns nil.
// It does NOT marshal or persist; spec tests that need on-disk format use
// the real eventlog.Writer directly.
func (l *permissiveEventLog) Append(_ context.Context, fed core.FederationName, evt core.EventRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextSeq++
	if e, ok := evt.(*fakeEventRecord); ok {
		e.seq = l.nextSeq
	}
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
