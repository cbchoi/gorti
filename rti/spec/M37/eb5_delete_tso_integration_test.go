// M37 EB-5 — om_delete_object_tso server-side integration repro.
//
// Replays the fixture scenario at the manager level with the
// production wiring shape (shared outbox; time.Manager as TSOGate +
// TSOValidator): regulating publisher registers, advances to 5,
// TSO-deletes at 10, advances to 15; constrained subscriber (subscribed
// to a non-handle-1 attribute) advances to 15 and must receive the
// buffered RemoveObjectInstance BEFORE its grant (§6.15 + §8.14).

package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/object"
	hlatime "github.com/cbchoi/gorti/rti/internal/time"
)

func TestSpec_M37_EB5_DeleteTSO_RemoveBeforeGrant_Integration(t *testing.T) {
	ctx := context.Background()
	fed := core.FederationName("om_delete_object_tso")
	pub := core.FederateHandle(1)
	sub := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)
	pos := core.AttributeHandle(2) // Position — deliberately NOT handle 1

	timeMgr, out := newTimeManager(t)
	reg, declMgr, _ := newRegistry(t, func(o *object.Options) {
		o.Outbox = out // share the time manager's outbox (production shape)
		o.TSOGate = timeMgr
		o.TSOValidator = timeMgr
	})
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)

	if err := timeMgr.EnableRegulation(ctx, fed, pub, 1.0); err != nil {
		t.Fatalf("enable regulation: %v", err)
	}
	if err := timeMgr.EnableConstrained(ctx, fed, sub); err != nil {
		t.Fatalf("enable constrained: %v", err)
	}
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, pub, cls, []core.AttributeHandle{pos}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, sub, cls, []core.AttributeHandle{pos}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	obj, _, err := reg.Register(ctx, fed, pub, cls, "car-tso")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := timeMgr.TimeAdvanceRequest(ctx, fed, pub, 5); err != nil {
		t.Fatalf("pub TAR(5): %v", err)
	}
	ts := core.LogicalTime(10)
	if err := reg.Delete(ctx, fed, pub, obj, &ts, nil); err != nil {
		t.Fatalf("TSO delete: %v", err)
	}
	if err := timeMgr.TimeAdvanceRequest(ctx, fed, pub, 15); err != nil {
		t.Fatalf("pub TAR(15): %v", err)
	}
	if err := timeMgr.TimeAdvanceRequest(ctx, fed, sub, 15); err != nil {
		t.Fatalf("sub TAR(15): %v", err)
	}

	// Subscriber stream: DISCOVER ... REMOVE(ts=10) ... GRANT(15), with
	// the REMOVE strictly before the final grant.
	removeIdx, grantIdx := -1, -1
	i := 0
	for _, rec := range out.snapshot() {
		if rec.h != sub {
			continue
		}
		if fe := innerFederateEvent(rec.evt); fe != nil {
			if rm := fe.GetRemove(); rm != nil {
				if rm.GetObjectHandle() != uint64(obj) {
					t.Fatalf("remove for object %d, want %d", rm.GetObjectHandle(), obj)
				}
				if rm.LogicalTime == nil || *rm.LogicalTime != 10 {
					t.Fatalf("remove logical_time = %v, want 10", rm.LogicalTime)
				}
				removeIdx = i
			}
		}
		if g, ok := rec.evt.(*hlatime.TimeAdvanceGrant); ok && float64(g.Time) == 15 {
			grantIdx = i
		}
		i++
	}
	if removeIdx == -1 {
		t.Fatalf("subscriber never received RemoveObjectInstance")
	}
	if grantIdx == -1 {
		t.Fatalf("subscriber never received the grant to 15")
	}
	if removeIdx > grantIdx {
		t.Fatalf("§8.14 violation: REMOVE (idx %d) after GRANT(15) (idx %d)", removeIdx, grantIdx)
	}
}

// Same scenario in the LIVE fixture's arrival order: the subscriber's
// TAR(15) lands BEFORE the publisher advances at all. §8.10: a TAR is
// granted to EXACTLY the requested time once every TSO message with
// timestamp <= t has been delivered — the RTI must HOLD the request
// until LBTS covers it, not burn it with an early grant at LBTS
// (early/partial grants are FQR's contract, §8.12). Pre-M37 the
// TAR-family "incremental grant at LBTS" fired a full grant at
// LBTS=1, so the subscriber was never granted to 15 and the buffered
// REMOVE(10) never released (om_delete_object_tso 15/16).
func TestSpec_M37_EB5_DeleteTSO_SubscriberTARFirst_Integration(t *testing.T) {
	ctx := context.Background()
	fed := core.FederationName("om_delete_object_tso")
	pub := core.FederateHandle(1)
	sub := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)
	pos := core.AttributeHandle(2)

	timeMgr, out := newTimeManager(t)
	reg, declMgr, _ := newRegistry(t, func(o *object.Options) {
		o.Outbox = out
		o.TSOGate = timeMgr
		o.TSOValidator = timeMgr
	})
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)

	if err := timeMgr.EnableRegulation(ctx, fed, pub, 1.0); err != nil {
		t.Fatalf("enable regulation: %v", err)
	}
	if err := timeMgr.EnableConstrained(ctx, fed, sub); err != nil {
		t.Fatalf("enable constrained: %v", err)
	}
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, pub, cls, []core.AttributeHandle{pos}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, sub, cls, []core.AttributeHandle{pos}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, pub, cls, "car-tso")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// LIVE ordering: subscriber TARs to 15 right after discovery,
	// while the publisher is still regulating at t=0 (LBTS = 1).
	if err := timeMgr.TimeAdvanceRequest(ctx, fed, sub, 15); err != nil {
		t.Fatalf("sub TAR(15): %v", err)
	}
	if err := timeMgr.TimeAdvanceRequest(ctx, fed, pub, 5); err != nil {
		t.Fatalf("pub TAR(5): %v", err)
	}
	ts := core.LogicalTime(10)
	if err := reg.Delete(ctx, fed, pub, obj, &ts, nil); err != nil {
		t.Fatalf("TSO delete: %v", err)
	}
	if err := timeMgr.TimeAdvanceRequest(ctx, fed, pub, 15); err != nil {
		t.Fatalf("pub TAR(15): %v", err)
	}

	removeIdx, grant15Idx := -1, -1
	i := 0
	for _, rec := range out.snapshot() {
		if rec.h != sub {
			continue
		}
		if fe := innerFederateEvent(rec.evt); fe != nil && fe.GetRemove() != nil {
			removeIdx = i
		}
		if g, ok := rec.evt.(*hlatime.TimeAdvanceGrant); ok {
			if float64(g.Time) < 15 {
				t.Fatalf("§8.10 violation: subscriber's TAR(15) granted early at t=%g (TAR must grant at exactly the requested time)", float64(g.Time))
			}
			grant15Idx = i
		}
		i++
	}
	if removeIdx == -1 {
		t.Fatalf("subscriber never received RemoveObjectInstance")
	}
	if grant15Idx == -1 {
		t.Fatalf("subscriber never granted to 15")
	}
	if removeIdx > grant15Idx {
		t.Fatalf("§8.14 violation: REMOVE (idx %d) after GRANT(15) (idx %d)", removeIdx, grant15Idx)
	}
}
