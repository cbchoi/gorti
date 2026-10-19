package delivery

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func inspectQueue(t *testing.T, observation *OutboxQueueObservation, admitted, drained, invalidated, scratch, queued uint64, closed bool) {
	t.Helper()
	got := observation.Inspect()
	if got.Admitted != admitted || got.Drained != drained || got.Invalidated != invalidated || got.Scratch != scratch || got.Queued != queued || got.Closed != closed {
		t.Fatalf("queue = %+v; want admitted/drained/invalidated/scratch/queued/closed = %d/%d/%d/%d/%d/%v", got, admitted, drained, invalidated, scratch, queued, closed)
	}
	if got.Admitted != got.Drained+got.Invalidated+got.Scratch+got.Queued {
		t.Fatalf("custody conservation failed: %+v", got)
	}
}

func observedRecipient(t *testing.T, m *MultiOutbox, h core.FederateHandle) (<-chan []core.OutboundEvent, *OutboxQueueObservation, func() error) {
	t.Helper()
	m.Bind("f", h)
	ch, cancel, err := m.Subscribe(context.Background(), "f", h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cancel() })
	observation, err := m.ObserveQueue("f", h)
	if err != nil {
		t.Fatal(err)
	}
	return ch, observation, cancel
}

func sendObserved(t *testing.T, m *MultiOutbox, h core.FederateHandle, event core.OutboundEvent) {
	t.Helper()
	if err := m.Send(context.Background(), "f", h, event); err != nil {
		t.Fatal(err)
	}
}

func TestQueueObservationAdmissionDrainAndOverflow(t *testing.T) {
	m := NewObservedWithBatch(2, 2, 0)
	ch, o, _ := observedRecipient(t, m, 1)
	// Reused producer sequence and payload are separate admissions.
	event := &fakeOutboundEvent{seq: 7}
	sendObserved(t, m, 1, event)
	inspectQueue(t, o, 1, 0, 0, 1, 0, false)
	sendObserved(t, m, 1, &timepkg.TimeAdvanceGrant{})
	inspectQueue(t, o, 2, 0, 0, 0, 2, false)
	sendObserved(t, m, 1, event)
	if err := m.Send(context.Background(), "f", 1, event); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("overflow: %v", err)
	}
	inspectQueue(t, o, 3, 0, 0, 1, 2, false)
	batch := <-ch
	if len(batch) != 2 || batch[0] != event {
		t.Fatalf("changed payload/order: %v", batch)
	}
	inspectQueue(t, o, 3, 2, 0, 1, 0, false)
	sendObserved(t, m, 1, event)
	if len(<-ch) != 2 {
		t.Fatal("wrong final batch")
	}
	inspectQueue(t, o, 4, 4, 0, 0, 0, false)
}

func TestQueueObservationReservationsAreCommittedOnly(t *testing.T) {
	m := NewObservedWithBatch(1, 1, 0)
	ch1, o1, _ := observedRecipient(t, m, 1)
	ch2, o2, _ := observedRecipient(t, m, 2)
	event := &fakeOutboundEvent{seq: 9}
	deliveries := []core.OutboxDelivery{{Recipient: 1, Event: event}, {Recipient: 2, Event: event}}
	r, err := m.Reserve(context.Background(), "f", deliveries)
	if err != nil {
		t.Fatal(err)
	}
	r.Release()
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	inspectQueue(t, o1, 0, 0, 0, 0, 0, false)
	sendObserved(t, m, 2, event)
	if _, err := m.Reserve(context.Background(), "f", deliveries); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("fanout overflow: %v", err)
	}
	inspectQueue(t, o1, 0, 0, 0, 0, 0, false)
	if len(ch1) != 0 {
		t.Fatal("failed fanout leaked first recipient")
	}
	<-ch2
	r, err = m.Reserve(context.Background(), "f", deliveries)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	r.Release()
	inspectQueue(t, o1, 1, 0, 0, 0, 1, false)
	inspectQueue(t, o2, 2, 1, 0, 0, 1, false)
	<-ch1
	<-ch2
}

func TestQueueObservationRestoreAndTeardown(t *testing.T) {
	m := NewObservedWithBatch(4, 2, 0)
	ch, old, cancel := observedRecipient(t, m, 1)
	event := &fakeOutboundEvent{seq: 3}
	for i := 0; i < 5; i++ {
		sendObserved(t, m, 1, event)
	}
	<-ch
	transition := []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 2, Event: &fakeOutboundEvent{seq: 99}}}
	r, _, err := m.PrepareRestore(context.Background(), "f", transition)
	if err != nil {
		t.Fatal(err)
	}
	r.Release()
	inspectQueue(t, old, 5, 2, 0, 1, 2, false)
	r, _, err = m.PrepareRestore(context.Background(), "f", transition)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	inspectQueue(t, old, 5, 2, 3, 0, 0, true)
	next, err := m.ObserveQueue("f", 2)
	if err != nil {
		t.Fatal(err)
	}
	if next == old || next.Inspect().CallbackEpoch != 1 || next.Inspect().Handle != 2 {
		t.Fatal("restore did not replace observation epoch")
	}
	inspectQueue(t, next, 1, 0, 0, 0, 1, false)
	if got := <-ch; len(got) != 1 || got[0].Seq() != 99 {
		t.Fatal("restore first-event guarantee lost")
	}
	sendObserved(t, m, 2, event)
	if err := cancel(); err != nil {
		t.Fatal(err)
	}
	inspectQueue(t, next, 2, 1, 1, 0, 0, true)
	// Cancel still flushes accepted scratch. Invalidation is not cancellation.
	if got := <-ch; len(got) != 1 || got[0] != event {
		t.Fatal("cancel changed final-flush delivery")
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed")
	}
	m.Bind("f", 2)
	t.Cleanup(func() { m.Unbind("f", 2) })
	rebound, err := m.ObserveQueue("f", 2)
	if err != nil || rebound == next {
		t.Fatal("reused handle reused observation")
	}
	inspectQueue(t, rebound, 0, 0, 0, 0, 0, false)
	inspectQueue(t, next, 2, 1, 1, 0, 0, true)
}

func TestQueueObservationShortBatchRejectionAndTimerRetry(t *testing.T) {
	m := NewObservedWithBatch(4, 4, time.Hour)
	ch, o, _ := observedRecipient(t, m, 1)
	sendObserved(t, m, 1, &timepkg.TimeAdvanceGrant{})
	event := &fakeOutboundEvent{seq: 1}
	sendObserved(t, m, 1, event)
	sendObserved(t, m, 1, event)
	// A grant forces an attempted short flush even with apparent event capacity.
	if err := m.Send(context.Background(), "f", 1, &timepkg.TimeAdvanceGrant{}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("grant rejection: %v", err)
	}
	state := (*m.subs.Load())[fedHandleKey{fed: "f", h: 1}]
	m.flushScratch(state, state.flushGen.Load())
	inspectQueue(t, o, 3, 0, 0, 2, 1, false)
	if len(<-ch) != 1 {
		t.Fatal("short grant batch changed")
	}
	m.flushScratch(state, state.flushGen.Load())
	inspectQueue(t, o, 3, 1, 0, 0, 2, false)
	if len(<-ch) != 2 {
		t.Fatal("retry did not retain scratch")
	}
	inspectQueue(t, o, 3, 3, 0, 0, 0, false)
}

func TestQueueObservationActualTimer(t *testing.T) {
	m := NewObservedWithBatch(4, 4, time.Millisecond)
	ch, o, _ := observedRecipient(t, m, 1)
	event := &fakeOutboundEvent{seq: 1}
	sendObserved(t, m, 1, event)
	select {
	case batch := <-ch:
		if len(batch) != 1 || batch[0] != event {
			t.Fatal("timer changed payload")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timer did not flush")
	}
	inspectQueue(t, o, 1, 1, 0, 0, 0, false)
}

func TestQueueObservationSingleReservationAndDuplicateRecipient(t *testing.T) {
	m := NewObservedWithBatch(4, 4, 0)
	ch, o, _ := observedRecipient(t, m, 1)
	event := &fakeOutboundEvent{seq: 7}
	single := []core.OutboxDelivery{{Recipient: 1, Event: event}}
	r, err := m.Reserve(context.Background(), "f", single)
	if err != nil {
		t.Fatal(err)
	}
	// Inspect is independent of the reservation's held recipient lock.
	inspectQueue(t, o, 0, 0, 0, 0, 0, false)
	r.Release()
	r, err = m.Reserve(context.Background(), "f", single)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	r.Release()
	inspectQueue(t, o, 1, 0, 0, 1, 0, false)
	deliveries := []core.OutboxDelivery{{Recipient: 1, Event: event}, {Recipient: 1, Event: &timepkg.TimeAdvanceGrant{}}}
	r, err = m.Reserve(context.Background(), "f", deliveries)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	inspectQueue(t, o, 3, 0, 0, 0, 3, false)
	if len(<-ch) != 3 {
		t.Fatal("duplicate-recipient batch changed")
	}
	inspectQueue(t, o, 3, 3, 0, 0, 0, false)
}

func TestQueueObservationCloseVariants(t *testing.T) {
	for _, mode := range []string{"cancel", "unbind", "federation"} {
		t.Run(mode, func(t *testing.T) {
			m := NewObservedWithBatch(2, 2, time.Hour)
			ch, o, cancel := observedRecipient(t, m, 1)
			m.Bind("other", 1)
			t.Cleanup(func() { m.UnbindFederation("other") })
			unrelated, err := m.ObserveQueue("other", 1)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				sendObserved(t, m, 1, &fakeOutboundEvent{seq: uint64(i)})
			}
			state := (*m.subs.Load())[fedHandleKey{fed: "f", h: 1}]
			gen := state.flushGen.Load()
			switch mode {
			case "cancel":
				_ = cancel()
			case "unbind":
				m.Unbind("f", 1)
			case "federation":
				m.UnbindFederation("f")
			}
			m.flushScratch(state, gen)
			m.Unbind("f", 1)
			_ = cancel()
			inspectQueue(t, o, 3, 0, 3, 0, 0, true)
			inspectQueue(t, unrelated, 0, 0, 0, 0, 0, false)
			if o.ch != nil || o.batches != nil || state.flushArmed || state.scratch != nil {
				t.Fatal("teardown retained observation channel, metadata, scratch or timer")
			}
			if _, err := m.ObserveQueue("f", 1); !errors.Is(err, core.ErrOutboxUnavailable) {
				t.Fatalf("closed binding: %v", err)
			}
			if len(<-ch) != 2 {
				t.Fatal("teardown changed already-buffered batch")
			}
			if _, ok := <-ch; ok {
				t.Fatal("teardown left stream open")
			}
			inspectQueue(t, o, 3, 0, 3, 0, 0, true)
			if mode == "cancel" && state.dropsTotal != 1 {
				t.Fatal("legacy cancel-drop count changed")
			}
		})
	}
}

func TestQueueObservationDisabledAndDirectSubscribe(t *testing.T) {
	m := NewWithBatch(4, 4, 0)
	ch, cancel, err := m.Subscribe(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cancel() })
	if _, err := m.ObserveQueue("f", 1); !errors.Is(err, ErrQueueObservationDisabled) {
		t.Fatalf("disabled: %v", err)
	}
	state := (*m.subs.Load())[fedHandleKey{fed: "f", h: 1}]
	event := &fakeOutboundEvent{seq: 1}
	allocations := testing.AllocsPerRun(100, func() {
		for i := 0; i < 4; i++ {
			_ = m.Send(context.Background(), "f", 1, event)
		}
		<-ch
	})
	// The existing outbox allocates one replacement scratch per flushed batch.
	if state.observation != nil || allocations != 1 {
		t.Fatalf("disabled observation/state allocations = %v / %g", state.observation, allocations)
	}
	observed := NewObservedWithBatch(1, 1, 0)
	direct, stop, err := observed.Subscribe(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stop() })
	o, err := observed.ObserveQueue("f", 1)
	if err != nil {
		t.Fatal(err)
	}
	sendObserved(t, observed, 1, event)
	<-direct
	inspectQueue(t, o, 1, 1, 0, 0, 0, false)
}

func TestQueueObservationConcurrentDrainAndInspect(t *testing.T) {
	m := NewObservedWithBatch(32, 4, 0)
	ch, o, cancel := observedRecipient(t, m, 1)
	const n = 5000
	drained := make(chan uint64, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		var count uint64
		for batch := range ch {
			count += uint64(len(batch))
		}
		drained <- count
	}()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var previous OutboxQueueCounts
		for {
			select {
			case <-stop:
				return
			default:
			}
			got := o.Inspect()
			if got.Admitted != got.Drained+got.Invalidated+got.Scratch+got.Queued || got.Admitted < previous.Admitted || got.Drained < previous.Drained {
				t.Errorf("concurrent conservation/monotonicity: before=%+v after=%+v", previous, got)
				return
			}
			previous = got
			runtime.Gosched()
		}
	}()
	t.Cleanup(func() { _ = cancel(); close(stop); wg.Wait(); <-readerDone })
	for i := 0; i < n; i++ {
		var event core.OutboundEvent = &fakeOutboundEvent{seq: uint64(i)}
		if i%5 == 4 {
			event = &timepkg.TimeAdvanceGrant{}
		}
		for {
			err := m.Send(context.Background(), "f", 1, event)
			if err == nil {
				break
			}
			if !errors.Is(err, core.ErrFederateOverflow) {
				t.Fatal(err)
			}
			runtime.Gosched()
		}
	}
	deadline := time.Now().Add(5 * time.Second)                   //nolint:forbidigo // Bound the real-time worker drain, not federation logical time.
	for o.Inspect().Drained != n && time.Now().Before(deadline) { //nolint:forbidigo // Detect a stalled background worker within the wall-clock deadline.
		runtime.Gosched()
	}
	inspectQueue(t, o, n, n, 0, 0, 0, false)
	_ = cancel()
	if got := <-drained; got != n {
		t.Fatalf("independent reader got %d events, want %d", got, n)
	}
	inspectQueue(t, o, n, n, 0, 0, 0, true)
}

func TestQueueObservationRestoreRacesVariableBatchDrain(t *testing.T) {
	for round := 0; round < 100; round++ {
		m := NewObservedWithBatch(128, 4, 0)
		ch, old, cancel := observedRecipient(t, m, 1)
		const count = 80
		for i := 0; i < count; i++ {
			var event core.OutboundEvent = &fakeOutboundEvent{seq: uint64(i + 1)}
			if i%5 == 4 {
				event = &timepkg.TimeAdvanceGrant{}
			}
			sendObserved(t, m, 1, event)
		}
		reader := make(chan uint64, 1)
		terminal := &fakeOutboundEvent{seq: 999}
		go func() {
			var oldEvents uint64
			for batch := range ch {
				for _, event := range batch {
					if event == terminal {
						reader <- oldEvents
						return
					}
					oldEvents++
				}
				runtime.Gosched()
			}
			reader <- oldEvents
		}()
		r, _, err := m.PrepareRestore(context.Background(), "f", []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 1, Event: terminal}})
		if err != nil {
			_ = cancel()
			<-reader
			t.Fatal(err)
		}
		if err := r.Commit(); err != nil {
			_ = cancel()
			<-reader
			t.Fatal(err)
		}
		got := <-reader
		inspectQueue(t, old, count, got, count-got, 0, 0, true)
		next, err := m.ObserveQueue("f", 1)
		if err != nil {
			t.Fatal(err)
		}
		inspectQueue(t, next, 1, 1, 0, 0, 0, false)
		_ = cancel()
	}
}

func TestQueueObservationRealTimeHandoffFailureAndRetraction(t *testing.T) {
	m := NewObservedWithBatch(1, 1, 0)
	ch, o, _ := observedRecipient(t, m, 1)
	manager, err := timepkg.New(timepkg.Options{Clock: core.NewManualClock(time.Unix(0, 0)), Outbox: m})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.OnFederationDestroyed("f") })
	ctx := context.Background()
	blocker := &fakeOutboundEvent{seq: 1}
	waiting := &fakeOutboundEvent{seq: 2}
	retracted := &fakeOutboundEvent{seq: 3}
	if err := manager.EnableConstrained(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	if batch := <-ch; len(batch) != 1 {
		t.Fatalf("activation callbacks=%v", batch)
	} else if _, ok := batch[0].(*timepkg.TimeConstrainedEnabled); !ok {
		t.Fatalf("activation callback=%T", batch[0])
	}
	sendObserved(t, m, 1, blocker)
	if err := manager.BufferTSO(ctx, "f", 1, 5, waiting); err != nil {
		t.Fatal(err)
	}
	if err := manager.BufferTSOWithRetraction(ctx, "f", 1, 6, retracted, 2, 99); err != nil {
		t.Fatal(err)
	}
	inspectQueue(t, o, 2, 1, 0, 0, 1, false)
	if removed := manager.RetractMessage("f", 2, 99); removed != 1 {
		t.Fatalf("removed %d, want 1", removed)
	}
	if err := manager.DisableConstrained(ctx, "f", 1); err != nil {
		t.Fatalf("disable constraint: %v", err)
	}
	if manager.Metrics("f", 1).Constrained {
		t.Fatal("transport pressure rolled back the accepted constraint change")
	}
	if retries := manager.PendingDeliveryRetries(); len(retries) != 1 || retries[0] != "f" {
		t.Fatalf("failed handoff did not retain retry work: %v", retries)
	}
	if err := manager.RetryPendingDeliveries(ctx, "f"); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("retry before capacity is released: %v", err)
	}
	if ts, pending := manager.QueryLITS("f", 1); !pending || ts != 5 {
		t.Fatalf("failed drain lost buffered event: %v/%v", ts, pending)
	}
	inspectQueue(t, o, 2, 1, 0, 0, 1, false)
	if batch := <-ch; len(batch) != 1 || batch[0] != blocker {
		t.Fatal("blocker changed")
	}
	if err := manager.RetryPendingDeliveries(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	if retries := manager.PendingDeliveryRetries(); len(retries) != 0 {
		t.Fatalf("successful drain retained retry work: %v", retries)
	}
	if _, pending := manager.QueryLITS("f", 1); pending {
		t.Fatal("successful drain retained time custody")
	}
	inspectQueue(t, o, 3, 2, 0, 0, 1, false)
	if batch := <-ch; len(batch) != 1 || batch[0] != waiting {
		t.Fatal("drain reordered, duplicated or delivered retracted event")
	}
	inspectQueue(t, o, 3, 3, 0, 0, 0, false)
	if err := manager.RetryPendingDeliveries(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	inspectQueue(t, o, 3, 3, 0, 0, 0, false)
	select {
	case batch := <-ch:
		t.Fatalf("completed retry delivered duplicate or retracted events: %v", batch)
	default:
	}
}
