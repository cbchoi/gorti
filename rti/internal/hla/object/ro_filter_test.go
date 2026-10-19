package object

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func roFilter(t *testing.T, r *Registry) core.QueuedMessageFilter {
	t.Helper()
	filter, ok := any(r).(core.QueuedMessageFilter)
	if !ok {
		t.Fatal("object registry has no queued-message subscription filter")
	}
	return filter
}

func TestROQueuedInteractionRechecksCurrentSubscription(t *testing.T) {
	ctx := context.Background()
	r, tm, out, _, obj := roFixture(t, 2)
	filter := roFilter(t, r)
	tm.SetQueuedMessageFilter(filter)
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	if err := roSend(r, obj, testInteraction, nil); err != nil {
		t.Fatal(err)
	}
	decl := r.opts.Declarations.(*declaration.Manager)
	if err := decl.UnsubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 0 {
		t.Fatalf("unsubscribed queued interaction delivered: %v", out.records)
	}
}

func TestROQueuedReflectionTrimsWithoutMutatingRetrySource(t *testing.T) {
	ctx := context.Background()
	r, tm, _, gate, obj := roFixture(t, 2)
	filter := roFilter(t, r)
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, nil); err != nil {
		t.Fatal(err)
	}
	original := gate.deliveries[0].Event.(*CallbackEvent)
	decl := r.opts.Declarations.(*declaration.Manager)
	if err := decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	trimmed := filter.FilterQueuedMessage(ctx, "fed", 2, original).(*CallbackEvent)
	if trimmed == original || len(trimmed.Attributes) != 1 || trimmed.Attributes[1] == nil {
		t.Fatalf("trimmed callback=%+v", trimmed)
	}
	if len(original.Attributes) != 2 {
		t.Fatal("filter mutated the queued source")
	}
	if err := decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if filtered := filter.FilterQueuedMessage(ctx, "fed", 2, original); filtered != nil {
		t.Fatalf("unsubscribed callback=%+v", filtered)
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "fed", 1, obj, nil, nil); err != nil {
		t.Fatal(err)
	}
	retained := filter.FilterQueuedMessage(ctx, "fed", 2, original).(*CallbackEvent)
	if len(retained.Attributes) != 1 || retained.Attributes[2] == nil || retained.ObjectClass != 1 {
		t.Fatalf("deleted-object queued reflection=%+v", retained)
	}
	removal := gate.deliveries[0].Event.(*CallbackEvent)
	if removal.ObjectClass != 1 {
		t.Fatal("removal omitted retained object class")
	}
	if err := decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	if filtered := filter.FilterQueuedMessage(ctx, "fed", 2, removal); filtered != removal {
		t.Fatal("removal must survive unsubscribe after discovery")
	}
}

func TestROQueuedFilterInheritsInteractionsButRetainsKnownObjectClass(t *testing.T) {
	ctx := context.Background()
	r, tm, _, gate, obj := roFixture(t, 2)
	filter := roFilter(t, r)
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	decl := r.opts.Declarations.(*declaration.Manager)
	if err := roSend(r, obj, testInteraction, nil); err != nil {
		t.Fatal(err)
	}
	interaction := gate.deliveries[0].Event
	if err := decl.UnsubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeInteractionClass(ctx, "fed", 2, 6); err != nil {
		t.Fatal(err)
	}
	if filtered := filter.FilterQueuedMessage(ctx, "fed", 2, interaction); filtered == nil {
		t.Fatal("ancestor interaction subscription lost")
	}
	if err := roSend(r, obj, "reflect", nil); err != nil {
		t.Fatal(err)
	}
	reflect := gate.deliveries[0].Event
	if err := decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 9, []core.AttributeHandle{101}); err != nil {
		t.Fatal(err)
	}
	if filtered := filter.FilterQueuedMessage(ctx, "fed", 2, reflect); filtered != nil {
		t.Fatal("new ancestor subscription changed the previously conveyed object class")
	}
}
