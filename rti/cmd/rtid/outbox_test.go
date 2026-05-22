package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeOutboundEvent satisfies core.OutboundEvent without requiring a real
// *rtiv1.FederateEvent payload — used for cases where only the Send/Receive
// path is exercised.
type fakeOutboundEvent struct{ seq uint64 }

func (e *fakeOutboundEvent) Seq() uint64 { return e.seq }

// TestMultiOutbox_SendDeliversToSubscriber: a Send to (fed, h) appears on
// the channel returned by Subscribe(fed, h).
func TestMultiOutbox_SendDeliversToSubscriber(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(7))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = cancel() }()

	if err := mo.Send(context.Background(), "alpha", core.FederateHandle(7), &fakeOutboundEvent{seq: 42}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case batch := <-ch:
		if len(batch) != 1 || batch[0].Seq() != 42 {
			t.Errorf("delivered batch = %v, want [Seq=42]", batch)
		}
	case <-time.After(time.Second):
		t.Fatalf("Send did not deliver within 1s")
	}
}

// TestMultiOutbox_SendNoSubscriber_NoError: with no subscriber registered
// for (fed, h), Send drops the event silently. Per docs/agent-a-rti-core.md
// the bounded-channel contract handles overflow as a federate-level crash;
// "no subscriber" is a separate condition (federate hasn't started its
// stream yet) that must NOT crash the federation.
func TestMultiOutbox_SendNoSubscriber_NoError(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	if err := mo.Send(context.Background(), "alpha", core.FederateHandle(99), &fakeOutboundEvent{seq: 1}); err != nil {
		t.Errorf("Send to absent subscriber: err = %v, want nil (drop silently)", err)
	}
}

// TestMultiOutbox_OverflowReturnsError: when the per-federate channel is
// full, Send returns ErrFederateOverflow.
func TestMultiOutbox_OverflowReturnsError(t *testing.T) {
	mo := newMultiOutboxWithBatch(2, 1, 0)
	_, cancel, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(1))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = cancel() }()

	for i := 0; i < 2; i++ {
		if err := mo.Send(context.Background(), "alpha", core.FederateHandle(1), &fakeOutboundEvent{seq: uint64(i + 1)}); err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
	}
	err = mo.Send(context.Background(), "alpha", core.FederateHandle(1), &fakeOutboundEvent{seq: 99})
	if !errors.Is(err, core.ErrFederateOverflow) {
		t.Errorf("Send into full channel: err = %v, want ErrFederateOverflow", err)
	}
}

// TestMultiOutbox_PerFederateIsolation: a subscriber for (fed, 1) does NOT
// see events sent to (fed, 2).
func TestMultiOutbox_PerFederateIsolation(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	ch1, cancel1, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(1))
	if err != nil {
		t.Fatalf("Subscribe 1: %v", err)
	}
	defer func() { _ = cancel1() }()
	_, cancel2, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(2))
	if err != nil {
		t.Fatalf("Subscribe 2: %v", err)
	}
	defer func() { _ = cancel2() }()

	if err := mo.Send(context.Background(), "alpha", core.FederateHandle(2), &fakeOutboundEvent{seq: 7}); err != nil {
		t.Fatalf("Send to 2: %v", err)
	}

	select {
	case batch := <-ch1:
		t.Errorf("federate 1 received batch %v intended for federate 2", batch)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestMultiOutbox_CancelClosesChannel: cancel() unregisters the subscriber
// and closes its channel; subsequent Sends do not block.
func TestMultiOutbox_CancelClosesChannel(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(1))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := cancel(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// Channel should be closed (or eventually closed); a receive returns
	// the zero value with ok=false when closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("channel returned an event after cancel; want closed")
		}
	case <-time.After(time.Second):
		t.Fatalf("channel did not close within 1s after cancel")
	}
}

// TestMultiOutbox_DoubleSubscribeIsRejected: the same (fed, h) pair cannot
// have two concurrent subscribers — the federate already owns the stream.
func TestMultiOutbox_DoubleSubscribeIsRejected(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	_, cancel, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(1))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = cancel() }()

	if _, _, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(1)); err == nil {
		t.Errorf("second Subscribe to (alpha, 1) returned nil error; want rejection")
	}
}

// TestMultiOutbox_ConcurrentSendSubscribe: race-clean under -race.
func TestMultiOutbox_ConcurrentSendSubscribe(t *testing.T) {
	mo := newMultiOutboxWithBatch(64, 1, 0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			fed := core.FederationName("fed-" + string(rune('A'+i)))
			ch, cancel, err := mo.Subscribe(context.Background(), fed, core.FederateHandle(1))
			if err != nil {
				t.Errorf("Subscribe: %v", err)
				return
			}
			defer func() { _ = cancel() }()
			for j := 0; j < 16; j++ {
				_ = mo.Send(context.Background(), fed, core.FederateHandle(1), &fakeOutboundEvent{seq: uint64(j + 1)})
			}
			received := 0
		drain:
			for {
				select {
				case <-ch:
					received++
					if received == 16 {
						break drain
					}
				case <-time.After(time.Second):
					break drain
				}
			}
			if received != 16 {
				t.Errorf("federate %s: received %d events, want 16", fed, received)
			}
		}()
	}
	wg.Wait()
}

// TestMultiOutbox_BindBuffersEventsBeforeSubscribe — M27 Phase A.
// After Bind, Send for that (fed, h) buffers into the per-recipient
// channel. A later Subscribe attaches a reader and drains the buffered
// events. Closes the race where service-group RPCs fire between
// JoinFederation returning and StreamService.Events connecting.
func TestMultiOutbox_BindBuffersEventsBeforeSubscribe(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	const fed = core.FederationName("alpha")
	const h = core.FederateHandle(3)

	// 1. Bind first — simulates the federation join hook firing
	//    before the federate opens its Events stream.
	mo.Bind(fed, h)

	// 2. Send several events. With the pre-M27 behaviour these would
	//    be silently dropped. With Bind, they buffer into the channel.
	for i := 1; i <= 3; i++ {
		if err := mo.Send(context.Background(), fed, h, &fakeOutboundEvent{seq: uint64(i)}); err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
	}

	// 3. Subscribe — should attach to the existing state and the
	//    reader sees the buffered events.
	ch, cancel, err := mo.Subscribe(context.Background(), fed, h)
	if err != nil {
		t.Fatalf("Subscribe after Bind: %v", err)
	}
	defer func() { _ = cancel() }()

	got := make([]uint64, 0, 3)
	deadline := time.After(2 * time.Second)
	for len(got) < 3 {
		select {
		case batch, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed early; got=%v", got)
			}
			for _, ev := range batch {
				got = append(got, ev.Seq())
			}
		case <-deadline:
			t.Fatalf("did not drain 3 events within 2s; got=%v", got)
		}
	}
	wantSet := map[uint64]bool{1: true, 2: true, 3: true}
	for _, s := range got {
		if !wantSet[s] {
			t.Errorf("unexpected seq %d delivered; want one of {1,2,3}", s)
		}
	}
}

// TestMultiOutbox_BindIsIdempotent — calling Bind twice for the same
// (fed, h) is a no-op (second call does not destroy the existing
// state or its buffered events).
func TestMultiOutbox_BindIsIdempotent(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	const fed = core.FederationName("alpha")
	const h = core.FederateHandle(1)

	mo.Bind(fed, h)
	_ = mo.Send(context.Background(), fed, h, &fakeOutboundEvent{seq: 1})
	mo.Bind(fed, h) // second Bind must not lose the buffered event

	ch, cancel, err := mo.Subscribe(context.Background(), fed, h)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = cancel() }()

	select {
	case batch := <-ch:
		if len(batch) != 1 || batch[0].Seq() != 1 {
			t.Errorf("delivered=%v; want [seq=1]", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("event lost after second Bind")
	}
}

// TestMultiOutbox_UnbindDropsBufferedEvents — Unbind without a prior
// Subscribe cleans up the state. A later Subscribe gets a fresh
// channel (the old buffered events are dropped, intended — the
// federate that bound but resigned without subscribing wouldn't
// have wanted those events anyway).
func TestMultiOutbox_UnbindDropsBufferedEvents(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	const fed = core.FederationName("alpha")
	const h = core.FederateHandle(2)

	mo.Bind(fed, h)
	_ = mo.Send(context.Background(), fed, h, &fakeOutboundEvent{seq: 99})
	mo.Unbind(fed, h)

	// After Unbind, a Send for the same (fed, h) drops silently
	// (back to pre-Bind behaviour).
	if err := mo.Send(context.Background(), fed, h, &fakeOutboundEvent{seq: 100}); err != nil {
		t.Errorf("Send after Unbind: err=%v, want nil (silent drop)", err)
	}
}

// TestMultiOutbox_DuplicateSubscribeStillRejected — even with Bind,
// a second Subscribe while the first reader is attached must reject.
// Two readers would split the event stream.
func TestMultiOutbox_DuplicateSubscribeStillRejected(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	const fed = core.FederationName("alpha")
	const h = core.FederateHandle(4)

	mo.Bind(fed, h)
	_, cancel, err := mo.Subscribe(context.Background(), fed, h)
	if err != nil {
		t.Fatalf("first Subscribe: %v", err)
	}
	defer func() { _ = cancel() }()

	_, _, err = mo.Subscribe(context.Background(), fed, h)
	if err == nil {
		t.Fatal("second Subscribe: nil error; want duplicate-subscriber rejection")
	}
}

// TestMultiOutbox_SubscribeAfterCancelWorks — cancel releases the
// reader slot, so a subsequent Subscribe for the same (fed, h)
// succeeds (it's a fresh subscription on a fresh state).
func TestMultiOutbox_SubscribeAfterCancelWorks(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	const fed = core.FederationName("alpha")
	const h = core.FederateHandle(5)

	_, cancel1, err := mo.Subscribe(context.Background(), fed, h)
	if err != nil {
		t.Fatalf("Subscribe 1: %v", err)
	}
	_ = cancel1()

	_, cancel2, err := mo.Subscribe(context.Background(), fed, h)
	if err != nil {
		t.Fatalf("Subscribe 2 after cancel: %v", err)
	}
	_ = cancel2()
}
