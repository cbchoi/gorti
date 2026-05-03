package m5spec

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/federation"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// federateEventCarrier mirrors the interface used by the gRPC stream
// service to extract the underlying proto from a core.OutboundEvent.
// Spec tests use it to inspect timestamp presence on outbound events
// without leaking the object package's adapter type.
type federateEventCarrier interface {
	Inner() *rtiv1.FederateEvent
}

// TestSpec_M5_BestEffort_RODelivery: in a federation with mode=BestEffort,
// updates to a best-effort attribute are delivered RO (Receive Order)
// rather than TSO (Time Stamp Order). Specifically: the OutboundEvent
// surfaced to the subscriber's outbox carries no timestamp.
//
// The wire-level invariant per docs/agent-a-rti-core.md §5.7: when the
// federation is in best-effort mode AND the attribute is declared
// best-effort in the FOM, the publisher's update bypasses the TSO queue
// and reaches subscribers immediately. The Outbox.Send call carries an
// OutboundEvent whose timestamp accessor returns nil (matching
// core.Outbox semantics: nil = RO; non-nil = TSO).
//
// Implements: FR-OM-3; M5 RO/TSO contract.
func TestSpec_M5_BestEffort_RODelivery(t *testing.T) {
	const (
		fed        core.FederationName    = "be-fed"
		cls        core.ObjectClassHandle = 1
		attrBE     core.AttributeHandle   = 1
		producer   core.FederateHandle    = 1
		subscriber core.FederateHandle    = 2
	)
	ctx := context.Background()

	fedMgr, err := federation.New(federation.Options{
		Clock:    testClock(),
		FOMs:     newPermissiveFOMRepo(),
		EventLog: newPermissiveEventLog(),
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	if err := fedMgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name:       fed,
		FOMModules: []core.FOMModule{{Path: "test", XML: minimalFOMXML()}},
		Mode:       core.ModeBestEffort,
	}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}

	declMgr := declaration.New()
	outbox := newRecordingOutbox()
	orders := newOrderTable()
	// Mark the attribute as best-effort (Receive order).
	orders.DeclareAttributeReceive(fed, cls, attrBE)

	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         newPermissiveFOMRepo(),
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
		Federations:  fedMgr,
		Orders:       orders,
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}

	// Producer publishes; subscriber subscribes.
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, producer, cls, []core.AttributeHandle{attrBE}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, subscriber, cls, []core.AttributeHandle{attrBE}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, producer, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	preCount := len(outbox.Sent())

	// Push an update WITH a timestamp. Best-effort + Receive-order
	// declaration should cause the registry to drop the timestamp on
	// the outbound envelope.
	ts := core.LogicalTime(42.0)
	if err := reg.UpdateAttributes(ctx, fed, producer, obj,
		map[core.AttributeHandle][]byte{attrBE: {0xAB}}, &ts); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}

	updateSent := outbox.Sent()[preCount:]
	if len(updateSent) != 1 {
		t.Fatalf("expected 1 reflect to subscriber, got %d", len(updateSent))
	}
	if updateSent[0].Federate != subscriber {
		t.Fatalf("reflect went to federate %d, want %d", updateSent[0].Federate, subscriber)
	}
	carrier, ok := updateSent[0].Event.(federateEventCarrier)
	if !ok {
		t.Fatalf("OutboundEvent %T does not satisfy federateEventCarrier", updateSent[0].Event)
	}
	reflect := carrier.Inner().GetReflect()
	if reflect == nil {
		t.Fatalf("expected ReflectAttributeValues body, got %+v", carrier.Inner())
	}
	if reflect.LogicalTime != nil {
		t.Errorf("RO delivery: ReflectAttributeValues.LogicalTime = %v (want nil)", *reflect.LogicalTime)
	}
}

// TestSpec_M5_BestEffort_VerboseModeStillTSO: in a federation with
// mode=Verbose (the default), updates remain TSO regardless of the
// attribute's declared order. This catches a regression where TASK-077's
// best-effort path accidentally short-circuits TSO for ALL attributes.
//
// Implements: FR-OM-3.
func TestSpec_M5_BestEffort_VerboseModeStillTSO(t *testing.T) {
	const (
		fed        core.FederationName    = "verbose-fed"
		cls        core.ObjectClassHandle = 1
		attrBE     core.AttributeHandle   = 1
		producer   core.FederateHandle    = 1
		subscriber core.FederateHandle    = 2
	)
	ctx := context.Background()

	fedMgr, err := federation.New(federation.Options{
		Clock:    testClock(),
		FOMs:     newPermissiveFOMRepo(),
		EventLog: newPermissiveEventLog(),
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	// Verbose mode (the default after TASK-076 normalization).
	if err := fedMgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name:       fed,
		FOMModules: []core.FOMModule{{Path: "test", XML: minimalFOMXML()}},
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}

	declMgr := declaration.New()
	outbox := newRecordingOutbox()
	orders := newOrderTable()
	// Even with the attribute declared best-effort, Verbose mode must
	// keep TSO delivery — this is the regression check.
	orders.DeclareAttributeReceive(fed, cls, attrBE)

	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         newPermissiveFOMRepo(),
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
		Federations:  fedMgr,
		Orders:       orders,
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}

	if err := declMgr.PublishObjectClassAttributes(ctx, fed, producer, cls, []core.AttributeHandle{attrBE}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fed, subscriber, cls, []core.AttributeHandle{attrBE}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, producer, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	preCount := len(outbox.Sent())

	ts := core.LogicalTime(7.5)
	if err := reg.UpdateAttributes(ctx, fed, producer, obj,
		map[core.AttributeHandle][]byte{attrBE: {0xCD}}, &ts); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}

	updateSent := outbox.Sent()[preCount:]
	if len(updateSent) != 1 {
		t.Fatalf("expected 1 reflect to subscriber, got %d", len(updateSent))
	}
	carrier, ok := updateSent[0].Event.(federateEventCarrier)
	if !ok {
		t.Fatalf("OutboundEvent %T does not satisfy federateEventCarrier", updateSent[0].Event)
	}
	reflect := carrier.Inner().GetReflect()
	if reflect == nil {
		t.Fatalf("expected ReflectAttributeValues body, got %+v", carrier.Inner())
	}
	if reflect.LogicalTime == nil {
		t.Errorf("Verbose mode: ReflectAttributeValues.LogicalTime is nil (want %v)", ts)
	} else if *reflect.LogicalTime != float64(ts) {
		t.Errorf("Verbose mode: LogicalTime = %v, want %v", *reflect.LogicalTime, ts)
	}
}
