// Package m37spec — M37 semantic-fix acceptance tests.
//
// Covers the four pinned no-proto-change fixes:
//   - EB-1: DeleteObjectInstance REMOVE fan-out probes the full
//     subscription-relevant attribute set (om_delete_object_tso).
//   - EB-2: §8.14 delivery order — buffered TSO releases BEFORE the
//     grant that advances past their timestamp.
//   - EB-3: outgoing-TSO timestamp validation for regulating senders
//     (ts >= currentTime + lookahead, §8.1.2).
//   - EB-4: late-join retroactive §6.9 Discover on subscribe-after-
//     register.
package m37spec

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// recordingOutbox captures every Send in arrival order. Goroutine-safe.
type recordingOutbox struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	fed core.FederationName
	h   core.FederateHandle
	evt core.OutboundEvent
}

func (o *recordingOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, recordedEvent{fed: fed, h: h, evt: evt})
	return nil
}

func (o *recordingOutbox) snapshot() []recordedEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]recordedEvent, len(o.events))
	copy(out, o.events)
	return out
}

// innerFederateEvent extracts the wrapped proto from an object-package
// outbound event, or nil when the event is not one (e.g. a time grant).
func innerFederateEvent(evt core.OutboundEvent) *rtiv1.FederateEvent {
	if fe, ok := evt.(interface{ Inner() *rtiv1.FederateEvent }); ok {
		return fe.Inner()
	}
	return nil
}

// Stub FOM that lets every lookup succeed at handle 1 — same pattern as
// rti/internal/object/registry_test.go.
type stubFOMHandle struct{}

func (*stubFOMHandle) IsValid() bool                                           { return true }
func (*stubFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) { return 1, true }
func (*stubFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

type stubFOMRepo struct{}

func (*stubFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return &stubFOMHandle{}, nil
}
func (*stubFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return &stubFOMHandle{}, nil
}

// newRegistry builds an object.Registry over a real declaration.Manager
// and a recording outbox. opts mutators may customize object.Options
// (e.g. wire a TSO validator) before construction.
func newRegistry(t *testing.T, mutate ...func(*object.Options)) (*object.Registry, *declaration.Manager, *recordingOutbox) {
	t.Helper()
	declMgr := declaration.New()
	out := &recordingOutbox{}
	opts := object.Options{
		Declarations: declMgr,
		Outbox:       out,
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(stdtime.Unix(0, 0)),
	}
	for _, m := range mutate {
		m(&opts)
	}
	reg, err := object.New(opts)
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	return reg, declMgr, out
}
