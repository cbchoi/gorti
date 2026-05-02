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

func (*fakeFOMHandle) IsValid() bool                                                       { return true }
func (*fakeFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool)             { return 1, true }
func (*fakeFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool)   { return 1, true }
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
