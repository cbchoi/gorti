package m5spec

import (
	"context"
	"sync"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func (o *recordingOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	staged := make([]recordedSend, len(deliveries))
	for i, delivery := range deliveries {
		staged[i] = recordedSend{fed, delivery.Recipient, delivery.Event}
	}
	o.mu.Lock()
	return &bestEffortReservation{out: o, sent: staged}, nil
}

type bestEffortReservation struct {
	once sync.Once
	out  *recordingOutbox
	sent []recordedSend
}

func (r *bestEffortReservation) Commit() error {
	r.once.Do(func() { r.out.sent = append(r.out.sent, r.sent...); r.sent = nil; r.out.mu.Unlock() })
	return nil
}

func (r *bestEffortReservation) Release() {
	r.once.Do(func() { r.sent = nil; r.out.mu.Unlock() })
}

func scopeThenReflection(t *testing.T, sent []recordedSend, subscriber core.FederateHandle, objectHandle core.ObjectHandle, attribute core.AttributeHandle) *rtiv1.FederateEvent {
	t.Helper()
	if len(sent) != 2 || sent[0].Federate != subscriber || sent[1].Federate != subscriber {
		t.Fatalf("update fanout=%+v, want scope-in and reflection to %d", sent, subscriber)
	}
	scopeWire, err := grpcadapter.EncodeOutboundEvent(sent[0].Event)
	if err != nil {
		t.Fatal(err)
	}
	scope := scopeWire.GetAttributesInScope()
	if scope == nil || scope.GetObjectHandle() != uint64(objectHandle) || len(scope.GetAttributeHandles()) != 1 || scope.GetAttributeHandles()[0] != uint64(attribute) {
		t.Fatalf("scope callback=%+v", scopeWire)
	}
	wire, err := grpcadapter.EncodeOutboundEvent(sent[1].Event)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// TestSpec_M5_BestEffort_RODelivery: in a federation with mode=BestEffort,
// updates to a Receive-order attribute use RO rather than TSO. The optional
// producer-supplied timestamp is retained independently of delivery order.
// This fixture has no constrained recipient; delivery is immediate.
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
		Clock:        core.NewManualClock(time.Unix(0, 0)),
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

	// A Receive-order declaration changes message order, not its supplied time.
	ts := core.LogicalTime(42.0)
	if err := reg.UpdateAttributes(ctx, fed, producer, obj,
		map[core.AttributeHandle][]byte{attrBE: {0xAB}}, &ts); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}

	updateSent := outbox.Sent()[preCount:]
	wire := scopeThenReflection(t, updateSent, subscriber, obj, attrBE)
	reflect := wire.GetReflect()
	if reflect == nil {
		t.Fatalf("expected ReflectAttributeValues body, got %+v", wire)
	}
	if reflect.LogicalTime == nil || *reflect.LogicalTime != float64(ts) {
		t.Errorf("RO delivery lost original timestamp: %v, want %v", reflect.LogicalTime, ts)
	}
	if reflect.Metadata == nil || reflect.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE ||
		reflect.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || reflect.Metadata.MessageRetractionHandle != nil {
		t.Errorf("RO delivery metadata = %+v", reflect.Metadata)
	}
}

// TestSpec_M5_BestEffort_VerboseModeHonorsFOMReceive: FOM Receive order
// also applies in Verbose mode. A supplied timestamp remains callback data,
// not evidence of TSO delivery.
//
// Implements: FR-OM-3.
func TestSpec_M5_BestEffort_VerboseModeHonorsFOMReceive(t *testing.T) {
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
	// Preferred order is independent of the federation profile.
	orders.DeclareAttributeReceive(fed, cls, attrBE)

	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         newPermissiveFOMRepo(),
		Clock:        core.NewManualClock(time.Unix(0, 0)),
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
	wire := scopeThenReflection(t, updateSent, subscriber, obj, attrBE)
	reflect := wire.GetReflect()
	if reflect == nil {
		t.Fatalf("expected ReflectAttributeValues body, got %+v", wire)
	}
	if reflect.LogicalTime == nil || *reflect.LogicalTime != float64(ts) {
		t.Errorf("Verbose RO delivery lost original timestamp: %v, want %v", reflect.LogicalTime, ts)
	}
	if reflect.Metadata == nil || reflect.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE ||
		reflect.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || reflect.Metadata.MessageRetractionHandle != nil {
		t.Errorf("Verbose mode must honor FOM Receive order: metadata = %+v", reflect.Metadata)
	}
}
