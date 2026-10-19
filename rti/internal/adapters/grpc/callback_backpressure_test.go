package grpc

import (
	"context"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

func TestDataCallbackPressurePreservesControlHeadroom(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 4)
	out := NewMandatoryCallbackOutbox(&failingCallbackQueue{}, func() *CallbackReceipts { return r })
	r.opts.ControlReserve = 2
	data := &requiredCallbackEvent{event: &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{}}}}
	deliveries := []core.OutboxDelivery{{Recipient: 1, Event: data}, {Recipient: 2, Event: data}}
	first, err := out.Reserve(context.Background(), "f", deliveries)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	extra, err := out.Reserve(context.Background(), "f", deliveries[:1])
	if extra != nil {
		defer extra.Release()
	}
	var pressure *callbackCapacityPressure
	if !errors.As(err, &pressure) || r.pending != 2 {
		t.Fatalf("data consumed control headroom: pending=%d err=%v", r.pending, err)
	}
	control := &syncpkg.SynchronizedEvent{Sequence: 1}
	second, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: control}, {Recipient: 2, Event: control}})
	if err != nil {
		t.Fatalf("control fanout blocked by data pressure: %v", err)
	}
	defer second.Release()
	if r.pending != 4 {
		t.Fatalf("pending=%d, want exact total capacity", r.pending)
	}
	extra, err = out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: control}})
	if extra != nil {
		defer extra.Release()
	}
	if !errors.As(err, &pressure) {
		t.Fatalf("control exceeded total capacity: %v", err)
	}
	first.Release()
	second.Release()
	select {
	case <-pressure.ready:
	default:
		t.Fatal("release did not wake capacity waiters")
	}
	third, err := out.Reserve(context.Background(), "f", deliveries)
	if err != nil {
		t.Fatal(err)
	}
	third.Release()
	extra, err = out.Reserve(context.Background(), "f", append(deliveries, deliveries[0]))
	if extra != nil {
		defer extra.Release()
	}
	if err == nil || errors.As(err, &pressure) {
		t.Fatalf("impossible data fanout must not wait forever: %v", err)
	}
}

func TestCallbackControlReserveValidation(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 4)
	for _, reserve := range []int{-1, 4, 5} {
		opts := r.opts
		opts.ControlReserve = reserve
		if _, err := NewCallbackReceipts(opts); err == nil {
			t.Fatalf("accepted invalid control reserve %d", reserve)
		}
	}
}

func TestDirectDataCallbackAdmissionPreservesControlHeadroom(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 2)
	r.opts.ControlReserve = 1
	data := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{}}}
	_, first, err := r.PrepareRequired(context.Background(), "f", 1, data)
	if err != nil {
		t.Fatal(err)
	}
	defer first(false)
	_, extra, err := r.PrepareRequired(context.Background(), "f", 1, data)
	defer extra(false)
	if err == nil || r.pending != 1 {
		t.Fatalf("direct data admission stole control headroom: pending=%d err=%v", r.pending, err)
	}
	control := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{}}}
	_, second, err := r.PrepareRequired(context.Background(), "f", 1, control)
	if err != nil {
		t.Fatal(err)
	}
	defer second(false)
}

func TestMandatoryCallbackPressureReservesWholeFanoutBeforePreparing(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 2)
	event := &syncpkg.SynchronizedEvent{Sequence: 1}
	first, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: event}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	validated, unavailable := 0, 0
	r.opts.ValidateRequiredRecipient = func(context.Context, core.FederationName, core.FederateHandle) error { validated++; return nil }
	r.opts.OnUnavailable = func(core.FederationName, core.FederateHandle, error) { unavailable++ }
	_, err = out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: event}, {Recipient: 2, Event: event}})
	if err == nil || r.pending != 1 || len(r.entries) != 1 {
		t.Fatalf("non-atomic admission: pending=%d entries=%d error=%v", r.pending, len(r.entries), err)
	}
	if validated != 0 || unavailable != 0 {
		t.Fatalf("temporary pressure prepared recipients or invalidated coverage: validated=%d unavailable=%d", validated, unavailable)
	}
}

func TestMandatoryCallbackPressureWaitsForCapacityAndRejectsImpossibleFanout(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 1)
	delivery := []core.OutboxDelivery{{Recipient: 1, Event: &syncpkg.SynchronizedEvent{Sequence: 1}}}
	first, err := out.Reserve(context.Background(), "f", delivery)
	if err != nil {
		t.Fatal(err)
	}
	_, err = out.Reserve(context.Background(), "f", delivery)
	var pressure *callbackCapacityPressure
	if !errors.As(err, &pressure) {
		t.Fatalf("missing pressure notification: %v", err)
	}
	select {
	case <-pressure.ready:
		t.Fatal("capacity signalled without release")
	default:
	}
	first.Release()
	select {
	case <-pressure.ready:
	default:
		t.Fatal("release did not wake waiter")
	}
	_, err = out.Reserve(context.Background(), "f", append(delivery, delivery...))
	if err == nil || errors.As(err, &pressure) {
		t.Fatalf("impossible fanout must not wait forever: %v", err)
	}
	if r.pending != 0 || r.reserved != 0 {
		t.Fatal("capacity leaked")
	}
}

func TestMandatoryCallbackCreditsExcludeConcurrentAdmissions(t *testing.T) {
	r, _, out := requiredCallbackFixture(t, 2)
	credits, err := r.reserveCredits(2)
	if err != nil {
		t.Fatal(err)
	}
	defer credits.release()
	_, err = out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: &syncpkg.SynchronizedEvent{}}})
	var pressure *callbackCapacityPressure
	if !errors.As(err, &pressure) || r.pending != 0 {
		t.Fatalf("reserved capacity was stolen: %v", err)
	}
	credits.release()
	select {
	case <-pressure.ready:
	default:
		t.Fatal("unused credit release did not notify")
	}
}
