package m5spec

import (
	"context"
	"errors"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/object"
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
// orderTable — TASK-077 helper. Implements object.AttributeOrderLookup
// for spec tests that need to declare per-attribute / per-interaction
// best-effort order. Lookups are scoped per (federation, class, attr)
// or (federation, class). Unknown → (TimeStamp, false), which the
// object registry treats as TimeStamp-order.
//
// Tests construct an orderTable, call DeclareAttributeReceive /
// DeclareInteractionReceive for the (cls, attr) pairs they want as
// best-effort, and pass the table as the AttributeOrderLookup in
// object.Options.Orders. Anything not declared defaults to TimeStamp
// (the FOM-default).
//
// This extension is back-compatible — existing tests that don't
// instantiate an orderTable see no behavior change.
// ===========================================================================

type attrOrderKey struct {
	fed  core.FederationName
	cls  core.ObjectClassHandle
	attr core.AttributeHandle
}

type interOrderKey struct {
	fed core.FederationName
	cls core.InteractionClassHandle
}

type orderTable struct {
	mu     sync.Mutex
	attrs  map[attrOrderKey]bool  // value true = OrderReceive (best-effort)
	inters map[interOrderKey]bool // value true = OrderReceive (best-effort)
}

func newOrderTable() *orderTable {
	return &orderTable{
		attrs:  map[attrOrderKey]bool{},
		inters: map[interOrderKey]bool{},
	}
}

// DeclareAttributeReceive marks (fed, cls, attr) as best-effort.
func (t *orderTable) DeclareAttributeReceive(fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.attrs[attrOrderKey{fed, cls, attr}] = true
}

// DeclareInteractionReceive marks (fed, cls) as best-effort.
func (t *orderTable) DeclareInteractionReceive(fed core.FederationName, cls core.InteractionClassHandle) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inters[interOrderKey{fed, cls}] = true
}

// AttributeOrder satisfies object.AttributeOrderLookup. Returns
// (OrderReceive, true) when the (fed, cls, attr) triple has been
// declared best-effort via DeclareAttributeReceive; otherwise
// (OrderTimeStamp, true). The "known"=true branch is taken
// unconditionally so the registry's mode-aware path always evaluates
// the explicit declaration rather than defaulting to TimeStamp on
// "unknown".
func (t *orderTable) AttributeOrder(fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle) (object.Order, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.attrs[attrOrderKey{fed, cls, attr}] {
		return object.OrderReceive, true
	}
	return object.OrderTimeStamp, true
}

// InteractionOrder satisfies object.AttributeOrderLookup.
func (t *orderTable) InteractionOrder(fed core.FederationName, cls core.InteractionClassHandle) (object.Order, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inters[interOrderKey{fed, cls}] {
		return object.OrderReceive, true
	}
	return object.OrderTimeStamp, true
}

// Compile-time check: orderTable satisfies object.AttributeOrderLookup.
var _ object.AttributeOrderLookup = (*orderTable)(nil)

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
// permissiveFOMRepo. Matches the shape accepted by the parser in
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
