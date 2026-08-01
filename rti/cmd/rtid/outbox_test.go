package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// fakeOutboundEvent satisfies core.OutboundEvent without requiring a real
// *rtiv1.FederateEvent payload — used for cases where only the Send/Receive
// path is exercised.
type fakeOutboundEvent struct{ seq uint64 }

func (e *fakeOutboundEvent) Seq() uint64 { return e.seq }

func TestResolveMultiOutboxConfig(t *testing.T) {
	t.Run("zero values use production defaults", func(t *testing.T) {
		batchSize, flushInterval, err := resolveMultiOutboxConfig(0, 0)
		if err != nil {
			t.Fatalf("resolveMultiOutboxConfig: %v", err)
		}
		if batchSize != defaultMultiBatchSize || flushInterval != defaultMultiFlushInterval {
			t.Errorf("resolved config = (%d, %s), want (%d, %s)", batchSize, flushInterval, defaultMultiBatchSize, defaultMultiFlushInterval)
		}
	})

	for _, tc := range []struct {
		name          string
		batchSize     int
		flushInterval time.Duration
	}{
		{name: "negative batch size", batchSize: -1, flushInterval: time.Millisecond},
		{name: "batch size above maximum", batchSize: maxMultiBatchSize + 1, flushInterval: time.Millisecond},
		{name: "negative flush interval", batchSize: 1, flushInterval: -time.Nanosecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := resolveMultiOutboxConfig(tc.batchSize, tc.flushInterval); err == nil {
				t.Fatal("resolveMultiOutboxConfig returned nil error")
			}
		})
	}
}

func TestResolveMultiOutboxCapacity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input int
		want  int
		ok    bool
	}{
		{name: "zero uses production default", want: defaultMultiEventCapacity, ok: true},
		{name: "minimum", input: 1, want: 1, ok: true},
		{name: "maximum", input: maxMultiEventCapacity, want: maxMultiEventCapacity, ok: true},
		{name: "negative", input: -1},
		{name: "above maximum", input: maxMultiEventCapacity + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveMultiOutboxCapacity(tc.input)
			if tc.ok {
				if err != nil {
					t.Fatalf("resolveMultiOutboxCapacity(%d): %v", tc.input, err)
				}
				if got != tc.want {
					t.Fatalf("resolveMultiOutboxCapacity(%d) = %d, want %d", tc.input, got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveMultiOutboxCapacity(%d) returned nil error", tc.input)
			}
		})
	}
}

func BenchmarkMultiOutboxReserveCommitSingleRecipient(b *testing.B) {
	mo := newMultiOutboxWithBatch(1<<20, defaultMultiBatchSize, time.Hour)
	ch, cancel, err := mo.Subscribe(context.Background(), "bench", core.FederateHandle(1))
	if err != nil {
		b.Fatal(err)
	}
	drained := make(chan struct{})
	go func() {
		for range ch {
		}
		close(drained)
	}()
	b.Cleanup(func() {
		_ = cancel()
		<-drained
	})
	deliveries := []core.OutboxDelivery{{
		Recipient: core.FederateHandle(1),
		Event:     &fakeOutboundEvent{seq: 1},
	}}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		reservation, err := mo.Reserve(context.Background(), "bench", deliveries)
		if err != nil {
			b.Fatal(err)
		}
		if err := reservation.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMultiOutboxReserveCommitTwoRecipients(b *testing.B) {
	mo := newMultiOutboxWithBatch(1<<20, defaultMultiBatchSize, time.Hour)
	drained := make(chan struct{}, 2)
	var cancels []func() error
	for _, handle := range []core.FederateHandle{1, 2} {
		ch, cancel, err := mo.Subscribe(context.Background(), "bench", handle)
		if err != nil {
			b.Fatal(err)
		}
		cancels = append(cancels, cancel)
		go func() {
			for range ch {
			}
			drained <- struct{}{}
		}()
	}
	b.Cleanup(func() {
		for _, cancel := range cancels {
			_ = cancel()
		}
		<-drained
		<-drained
	})
	deliveries := []core.OutboxDelivery{
		{Recipient: core.FederateHandle(1), Event: &fakeOutboundEvent{seq: 1}},
		{Recipient: core.FederateHandle(2), Event: &fakeOutboundEvent{seq: 2}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		reservation, err := mo.Reserve(context.Background(), "bench", deliveries)
		if err != nil {
			b.Fatal(err)
		}
		if err := reservation.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

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

func TestMultiOutbox_TimeAdvanceGrantFlushesPrecedingEvents(t *testing.T) {
	mo := newMultiOutboxWithBatch(32, 32, time.Hour)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(7))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = cancel() }()

	first := &fakeOutboundEvent{seq: 1}
	second := &fakeOutboundEvent{seq: 2}
	grant := &timepkg.TimeAdvanceGrant{Time: 3}
	for _, event := range []core.OutboundEvent{first, second, grant} {
		if err := mo.Send(context.Background(), "alpha", core.FederateHandle(7), event); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	select {
	case batch := <-ch:
		if len(batch) != 3 || batch[0] != first || batch[1] != second || batch[2] != grant {
			t.Fatalf("delivered batch = %v, want preceding events followed by grant", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("time advance grant did not flush the recipient batch")
	}
}

func TestMultiOutbox_SendNoSubscriber_ReturnsUnavailable(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	err := mo.Send(context.Background(), "alpha", core.FederateHandle(99), &fakeOutboundEvent{seq: 1})
	if !errors.Is(err, core.ErrOutboxUnavailable) {
		t.Errorf("Send to absent subscriber: err = %v, want ErrOutboxUnavailable", err)
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

	if err := mo.Send(context.Background(), fed, h, &fakeOutboundEvent{seq: 100}); !errors.Is(err, core.ErrOutboxUnavailable) {
		t.Errorf("Send after Unbind: err=%v, want ErrOutboxUnavailable", err)
	}
}

func TestMultiOutbox_TimerFlushRetriesWithoutDrop(t *testing.T) {
	mo := newMultiOutboxWithBatch(2, 2, 5*time.Millisecond)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()

	for _, seq := range []uint64{1, 2, 3} {
		if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: seq}); err != nil {
			t.Fatalf("Send(%d): %v", seq, err)
		}
	}
	time.Sleep(20 * time.Millisecond) // timer observes a full channel and retries

	first := <-ch
	if len(first) != 2 || first[0].Seq() != 1 || first[1].Seq() != 2 {
		t.Fatalf("first batch = %v, want seq 1,2", first)
	}
	select {
	case second := <-ch:
		if len(second) != 1 || second[0].Seq() != 3 {
			t.Fatalf("retried batch = %v, want seq 3", second)
		}
	case <-time.After(time.Second):
		t.Fatal("timer-flushed batch was not retried")
	}
	state := (*mo.subs.Load())[fedHandleKey{fed: "alpha", h: 1}]
	if got := atomic.LoadUint64(&state.dropsTotal); got != 0 {
		t.Fatalf("dropsTotal = %d, want 0", got)
	}
}

func TestMultiOutbox_ConcurrentTimerFlushAndCancel(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		mo := newMultiOutboxWithBatch(2, 2, time.Microsecond)
		_, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 1}); err != nil {
			t.Fatal(err)
		}
		if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 2}); err != nil {
			t.Fatal(err)
		}
		if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 3}); err != nil {
			t.Fatal(err)
		}

		done := make(chan struct{})
		go func() {
			_ = cancel()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("cancel deadlocked with timer flush")
		}
		if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 4}); !errors.Is(err, core.ErrOutboxUnavailable) {
			t.Fatalf("Send after cancel = %v, want ErrOutboxUnavailable", err)
		}
	}
}

func TestMultiOutbox_ReservationRejectsAllRecipientsAtomically(t *testing.T) {
	mo := newMultiOutboxWithBatch(2, 1, 0)
	ch1, cancel1, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel1() }()
	_, cancel2, err := mo.Subscribe(context.Background(), "alpha", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel2() }()
	for seq := uint64(1); seq <= 2; seq++ {
		if err := mo.Send(context.Background(), "alpha", 2, &fakeOutboundEvent{seq: seq}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{
		{Recipient: 1, Event: &fakeOutboundEvent{seq: 8}},
		{Recipient: 2, Event: &fakeOutboundEvent{seq: 8}},
	}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("Reserve = %v, want ErrFederateOverflow", err)
	}
	if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 9}); err != nil {
		t.Fatalf("recipient 1 capacity leaked after failed reservation: %v", err)
	}
	select {
	case batch := <-ch1:
		if len(batch) != 1 || batch[0].Seq() != 9 {
			t.Fatalf("recipient 1 batch = %v, want seq 9 only", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("recipient 1 send did not arrive")
	}
}

func TestMultiOutbox_ReservationCommitsRepeatedRecipientsInOrder(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	ch1, cancel1, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel1() }()
	ch2, cancel2, err := mo.Subscribe(context.Background(), "alpha", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel2() }()

	reservation, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{
		{Recipient: 1, Event: &fakeOutboundEvent{seq: 10}},
		{Recipient: 1, Event: &fakeOutboundEvent{seq: 11}},
		{Recipient: 2, Event: &fakeOutboundEvent{seq: 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []uint64{10, 11} {
		batch := <-ch1
		if len(batch) != 1 || batch[0].Seq() != want {
			t.Fatalf("recipient 1 batch = %v, want seq %d", batch, want)
		}
	}
	batch := <-ch2
	if len(batch) != 1 || batch[0].Seq() != 20 {
		t.Fatalf("recipient 2 batch = %v, want seq 20", batch)
	}
}

func TestMultiOutbox_ReservationAccountsForGrantForcedFlush(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 4, 0)
	_, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	deliveries := make([]core.OutboxDelivery, 0, 7)
	for seq := uint64(1); seq <= 6; seq++ {
		deliveries = append(deliveries, core.OutboxDelivery{Recipient: 1, Event: &fakeOutboundEvent{seq: seq}})
	}
	deliveries = append(deliveries, core.OutboxDelivery{Recipient: 1, Event: &timepkg.TimeAdvanceGrant{Time: 7}})
	if _, err := mo.Reserve(context.Background(), "alpha", deliveries); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("Reserve = %v, want ErrFederateOverflow before WAL", err)
	}
	if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 9}); err != nil {
		t.Fatalf("failed reservation mutated recipient capacity: %v", err)
	}
}

func TestMultiOutbox_SingleReservationCommitsInOrderAndIsIdempotent(t *testing.T) {
	mo := newMultiOutboxWithBatch(9, 3, time.Hour)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 1}); err != nil {
		t.Fatal(err)
	}
	reservation, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{{
		Recipient: 1,
		Event:     &fakeOutboundEvent{seq: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatalf("second Commit: %v", err)
	}
	reservation.Release()
	if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 3}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-ch:
		if len(batch) != 3 || batch[0].Seq() != 1 || batch[1].Seq() != 2 || batch[2].Seq() != 3 {
			t.Fatalf("batch = %v, want seq 1,2,3", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("single-recipient reservation was not delivered")
	}
}

func TestMultiOutbox_SingleReservationReleaseDoesNotDeliver(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	reservation, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{{
		Recipient: 1,
		Event:     &fakeOutboundEvent{seq: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reservation.Release()
	reservation.Release()
	if err := reservation.Commit(); err != nil {
		t.Fatalf("Commit after Release: %v", err)
	}
	if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 2}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-ch:
		if len(batch) != 1 || batch[0].Seq() != 2 {
			t.Fatalf("batch = %v, want released event omitted", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("send after reservation release was not delivered")
	}
}

func TestMultiOutbox_SingleReservationRejectsUnavailableAndOverflow(t *testing.T) {
	mo := newMultiOutboxWithBatch(1, 1, 0)
	delivery := []core.OutboxDelivery{{Recipient: 1, Event: &fakeOutboundEvent{seq: 2}}}
	if _, err := mo.Reserve(context.Background(), "alpha", delivery); !errors.Is(err, core.ErrOutboxUnavailable) {
		t.Fatalf("missing recipient Reserve = %v, want ErrOutboxUnavailable", err)
	}
	_, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := mo.Reserve(context.Background(), "alpha", delivery); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("full recipient Reserve = %v, want ErrFederateOverflow", err)
	}
	if err := cancel(); err != nil {
		t.Fatal(err)
	}
	if _, err := mo.Reserve(context.Background(), "alpha", delivery); !errors.Is(err, core.ErrOutboxUnavailable) {
		t.Fatalf("closed recipient Reserve = %v, want ErrOutboxUnavailable", err)
	}
}

func TestMultiOutbox_SingleGrantReservationRequiresFlushCapacity(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 4, 0)
	_, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	if err := mo.Send(context.Background(), "alpha", 1, &timepkg.TimeAdvanceGrant{Time: 1}); err != nil {
		t.Fatal(err)
	}
	normal, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{{
		Recipient: 1,
		Event:     &fakeOutboundEvent{seq: 2},
	}})
	if err != nil {
		t.Fatalf("normal scratch reservation with full channel: %v", err)
	}
	normal.Release()
	if _, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{{
		Recipient: 1,
		Event:     &timepkg.TimeAdvanceGrant{Time: 3},
	}}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("grant reservation = %v, want ErrFederateOverflow", err)
	}
}

func TestMultiOutbox_SingleReservationBlocksCancelUntilRelease(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	_, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{{
		Recipient: 1,
		Event:     &fakeOutboundEvent{seq: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cancel()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("cancel completed while reservation held the recipient lock")
	case <-time.After(10 * time.Millisecond):
	}
	reservation.Release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel remained blocked after reservation release")
	}
}

func TestMultiOutbox_SingleReservationOwnsDeliveryValue(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	deliveries := []core.OutboxDelivery{{Recipient: 1, Event: &fakeOutboundEvent{seq: 1}}}
	reservation, err := mo.Reserve(context.Background(), "alpha", deliveries)
	if err != nil {
		t.Fatal(err)
	}
	deliveries[0] = core.OutboxDelivery{Recipient: 2, Event: &fakeOutboundEvent{seq: 99}}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-ch:
		if len(batch) != 1 || batch[0].Seq() != 1 {
			t.Fatalf("batch = %v, want original seq 1", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("owned single delivery was not committed")
	}
}

func TestMultiOutbox_SingleReservationPreventsSendOvertake(t *testing.T) {
	mo := newMultiOutboxWithBatch(4, 1, 0)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	reservation, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{{
		Recipient: 1,
		Event:     &fakeOutboundEvent{seq: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 2})
	}()
	select {
	case err := <-sendDone:
		t.Fatalf("concurrent Send completed before reservation: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-sendDone; err != nil {
		t.Fatal(err)
	}
	for _, want := range []uint64{1, 2} {
		select {
		case batch := <-ch:
			if len(batch) != 1 || batch[0].Seq() != want {
				t.Fatalf("batch = %v, want seq %d", batch, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for seq %d", want)
		}
	}
}

func TestMultiOutbox_SingleReservationConcurrentCommitReleaseIsOnceOnly(t *testing.T) {
	mo := newMultiOutboxWithBatch(8, 1, 0)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	reservation, err := mo.Reserve(context.Background(), "alpha", []core.OutboxDelivery{{
		Recipient: 1,
		Event:     &fakeOutboundEvent{seq: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	commitErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		commitErr <- reservation.Commit()
	}()
	go func() {
		defer wg.Done()
		reservation.Release()
	}()
	wg.Wait()
	if err := <-commitErr; err != nil {
		t.Fatal(err)
	}
	if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: 2}); err != nil {
		t.Fatal(err)
	}
	first := <-ch
	if len(first) != 1 {
		t.Fatalf("first batch = %v", first)
	}
	if first[0].Seq() == 1 {
		second := <-ch
		if len(second) != 1 || second[0].Seq() != 2 {
			t.Fatalf("second batch = %v, want seq 2", second)
		}
	} else if first[0].Seq() != 2 {
		t.Fatalf("first seq = %d, want optional 1 then required 2", first[0].Seq())
	}
	select {
	case duplicate := <-ch:
		t.Fatalf("duplicate reservation delivery: %v", duplicate)
	default:
	}
}

// TestMultiOutbox_DuplicateSubscribeStillRejected verifies that a second
// reader cannot attach while the first subscription is active.
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

// TestMultiOutbox_PersistentFlushTimerRearmsAfterDisarm — W6d: the
// per-recipient timer is persistent (created once, Reset thereafter).
// Exercises the disarm (full-batch flush stops the timer) -> re-arm
// (Reset) -> timer-flush cycle twice, so the second cycle runs entirely
// on the Reset path of the reused timer.
func TestMultiOutbox_PersistentFlushTimerRearmsAfterDisarm(t *testing.T) {
	mo := newMultiOutboxWithBatch(64, 4, time.Millisecond)
	ch, cancel, err := mo.Subscribe(context.Background(), "alpha", core.FederateHandle(1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()

	recv := func(wantSeqs []uint64) {
		t.Helper()
		select {
		case batch := <-ch:
			if len(batch) != len(wantSeqs) {
				t.Fatalf("batch length = %d, want %d", len(batch), len(wantSeqs))
			}
			for i, want := range wantSeqs {
				if got := batch[i].Seq(); got != want {
					t.Fatalf("batch[%d] seq = %d, want %d", i, got, want)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for batch %v", wantSeqs)
		}
	}

	seq := uint64(0)
	send := func(n int) []uint64 {
		t.Helper()
		seqs := make([]uint64, 0, n)
		for range n {
			seq++
			if err := mo.Send(context.Background(), "alpha", 1, &fakeOutboundEvent{seq: seq}); err != nil {
				t.Fatalf("Send seq %d: %v", seq, err)
			}
			seqs = append(seqs, seq)
		}
		return seqs
	}

	for cycle := range 2 {
		// One event arms the timer; three more complete the batch, which
		// flushes synchronously and disarms (Stop, generation bump).
		full := send(4)
		recv(full)
		// A single trailing event re-arms; only the deferred flush can
		// deliver it.
		tail := send(1)
		recv(tail)
		_ = cycle
	}
}

// TestMultiOutbox_FlushTimerGenerationRace — W6d: hammers the persistent
// timer's arm/disarm generation counter from three sides at once (sender
// full-batch disarms, timer-callback flushes, reader drains) under -race.
// The invariant is exactly the outbox delivery contract: every accepted
// event is delivered exactly once, in send order, with no stale timer
// fire duplicating or dropping a batch.
func TestMultiOutbox_FlushTimerGenerationRace(t *testing.T) {
	const total = 5000
	mo := newMultiOutboxWithBatch(1<<15, 4, 10*time.Microsecond)
	ch, cancel, err := mo.Subscribe(context.Background(), "race", core.FederateHandle(1))
	if err != nil {
		t.Fatal(err)
	}

	readerDone := make(chan error, 1)
	go func() {
		next := uint64(1)
		for batch := range ch {
			for _, evt := range batch {
				if evt.Seq() != next {
					readerDone <- fmt.Errorf("event seq = %d, want %d (stale timer fire reordered or duplicated a batch)", evt.Seq(), next)
					return
				}
				next++
			}
		}
		if next != total+1 {
			readerDone <- fmt.Errorf("delivered %d events, want %d", next-1, total)
			return
		}
		readerDone <- nil
	}()

	for seq := uint64(1); seq <= total; seq++ {
		for {
			err := mo.Send(context.Background(), "race", 1, &fakeOutboundEvent{seq: seq})
			if err == nil {
				break
			}
			if !errors.Is(err, core.ErrFederateOverflow) {
				t.Fatalf("Send seq %d: %v", seq, err)
			}
			time.Sleep(10 * time.Microsecond)
		}
		// Stall inside partially filled batches so the 10us timer fires
		// while the next Send is about to disarm/re-arm — the in-flight
		// callback vs Reset window the generation counter guards.
		if seq%3 == 1 {
			time.Sleep(15 * time.Microsecond)
		}
	}
	if err := cancel(); err != nil {
		t.Fatal(err)
	}
	if err := <-readerDone; err != nil {
		t.Fatal(err)
	}
}

// TestMultiOutbox_FlushTimerCloseRace — W6d: closing the recipient
// (Unbind) while the persistent timer is firing must never send on the
// closed channel or resurrect the timer.
func TestMultiOutbox_FlushTimerCloseRace(t *testing.T) {
	for range 200 {
		mo := newMultiOutboxWithBatch(64, 8, 5*time.Microsecond)
		ch, _, err := mo.Subscribe(context.Background(), "race", core.FederateHandle(1))
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			for range ch { //nolint:revive // draining
			}
		}()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for seq := uint64(1); seq <= 32; seq++ {
				if err := mo.Send(context.Background(), "race", 1, &fakeOutboundEvent{seq: seq}); err != nil {
					return // closed underneath us — expected
				}
			}
		}()
		time.Sleep(20 * time.Microsecond)
		mo.Unbind("race", core.FederateHandle(1))
		wg.Wait()
	}
}
