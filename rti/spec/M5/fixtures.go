package m5spec

import (
	"context"
	"errors"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ===========================================================================
// permissiveFOMRepo — minimal core.FOMRepository for tests. Accepts every
// Load call, returns a stub handle that resolves all lookups to handle 1.
// Mirrors rti/spec/M2/fixtures.go::fakeFOMRepo so M5 spec tests don't have
// to import the M2 spec package.
// ===========================================================================

type permissiveFOMRepo struct {
	mu       sync.Mutex
	loadedBy map[core.FederationName]core.FOMHandle
	stub     *permissiveFOMHandle
}

func newPermissiveFOMRepo() *permissiveFOMRepo {
	return &permissiveFOMRepo{
		loadedBy: map[core.FederationName]core.FOMHandle{},
		stub:     &permissiveFOMHandle{},
	}
}

func (r *permissiveFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stub, nil
}

func (r *permissiveFOMRepo) Get(_ context.Context, fed core.FederationName) (core.FOMHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.loadedBy[fed]; ok {
		return h, nil
	}
	// Permissive: also return the stub for unknown federations so M5 tests
	// that don't go through the federation manager's Create path still work.
	return r.stub, nil
}

type permissiveFOMHandle struct{}

func (*permissiveFOMHandle) IsValid() bool { return true }
func (*permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (*permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (*permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

// ===========================================================================
// permissiveEventLog — multi-federation core.EventLog that records every
// Append and accepts any federation name. Mirrors M2/M3 fixture.
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

// ===========================================================================
// recordingOutbox — captures every Send so tests can assert on TSO vs RO
// (presence/absence of timestamp on the outbound event).
// ===========================================================================

type recordedSend struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

type recordingOutbox struct {
	mu   sync.Mutex
	sent []recordedSend
}

func newRecordingOutbox() *recordingOutbox { return &recordingOutbox{} }

func (o *recordingOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, recordedSend{fed, h, evt})
	return nil
}

func (o *recordingOutbox) Sent() []recordedSend {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]recordedSend, len(o.sent))
	copy(out, o.sent)
	return out
}

// ===========================================================================
// Helpers
// ===========================================================================

// minimalFOMXML returns a near-empty FOM XML accepted by the stub
// permissiveFOMRepo. Matches the shape Agent B's parser accepts in
// tests/conformance/foms/good/minimal.xml without re-reading from disk.
func minimalFOMXML() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<objectModel>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
    </objectClass>
  </objects>
</objectModel>`)
}

// testClock returns a core.Clock for spec tests. Uses core.NewRealClock
// so we don't sneak in a forbidden direct time.Now call.
func testClock() core.Clock { return core.NewRealClock() }
