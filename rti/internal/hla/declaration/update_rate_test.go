package declaration_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestUpdateRateDefaultDemandAndPassiveEdges(t *testing.T) {
	ctx := context.Background()
	m := declaration.New()
	lifecycle, ok := any(m).(core.ObjectUpdateRateLifecycle)
	if !ok {
		t.Fatal("declaration does not implement producer update-rate lifecycle")
	}
	out := delivery.NewWithBatch(8, 1, 0)
	queue, cancel, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	m.SetAdvisoryOutbox(out)
	if err := lifecycle.SyncObjectUpdateRateOwners(ctx, "f", 100, 10, map[core.AttributeHandle]core.FederateHandle{1: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, 10, []core.AttributeHandle{1}, core.SubscriptionOptions{Active: false}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-queue:
		t.Fatal("passive demand produced turn-on")
	default:
	}
	if err := m.SubscribeObjectClassAttributes(ctx, "f", 2, 10, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-queue:
		e, ok := batch[0].(*core.UpdateRateAdvisory)
		if !ok || !e.Enabled || e.Object != 100 || len(e.Attributes) != 1 || e.Attributes[0] != 1 || e.UpdateRateDesignator != nil {
			t.Fatalf("wrong default-rate turn-on: %#v", batch)
		}
	default:
		t.Fatal("missing default-rate turn-on")
	}
	if err := m.SubscribeObjectClassAttributesWithOptions(ctx, "f", 2, 10, []core.AttributeHandle{1}, core.SubscriptionOptions{Active: false}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-queue:
		e, ok := batch[0].(*core.UpdateRateAdvisory)
		if !ok || e.Enabled {
			t.Fatalf("wrong turn-off: %#v", batch)
		}
	default:
		t.Fatal("missing turn-off")
	}
	if got := m.SubscribersFor(ctx, "f", 10, []core.AttributeHandle{1}); len(got) != 1 || got[0] != 2 {
		t.Fatal("passive subscription lost data routing")
	}
}

func TestLifecycleAdvisoryRetryAfterQueueDrain(t *testing.T) {
	ctx := context.Background()
	m := declaration.New()
	_ = m.PublishObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	_ = m.SubscribeObjectClassAttributes(ctx, "f", 2, 10, []core.AttributeHandle{1})
	out := delivery.NewWithBatch(1, 1, 0)
	queue, cancel, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	m.SetAdvisoryOutbox(out)
	if err := out.Send(ctx, "f", 1, &declaration.AdvisoryEvent{Sequence: 99}); err != nil {
		t.Fatal(err)
	}
	m.OnFederateResign("f", 2)
	<-queue
	// A harmless successful declaration retries the coalesced lifecycle edge.
	if err := m.PublishObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-queue:
		e, ok := batch[0].(*declaration.AdvisoryEvent)
		if !ok || e.Kind != declaration.AdvisoryStopRegistration {
			t.Fatalf("wrong retry: %#v", batch)
		}
	default:
		t.Fatal("failed lifecycle advisory was permanently lost")
	}
}

func TestInternalMOMProducerHasNoExternalAdvisoryQueue(t *testing.T) {
	ctx := context.Background()
	m := declaration.New()
	const internal = ^core.FederateHandle(0)
	if err := m.PublishObjectClassAttributes(ctx, "f", internal, 10, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := m.SyncObjectUpdateRateOwners(ctx, "f", 100, 10, map[core.AttributeHandle]core.FederateHandle{1: internal}); err != nil {
		t.Fatal(err)
	}
	m.SetAdvisoryOutbox(delivery.NewWithBatch(1, 1, 0))
	if err := m.SubscribeObjectClassAttributes(ctx, "f", 2, 10, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("internal MOM advisory required nonexistent client queue: %v", err)
	}
	if len(m.ExportState("f").PendingAdvisories) != 0 {
		t.Fatal("internal producer retained undeliverable advisory")
	}
}
