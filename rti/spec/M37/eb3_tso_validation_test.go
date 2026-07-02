// M37 EB-3 — outgoing-TSO timestamp validation (IEEE 1516.1-2010
// §8.1.2 / §6.10 / §6.12 preconditions).
//
// A time-regulating federate promises never to send a TSO message with
// timestamp < currentTime + lookahead. Pre-M37 no server-side check
// existed: a misbehaving regulating sender could emit TSO into the
// past, violating peers' LBTS guarantees. The check rejects with
// core.ErrTimeInvalidLogicalTime at the send/update ingestion point.
//
// Exemptions: constrained-only and non-time-aware senders have no
// lookahead constraint. Boundary: ts == currentTime + lookahead is
// LEGAL (>=) — the tm fixtures send at exactly current+lookahead.

package m37spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/object"
)

func TestSpec_M37_EB3_ValidateOutgoingTSO_RegulatingSender(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTimeManager(t)
	fed := core.FederationName("f")
	reg := core.FederateHandle(1)

	if err := mgr.EnableRegulation(ctx, fed, reg, 1.0); err != nil {
		t.Fatalf("enable regulation: %v", err)
	}

	// currentTime 0, lookahead 1: ts 0.5 violates §8.1.2.
	if err := mgr.ValidateOutgoingTSO(fed, reg, 0.5); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("ts=0.5 < current(0)+lookahead(1): want ErrTimeInvalidLogicalTime, got %v", err)
	}
	// Boundary must PASS: ts == current + lookahead exactly.
	if err := mgr.ValidateOutgoingTSO(fed, reg, 1.0); err != nil {
		t.Fatalf("ts=1.0 == current(0)+lookahead(1) must be legal, got %v", err)
	}
	if err := mgr.ValidateOutgoingTSO(fed, reg, 7.5); err != nil {
		t.Fatalf("ts=7.5 well past the floor must be legal, got %v", err)
	}

	// Advance to 5 (sole pending, TAR grants immediately); the floor
	// moves to 5+1.
	if err := mgr.TimeAdvanceRequest(ctx, fed, reg, 5); err != nil {
		t.Fatalf("TAR: %v", err)
	}
	if err := mgr.ValidateOutgoingTSO(fed, reg, 5.9); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("ts=5.9 < current(5)+lookahead(1): want ErrTimeInvalidLogicalTime, got %v", err)
	}
	if err := mgr.ValidateOutgoingTSO(fed, reg, 6.0); err != nil {
		t.Fatalf("ts=6.0 == current(5)+lookahead(1) must be legal, got %v", err)
	}
}

func TestSpec_M37_EB3_ValidateOutgoingTSO_FloatBoundaryTolerance(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTimeManager(t)
	fed := core.FederationName("f")
	reg := core.FederateHandle(1)

	// lookahead 0.2, advance to 0.1: current+lookahead accumulates to
	// 0.30000000000000004 in float64. A sender computing 0.1+0.2 = same
	// noise sends 0.30000000000000004 and passes trivially, but a
	// sender at the DECIMAL boundary 0.3 must ALSO pass (mind float
	// equality).
	if err := mgr.EnableRegulation(ctx, fed, reg, 0.2); err != nil {
		t.Fatalf("enable regulation: %v", err)
	}
	if err := mgr.TimeAdvanceRequest(ctx, fed, reg, 0.1); err != nil {
		t.Fatalf("TAR: %v", err)
	}
	if err := mgr.ValidateOutgoingTSO(fed, reg, 0.3); err != nil {
		t.Fatalf("ts=0.3 at the decimal boundary of 0.1+0.2 must be legal, got %v", err)
	}
}

func TestSpec_M37_EB3_ValidateOutgoingTSO_NonRegulatingExempt(t *testing.T) {
	ctx := context.Background()
	mgr, _ := newTimeManager(t)
	fed := core.FederationName("f")
	constrained := core.FederateHandle(2)
	unaware := core.FederateHandle(3)

	if err := mgr.EnableConstrained(ctx, fed, constrained); err != nil {
		t.Fatalf("enable constrained: %v", err)
	}
	// Constrained-only: no lookahead constraint on sends.
	if err := mgr.ValidateOutgoingTSO(fed, constrained, 0.0); err != nil {
		t.Fatalf("constrained-only sender must be exempt, got %v", err)
	}
	// Never enabled anything: exempt.
	if err := mgr.ValidateOutgoingTSO(fed, unaware, 0.0); err != nil {
		t.Fatalf("non-time-aware sender must be exempt, got %v", err)
	}
}

// stubValidator lets the registry-side ingestion tests run without a
// full time.Manager.
type stubValidator struct {
	err   error
	calls int
}

func (v *stubValidator) ValidateOutgoingTSO(_ core.FederationName, _ core.FederateHandle, _ core.LogicalTime) error {
	v.calls++
	return v.err
}

func TestSpec_M37_EB3_UpdateAttributesRejectsInvalidTimestamp(t *testing.T) {
	ctx := context.Background()
	val := &stubValidator{err: core.ErrTimeInvalidLogicalTime}
	reg, declMgr, _ := newRegistry(t, func(o *object.Options) { o.TSOValidator = val })
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "obj-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	ts := core.LogicalTime(0.5)
	err = reg.UpdateAttributes(ctx, fed, owner, obj, map[core.AttributeHandle][]byte{1: {0x01}}, &ts)
	if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("TSO update with invalid timestamp: want ErrTimeInvalidLogicalTime, got %v", err)
	}
	if val.calls == 0 {
		t.Fatalf("validator never consulted")
	}

	// RO update (nil ts) is NOT subject to the check.
	if err := reg.UpdateAttributes(ctx, fed, owner, obj, map[core.AttributeHandle][]byte{1: {0x02}}, nil); err != nil {
		t.Fatalf("RO update must bypass TSO validation, got %v", err)
	}
}

func TestSpec_M37_EB3_SendInteractionRejectsInvalidTimestamp(t *testing.T) {
	ctx := context.Background()
	val := &stubValidator{err: core.ErrTimeInvalidLogicalTime}
	reg, declMgr, _ := newRegistry(t, func(o *object.Options) { o.TSOValidator = val })
	fed := core.FederationName("f")
	sender := core.FederateHandle(1)
	icls := core.InteractionClassHandle(1)

	if err := declMgr.PublishInteractionClass(ctx, fed, sender, icls); err != nil {
		t.Fatalf("publish interaction: %v", err)
	}
	ts := core.LogicalTime(0.5)
	err := reg.SendInteraction(ctx, fed, sender, icls, map[core.ParameterHandle][]byte{1: {0x01}}, &ts)
	if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("TSO interaction with invalid timestamp: want ErrTimeInvalidLogicalTime, got %v", err)
	}
	// RO interaction bypasses.
	if err := reg.SendInteraction(ctx, fed, sender, icls, map[core.ParameterHandle][]byte{1: {0x02}}, nil); err != nil {
		t.Fatalf("RO interaction must bypass TSO validation, got %v", err)
	}
}

func TestSpec_M37_EB3_DeleteRejectsInvalidTimestamp(t *testing.T) {
	ctx := context.Background()
	val := &stubValidator{err: core.ErrTimeInvalidLogicalTime}
	reg, declMgr, _ := newRegistry(t, func(o *object.Options) { o.TSOValidator = val })
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "obj-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := core.LogicalTime(0.5)
	if err := reg.Delete(ctx, fed, owner, obj, &ts, nil); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("TSO delete with invalid timestamp: want ErrTimeInvalidLogicalTime, got %v", err)
	}
	// The instance must survive the rejected delete.
	if err := reg.Delete(ctx, fed, owner, obj, nil, nil); err != nil {
		t.Fatalf("RO delete after rejected TSO delete: %v", err)
	}
}

func TestSpec_M37_EB3_ValidatorPassAllowsSend(t *testing.T) {
	ctx := context.Background()
	val := &stubValidator{err: nil}
	reg, declMgr, out := newRegistry(t, func(o *object.Options) { o.TSOValidator = val })
	fed := core.FederationName("f")
	owner := core.FederateHandle(1)
	sub := core.FederateHandle(2)
	cls := core.ObjectClassHandle(1)

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, sub, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "obj-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := core.LogicalTime(2.0)
	if err := reg.UpdateAttributes(ctx, fed, owner, obj, map[core.AttributeHandle][]byte{1: {0x01}}, &ts); err != nil {
		t.Fatalf("valid TSO update: %v", err)
	}
	if val.calls == 0 {
		t.Fatalf("validator never consulted")
	}
	reflected := false
	for _, rec := range out.snapshot() {
		if rec.h != sub {
			continue
		}
		if fe := innerFederateEvent(rec.evt); fe != nil && fe.GetReflect() != nil {
			reflected = true
		}
	}
	if !reflected {
		t.Fatalf("valid TSO update never reached the subscriber")
	}
}
