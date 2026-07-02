package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

const declFed = core.FederationName("m37_declaration_advisories")

func newDeclStack(t *testing.T) (*declaration.Manager, *fakeOutbox) {
	t.Helper()
	outbox := newFakeOutbox()
	mgr := declaration.New()
	mgr.SetAdvisoryOutbox(outbox)
	return mgr, outbox
}

// TestSpec_M37_StartStopRegistration_OnSubscriptionFlips: IEEE
// 1516.1-2010 §5.10/§5.11 — startRegistrationForObjectClass fires on
// each publisher when the class gains its FIRST subscriber;
// stopRegistrationForObjectClass when it loses its LAST. Intermediate
// subscribe/unsubscribe churn emits nothing.
func TestSpec_M37_StartStopRegistration_OnSubscriptionFlips(t *testing.T) {
	mgr, outbox := newDeclStack(t)
	ctx := context.Background()
	cls := core.ObjectClassHandle(5)
	attrs := []core.AttributeHandle{1, 2}

	// Publisher = federate 1.
	if err := mgr.PublishObjectClassAttributes(ctx, declFed, 1, cls, attrs); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if n := len(outbox.Sent()); n != 0 {
		t.Fatalf("publish alone emitted %d events, want 0", n)
	}

	// First subscriber (federate 2) → START to the publisher.
	if err := mgr.SubscribeObjectClassAttributes(ctx, declFed, 2, cls, attrs[:1]); err != nil {
		t.Fatalf("Subscribe(2): %v", err)
	}
	evts1 := outbox.SentTo(1)
	if len(evts1) != 1 {
		t.Fatalf("publisher events = %d, want 1 (start registration); got %+v", len(evts1), evts1)
	}
	start := evts1[0].GetStartRegistration()
	if start == nil || start.GetObjectClassHandle() != uint64(cls) {
		t.Errorf("event = %+v, want StartRegistrationForObjectClass{%d}", evts1[0], cls)
	}

	// Second subscriber (federate 3, different attr) → NO further event.
	if err := mgr.SubscribeObjectClassAttributes(ctx, declFed, 3, cls, attrs[1:]); err != nil {
		t.Fatalf("Subscribe(3): %v", err)
	}
	if n := len(outbox.SentTo(1)); n != 1 {
		t.Fatalf("publisher events after second subscriber = %d, want still 1", n)
	}

	// Federate 2 leaves → class still has subscriber 3 → NO event.
	if err := mgr.UnsubscribeObjectClassAttributes(ctx, declFed, 2, cls, attrs[:1]); err != nil {
		t.Fatalf("Unsubscribe(2): %v", err)
	}
	if n := len(outbox.SentTo(1)); n != 1 {
		t.Fatalf("publisher events after partial unsubscribe = %d, want still 1", n)
	}

	// Last subscriber leaves → STOP to the publisher.
	if err := mgr.UnsubscribeObjectClassAttributes(ctx, declFed, 3, cls, attrs[1:]); err != nil {
		t.Fatalf("Unsubscribe(3): %v", err)
	}
	evts1 = outbox.SentTo(1)
	if len(evts1) != 2 {
		t.Fatalf("publisher events = %d, want 2 (start, stop); got %+v", len(evts1), evts1)
	}
	stop := evts1[1].GetStopRegistration()
	if stop == nil || stop.GetObjectClassHandle() != uint64(cls) {
		t.Errorf("event = %+v, want StopRegistrationForObjectClass{%d}", evts1[1], cls)
	}
}

// TestSpec_M37_TurnInteractionsOnOff_OnSubscriptionFlips: IEEE
// 1516.1-2010 §5.12/§5.13 — turnInteractionsOn fires on each publisher
// when the interaction class gains its first subscriber;
// turnInteractionsOff when it loses its last.
func TestSpec_M37_TurnInteractionsOnOff_OnSubscriptionFlips(t *testing.T) {
	mgr, outbox := newDeclStack(t)
	ctx := context.Background()
	cls := core.InteractionClassHandle(9)

	if err := mgr.PublishInteractionClass(ctx, declFed, 1, cls); err != nil {
		t.Fatalf("PublishInteractionClass: %v", err)
	}

	if err := mgr.SubscribeInteractionClass(ctx, declFed, 2, cls); err != nil {
		t.Fatalf("Subscribe(2): %v", err)
	}
	evts1 := outbox.SentTo(1)
	if len(evts1) != 1 {
		t.Fatalf("publisher events = %d, want 1 (turn on); got %+v", len(evts1), evts1)
	}
	on := evts1[0].GetTurnInteractionsOn()
	if on == nil || on.GetInteractionClassHandle() != uint64(cls) {
		t.Errorf("event = %+v, want TurnInteractionsOn{%d}", evts1[0], cls)
	}

	// Second subscriber: no repeat.
	if err := mgr.SubscribeInteractionClass(ctx, declFed, 3, cls); err != nil {
		t.Fatalf("Subscribe(3): %v", err)
	}
	if n := len(outbox.SentTo(1)); n != 1 {
		t.Fatalf("publisher events after second subscriber = %d, want still 1", n)
	}

	if err := mgr.UnsubscribeInteractionClass(ctx, declFed, 2, cls); err != nil {
		t.Fatalf("Unsubscribe(2): %v", err)
	}
	if n := len(outbox.SentTo(1)); n != 1 {
		t.Fatalf("publisher events after partial unsubscribe = %d, want still 1", n)
	}

	if err := mgr.UnsubscribeInteractionClass(ctx, declFed, 3, cls); err != nil {
		t.Fatalf("Unsubscribe(3): %v", err)
	}
	evts1 = outbox.SentTo(1)
	if len(evts1) != 2 {
		t.Fatalf("publisher events = %d, want 2 (on, off); got %+v", len(evts1), evts1)
	}
	off := evts1[1].GetTurnInteractionsOff()
	if off == nil || off.GetInteractionClassHandle() != uint64(cls) {
		t.Errorf("event = %+v, want TurnInteractionsOff{%d}", evts1[1], cls)
	}
}

// TestSpec_M37_Advisories_NoOutbox_Pure: without SetAdvisoryOutbox the
// manager keeps its pre-M37 pure behavior (nothing to send to, no
// panic).
func TestSpec_M37_Advisories_NoOutbox_Pure(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()
	cls := core.ObjectClassHandle(5)

	if err := mgr.PublishObjectClassAttributes(ctx, declFed, 1, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := mgr.SubscribeObjectClassAttributes(ctx, declFed, 2, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := mgr.UnsubscribeObjectClassAttributes(ctx, declFed, 2, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
}
