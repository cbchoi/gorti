// TASK-251 (M23 W1) — DeleteObjectInstance + RemoveObjectInstance callback.
//
// Per AC §3.1 + §3.2: owner federate calls Delete; subscribers receive
// the RemoveObjectInstance event via Outbox; non-owner Delete returns
// PermissionDenied.

package m23spec

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

// recordingOutbox captures every Send so we can assert RemoveObjectInstance
// was delivered to the subscriber. Goroutine-safe.
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

// Stub FOM that lets every lookup succeed at handle 1. Object spec
// tests use the same pattern (rti/internal/object/registry_test.go).
type stubFOMHandle struct{}

func (*stubFOMHandle) IsValid() bool                                                 { return true }
func (*stubFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool)       { return 1, true }
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

func newM23Registry(t *testing.T) (*object.Registry, *declaration.Manager, *recordingOutbox) {
	t.Helper()
	declMgr := declaration.New()
	out := &recordingOutbox{}
	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       out,
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(stdtime.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	return reg, declMgr, out
}

// TestSpec_M23_DeleteEmitsRemoveCallback — AC §3.1+§3.2.
func TestSpec_M23_DeleteEmitsRemoveCallback(t *testing.T) {
	reg, declMgr, out := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const subscriber = core.FederateHandle(2)
	const cls = core.ObjectClassHandle(7)
	attr := core.AttributeHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, subscriber, cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	obj, _, err := reg.Register(ctx, fed, owner, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Drop discover events emitted by Register; we want to isolate the
	// remove emitted by Delete.
	beforeDelete := len(out.snapshot())

	tag := []byte("done")
	if err := reg.Delete(ctx, fed, owner, obj, nil, tag); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	events := out.snapshot()
	if len(events) <= beforeDelete {
		t.Fatalf("no events emitted by Delete (had %d, still %d)", beforeDelete, len(events))
	}
	var found bool
	for _, ev := range events[beforeDelete:] {
		if ev.h != subscriber {
			continue
		}
		fe, ok := unwrapFederateEvent(ev.evt)
		if !ok {
			continue
		}
		if rm := fe.GetRemove(); rm != nil {
			if rm.GetObjectHandle() != uint64(obj) {
				t.Errorf("remove.object_handle = %d, want %d", rm.GetObjectHandle(), obj)
			}
			if string(rm.GetUserSuppliedTag()) != "done" {
				t.Errorf("remove.user_supplied_tag = %q, want \"done\"", string(rm.GetUserSuppliedTag()))
			}
			found = true
		}
	}
	if !found {
		t.Errorf("subscriber didn't receive RemoveObjectInstance after Delete")
	}
}

// TestSpec_M23_DeleteByNonOwnerRejected — owner-only enforcement.
func TestSpec_M23_DeleteByNonOwnerRejected(t *testing.T) {
	reg, declMgr, _ := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const intruder = core.FederateHandle(2)
	const cls = core.ObjectClassHandle(7)
	attr := core.AttributeHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = reg.Delete(ctx, fed, intruder, obj, nil, nil)
	if err != core.ErrObjectNotOwned {
		t.Errorf("Delete by non-owner err = %v, want ErrObjectNotOwned", err)
	}
}

// TestSpec_M23_DeleteUnknownObjectInvalid — unknown handle.
func TestSpec_M23_DeleteUnknownObjectInvalid(t *testing.T) {
	reg, _, _ := newM23Registry(t)
	ctx := context.Background()
	err := reg.Delete(ctx, "fed", 1, core.ObjectHandle(999), nil, nil)
	if err != core.ErrObjectHandleInvalid {
		t.Errorf("Delete unknown obj err = %v, want ErrObjectHandleInvalid", err)
	}
}

// unwrapFederateEvent extracts the inner *rtiv1.FederateEvent from a
// recorded outbound event. The object package's outboundEvent type is
// package-private, so we rely on the federateEventCarrier interface
// it satisfies (transport/grpc/stream.go::toFederateEvent uses the
// same path for cross-process delivery).
type federateEventCarrier interface {
	Inner() *rtiv1.FederateEvent
}

func unwrapFederateEvent(evt core.OutboundEvent) (*rtiv1.FederateEvent, bool) {
	c, ok := evt.(federateEventCarrier)
	if !ok {
		return nil, false
	}
	return c.Inner(), true
}
