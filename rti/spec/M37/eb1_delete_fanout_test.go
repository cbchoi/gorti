// M37 EB-1 — DeleteObjectInstance REMOVE fan-out (IEEE 1516.1-2010 §6.16).
//
// The pre-M37 delete path probed the hardcoded attribute set {1} to
// resolve subscribers, so a federate subscribed only to higher-handle
// attributes (e.g. Position at handle 2/3) discovered the instance but
// never received the REMOVE. §6.16 requires removeObjectInstance at
// every federate that knows the instance — i.e. the same recipient set
// as the §6.9 discover fan-out.

package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestSpec_M37_EB1_DeleteReachesSubscriberOfNonFirstAttribute(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	sub := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)

	// Owner publishes attrs {1,2,3}; subscriber subscribes to {2,3} ONLY
	// (never attr 1 — the pre-fix hardcoded probe).
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1, 2, 3}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, sub, cls, []core.AttributeHandle{2, 3}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	obj, _, err := reg.Register(ctx, fed, owner, cls, "obj-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Sanity: the subscriber discovered the instance.
	discovered := false
	for _, rec := range out.snapshot() {
		if rec.h != sub {
			continue
		}
		if fe := innerFederateEvent(rec.evt); fe != nil && fe.GetDiscover() != nil {
			discovered = true
		}
	}
	if !discovered {
		t.Fatalf("precondition failed: subscriber never received Discover")
	}

	if err := reg.Delete(ctx, fed, owner, obj, nil, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// §6.16: the subscriber MUST receive RemoveObjectInstance.
	for _, rec := range out.snapshot() {
		if rec.h != sub {
			continue
		}
		if fe := innerFederateEvent(rec.evt); fe != nil {
			if rm := fe.GetRemove(); rm != nil {
				if rm.GetObjectHandle() != uint64(obj) {
					t.Fatalf("remove carries object %d, want %d", rm.GetObjectHandle(), obj)
				}
				return // PASS
			}
		}
	}
	t.Fatalf("subscriber of attrs {2,3} never received RemoveObjectInstance (§6.16)")
}

func TestSpec_M37_EB1_DeleteStillSkipsOwner(t *testing.T) {
	ctx := context.Background()
	reg, declMgr, out := newRegistry(t)
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	cls := core.ObjectClassHandle(1)

	// Owner both publishes AND subscribes — must still not receive its
	// own REMOVE.
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "obj-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.Delete(ctx, fed, owner, obj, nil, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, rec := range out.snapshot() {
		if rec.h != owner {
			continue
		}
		if fe := innerFederateEvent(rec.evt); fe != nil && fe.GetRemove() != nil {
			t.Fatalf("owner received its own RemoveObjectInstance")
		}
	}
}
