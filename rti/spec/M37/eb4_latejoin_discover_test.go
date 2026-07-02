// M37 EB-4 — late-join retroactive Discover (IEEE 1516.1-2010 §6.9).
//
// Pre-M37, a federate that subscribed AFTER an instance was registered
// never received discoverObjectInstance — the om_request fixtures only
// pass under subscribe-first launch order (pinned by CD/CC). §6.9:
// discovery fires when an instance of a subscribed class becomes
// relevant, regardless of subscribe/register order. The declaration
// manager's post-subscribe hook (SetOnSubscribeObjectClass, from M36
// DD) drives a retroactive Discover for existing matching instances,
// idempotent per (subscriber, object) and DDM-aware via the same
// subscribersForDiscover recipient resolution the register-time
// fan-out uses.

package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// discoversFor collects the DiscoverObjectInstance events delivered to h.
func discoversFor(out *recordingOutbox, h core.FederateHandle) []*rtiv1.DiscoverObjectInstance {
	var ds []*rtiv1.DiscoverObjectInstance
	for _, rec := range out.snapshot() {
		if rec.h != h {
			continue
		}
		if fe := innerFederateEvent(rec.evt); fe != nil {
			if d := fe.GetDiscover(); d != nil {
				ds = append(ds, d)
			}
		}
	}
	return ds
}

func TestSpec_M37_EB4_SubscribeAfterRegisterYieldsRetroactiveDiscover(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	late := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	obj, name, err := reg.Register(ctx, fed, owner, cls, "obj-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if n := len(discoversFor(out, late)); n != 0 {
		t.Fatalf("federate discovered %d instances before subscribing", n)
	}

	// Subscribe AFTER register — §6.9 retroactive discover must fire.
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, late, cls, []core.AttributeHandle{2}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ds := discoversFor(out, late)
	if len(ds) != 1 {
		t.Fatalf("late subscriber received %d Discover events, want exactly 1", len(ds))
	}
	if ds[0].GetObjectHandle() != uint64(obj) || ds[0].GetObjectClassHandle() != uint64(cls) || ds[0].GetObjectName() != name {
		t.Fatalf("retroactive Discover carries (%d, %d, %q), want (%d, %d, %q)",
			ds[0].GetObjectHandle(), ds[0].GetObjectClassHandle(), ds[0].GetObjectName(), obj, cls, name)
	}
}

func TestSpec_M37_EB4_RetroactiveDiscoverIsIdempotent(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	late := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, _, err := reg.Register(ctx, fed, owner, cls, "obj-1"); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Repeated / widened subscriptions must not repeat the Discover.
	for _, attrs := range [][]core.AttributeHandle{{2}, {2}, {1, 2}} {
		if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, late, cls, attrs); err != nil {
			t.Fatalf("subscribe %v: %v", attrs, err)
		}
	}
	if n := len(discoversFor(out, late)); n != 1 {
		t.Fatalf("late subscriber received %d Discover events across repeated subscribes, want exactly 1", n)
	}
}

func TestSpec_M37_EB4_SubscribeFirstFederateNotRediscovered(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	early := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Subscribe-first: register-time fan-out delivers the Discover.
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, early, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, _, err := reg.Register(ctx, fed, owner, cls, "obj-1"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if n := len(discoversFor(out, early)); n != 1 {
		t.Fatalf("subscribe-first federate received %d Discover events after register, want 1", n)
	}
	// Re-subscribing must not re-discover.
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, early, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	if n := len(discoversFor(out, early)); n != 1 {
		t.Fatalf("subscribe-first federate received %d Discover events after re-subscribe, want 1", n)
	}
}

func TestSpec_M37_EB4_OwnerNeverRetroactivelyDiscoversOwnInstance(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, _, err := reg.Register(ctx, fed, owner, cls, "obj-1"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if n := len(discoversFor(out, owner)); n != 0 {
		t.Fatalf("owner received %d Discover events for its own instance, want 0", n)
	}
}

func TestSpec_M37_EB4_DeletedInstanceNotRetroactivelyDiscovered(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	late := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "obj-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Delete(ctx, fed, owner, obj, nil, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, late, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if n := len(discoversFor(out, late)); n != 0 {
		t.Fatalf("late subscriber received %d Discover events for a deleted instance, want 0", n)
	}
}

// MOM-owned instances (internal producer ^0) are excluded: the MOM
// manager sends its own retroactive Discover+Reflect pair for its
// classes (M36 DD-2); the generic path skipping them prevents double
// discovers for late MOM subscribers.
func TestSpec_M37_EB4_InternalProducerInstancesExcluded(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	fed := core.FederationName("f")
	internal := ^core.FederateHandle(0) // mirrors mom.momProducer
	late := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, internal, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, _, err := reg.Register(ctx, fed, internal, cls, "HLAfederation.f"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, late, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if n := len(discoversFor(out, late)); n != 0 {
		t.Fatalf("generic retroactive path sent %d Discover events for an internal-producer instance, want 0 (MOM manager owns that fan-out)", n)
	}
}
