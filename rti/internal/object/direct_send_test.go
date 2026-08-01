package object

// W4 — outbox reservation bypass when there is nothing to make atomic:
// EventLog nil, exactly one immediate delivery, no TSO-buffered work.
// These tests pin BOTH branches (fast Send vs Reserve) including the
// ErrFederateOverflow error surface, which must be bit-identical.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

// newNoWALRegistry builds a registry with NO EventLog wired, the W4
// fast-path precondition.
func newNoWALRegistry(t *testing.T, outbox core.Outbox) (*Registry, *declaration.Manager) {
	t.Helper()
	decl := declaration.New()
	reg, err := New(Options{
		Declarations: decl,
		Outbox:       outbox,
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg, decl
}

func overflowSendErr() error {
	// Shaped exactly like multiOutbox.Send / Reserve overflow errors.
	return fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, "fed", 2)
}

func TestUpdateAttributesDirectSendSkipsReservationWithoutEventLog(t *testing.T) {
	outbox := &atomicUpdateOutbox{}
	reg, decl := newNoWALRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	delivered, sent := 0, 0
	reg.opts.OnReflectDelivered = func(core.FederationName, core.FederateHandle) { delivered++ }
	reg.opts.OnUpdateSent = func(core.FederationName, core.FederateHandle) { sent++ }

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}
	if len(outbox.attempts) != 0 {
		t.Fatalf("Reserve attempts = %d, want 0 on the direct-send fast path", len(outbox.attempts))
	}
	if outbox.sendCalls != 1 || len(outbox.records) != 1 {
		t.Fatalf("Send calls = %d records = %d, want 1/1", outbox.sendCalls, len(outbox.records))
	}
	if got := outbox.records[0].Federate; got != 2 {
		t.Fatalf("recipient = %d, want 2", got)
	}
	if outbox.records[0].Event.(*outboundEvent).Inner().GetReflect() == nil {
		t.Fatalf("event = %T, want Reflect", outbox.records[0].Event)
	}
	if delivered != 1 || sent != 1 {
		t.Fatalf("callbacks = delivered %d sent %d, want 1/1", delivered, sent)
	}
}

func TestUpdateAttributesNoEventLogMultiRecipientStillReserves(t *testing.T) {
	outbox := &atomicUpdateOutbox{}
	reg, decl := newNoWALRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	for _, subscriber := range []core.FederateHandle{3, 2} {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", subscriber, 7, []core.AttributeHandle{2}); err != nil {
			t.Fatal(err)
		}
	}

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}
	if len(outbox.attempts) != 1 || len(outbox.attempts[0]) != 2 {
		t.Fatalf("Reserve attempts = %+v, want one reservation of 2 deliveries", outbox.attempts)
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0 (reservation commit delivers)", outbox.sendCalls)
	}
	if len(outbox.records) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(outbox.records))
	}
}

func TestUpdateAttributesEventLogSingleRecipientStillReserves(t *testing.T) {
	outbox := &atomicUpdateOutbox{}
	reg, decl, _ := newUpdateDeliveryRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}
	if len(outbox.attempts) != 1 || len(outbox.attempts[0]) != 1 {
		t.Fatalf("Reserve attempts = %+v, want one single-delivery reservation spanning the WAL append", outbox.attempts)
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0 (reservation commit delivers)", outbox.sendCalls)
	}
}

// updateOverflowViaDirectSend triggers ErrFederateOverflow through the W4
// fast path (EventLog nil, Send fails) and returns the surfaced error.
func updateOverflowViaDirectSend(t *testing.T, wire bool) (error, core.ObjectHandle) {
	t.Helper()
	outbox := &atomicUpdateOutbox{sendErr: overflowSendErr()}
	reg, decl := newNoWALRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	var err error
	if wire {
		err = reg.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{2: {0xAB}}, nil)
	} else {
		err = reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil)
	}
	if len(outbox.attempts) != 0 {
		t.Fatalf("Reserve attempts = %d, want 0 on the direct-send fast path", len(outbox.attempts))
	}
	if outbox.sendCalls != 1 {
		t.Fatalf("Send calls = %d, want 1", outbox.sendCalls)
	}
	return err, obj
}

// updateOverflowViaReserve triggers the same overflow through the
// reservation branch (EventLog wired, Reserve fails).
func updateOverflowViaReserve(t *testing.T, wire bool) (error, core.ObjectHandle) {
	t.Helper()
	outbox := &atomicUpdateOutbox{reserveErr: overflowSendErr()}
	reg, decl, _ := newUpdateDeliveryRegistry(t, outbox)
	obj := registerUpdateProducer(t, reg, decl)
	ctx := context.Background()
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	var err error
	if wire {
		err = reg.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{2: {0xAB}}, nil)
	} else {
		err = reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil)
	}
	if len(outbox.attempts) != 1 {
		t.Fatalf("Reserve attempts = %d, want 1", len(outbox.attempts))
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0 after reservation rejection", outbox.sendCalls)
	}
	return err, obj
}

func TestUpdateAttributesOverflowErrorSurfaceIdenticalAcrossBranches(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire bool
	}{
		{name: "typed", wire: false},
		{name: "wire", wire: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fastErr, fastObj := updateOverflowViaDirectSend(t, tc.wire)
			reserveErr, reserveObj := updateOverflowViaReserve(t, tc.wire)
			if fastObj != reserveObj {
				t.Fatalf("object handles diverged (%d vs %d); message comparison invalid", fastObj, reserveObj)
			}
			if !errors.Is(fastErr, core.ErrFederateOverflow) {
				t.Fatalf("fast-path error = %v, want errors.Is ErrFederateOverflow", fastErr)
			}
			if !errors.Is(reserveErr, core.ErrFederateOverflow) {
				t.Fatalf("reserve-path error = %v, want errors.Is ErrFederateOverflow", reserveErr)
			}
			if !strings.Contains(fastErr.Error(), "reserve fanout:") {
				t.Fatalf("fast-path error = %q, want the reserve fanout prefix", fastErr)
			}
			if fastErr.Error() != reserveErr.Error() {
				t.Fatalf("error text diverged:\n fast    = %q\n reserve = %q", fastErr, reserveErr)
			}
		})
	}
}

func registerInteractionEndpoints(t *testing.T, decl *declaration.Manager) {
	t.Helper()
	ctx := context.Background()
	if err := decl.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := decl.SubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
		t.Fatal(err)
	}
}

func TestSendInteractionDirectSendSkipsReservationWithoutEventLog(t *testing.T) {
	outbox := &atomicUpdateOutbox{}
	reg, decl := newNoWALRegistry(t, outbox)
	registerInteractionEndpoints(t, decl)
	ctx := context.Background()
	delivered, sent := 0, 0
	reg.opts.OnInteractionDelivered = func(core.FederationName, core.FederateHandle) { delivered++ }
	reg.opts.OnInteractionSent = func(core.FederationName, core.FederateHandle) { sent++ }

	if err := reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {0x01}}, nil); err != nil {
		t.Fatalf("SendInteraction: %v", err)
	}
	if len(outbox.attempts) != 0 {
		t.Fatalf("Reserve attempts = %d, want 0 on the direct-send fast path", len(outbox.attempts))
	}
	if outbox.sendCalls != 1 || len(outbox.records) != 1 {
		t.Fatalf("Send calls = %d records = %d, want 1/1", outbox.sendCalls, len(outbox.records))
	}
	if outbox.records[0].Event.(*outboundEvent).Inner().GetReceive() == nil {
		t.Fatalf("event = %T, want ReceiveInteraction", outbox.records[0].Event)
	}
	if delivered != 1 || sent != 1 {
		t.Fatalf("callbacks = delivered %d sent %d, want 1/1", delivered, sent)
	}
}

func TestSendInteractionNoEventLogMultiRecipientStillReserves(t *testing.T) {
	outbox := &atomicUpdateOutbox{}
	reg, decl := newNoWALRegistry(t, outbox)
	registerInteractionEndpoints(t, decl)
	ctx := context.Background()
	if err := decl.SubscribeInteractionClass(ctx, "fed", 3, 7); err != nil {
		t.Fatal(err)
	}

	if err := reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {0x01}}, nil); err != nil {
		t.Fatalf("SendInteraction: %v", err)
	}
	if len(outbox.attempts) != 1 || len(outbox.attempts[0]) != 2 {
		t.Fatalf("Reserve attempts = %+v, want one reservation of 2 deliveries", outbox.attempts)
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0 (reservation commit delivers)", outbox.sendCalls)
	}
}

func TestSendInteractionEventLogSingleRecipientStillReserves(t *testing.T) {
	outbox := &atomicUpdateOutbox{}
	decl := declaration.New()
	log := &recordingEventLog{}
	reg, err := New(Options{
		EventLog: log, Declarations: decl, Outbox: outbox,
		FOMs:  &stubFOMRepo{},
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	registerInteractionEndpoints(t, decl)
	ctx := context.Background()

	if err := reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {0x01}}, nil); err != nil {
		t.Fatalf("SendInteraction: %v", err)
	}
	if len(outbox.attempts) != 1 || len(outbox.attempts[0]) != 1 {
		t.Fatalf("Reserve attempts = %+v, want one single-delivery reservation spanning the WAL append", outbox.attempts)
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0 (reservation commit delivers)", outbox.sendCalls)
	}
}

func interactionOverflowViaDirectSend(t *testing.T, wire bool) error {
	t.Helper()
	outbox := &atomicUpdateOutbox{sendErr: overflowSendErr()}
	reg, decl := newNoWALRegistry(t, outbox)
	registerInteractionEndpoints(t, decl)
	ctx := context.Background()
	var err error
	if wire {
		err = reg.SendInteractionWire(ctx, "fed", 1, 7, map[uint64][]byte{1: {0x01}}, nil)
	} else {
		err = reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {0x01}}, nil)
	}
	if len(outbox.attempts) != 0 {
		t.Fatalf("Reserve attempts = %d, want 0 on the direct-send fast path", len(outbox.attempts))
	}
	if outbox.sendCalls != 1 {
		t.Fatalf("Send calls = %d, want 1", outbox.sendCalls)
	}
	return err
}

func interactionOverflowViaReserve(t *testing.T, wire bool) error {
	t.Helper()
	outbox := &atomicUpdateOutbox{reserveErr: overflowSendErr()}
	decl := declaration.New()
	log := &recordingEventLog{}
	reg, err := New(Options{
		EventLog: log, Declarations: decl, Outbox: outbox,
		FOMs:  &stubFOMRepo{},
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	registerInteractionEndpoints(t, decl)
	ctx := context.Background()
	if wire {
		err = reg.SendInteractionWire(ctx, "fed", 1, 7, map[uint64][]byte{1: {0x01}}, nil)
	} else {
		err = reg.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {0x01}}, nil)
	}
	if len(outbox.attempts) != 1 {
		t.Fatalf("Reserve attempts = %d, want 1", len(outbox.attempts))
	}
	if outbox.sendCalls != 0 {
		t.Fatalf("Send calls = %d, want 0 after reservation rejection", outbox.sendCalls)
	}
	return err
}

func TestSendInteractionOverflowErrorSurfaceIdenticalAcrossBranches(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire bool
	}{
		{name: "typed", wire: false},
		{name: "wire", wire: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fastErr := interactionOverflowViaDirectSend(t, tc.wire)
			reserveErr := interactionOverflowViaReserve(t, tc.wire)
			if !errors.Is(fastErr, core.ErrFederateOverflow) {
				t.Fatalf("fast-path error = %v, want errors.Is ErrFederateOverflow", fastErr)
			}
			if !errors.Is(reserveErr, core.ErrFederateOverflow) {
				t.Fatalf("reserve-path error = %v, want errors.Is ErrFederateOverflow", reserveErr)
			}
			if !strings.Contains(fastErr.Error(), "reserve fanout:") {
				t.Fatalf("fast-path error = %q, want the reserve fanout prefix", fastErr)
			}
			if fastErr.Error() != reserveErr.Error() {
				t.Fatalf("error text diverged:\n fast    = %q\n reserve = %q", fastErr, reserveErr)
			}
		})
	}
}
