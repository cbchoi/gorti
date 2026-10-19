package perf

import (
	"context"
	"fmt"
	"reflect"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type perfOutboxTestEvent uint64

func (e perfOutboxTestEvent) Seq() uint64 { return uint64(e) }

func subscribePerfOutbox(t *testing.T, o *perfOutbox) (*perfRecipientState, func() error) {
	t.Helper()
	_, cancel, err := o.Subscribe(context.Background(), "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cancel(); err != nil {
			t.Error(err)
		}
	})
	return (*o.subs.Load())[fedHandleKey{fed: "test", h: 1}], cancel
}

func sendPerfOutboxEvent(t *testing.T, o *perfOutbox, seq uint64) {
	t.Helper()
	if err := o.Send(context.Background(), "test", 1, perfOutboxTestEvent(seq)); err != nil {
		t.Fatal(err)
	}
}

func requirePerfOutboxEvents(t *testing.T, ch <-chan []core.OutboundEvent, want []uint64) {
	t.Helper()
	var got []uint64
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case batch, ok := <-ch:
			if !ok {
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("events = %v, want %v", got, want)
				}
				return
			}
			for _, evt := range batch {
				got = append(got, evt.Seq())
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for channel closure")
		}
	}
}

func TestPerfOutboxCancelWithPendingTimer(t *testing.T) {
	o := newPerfOutbox(128)
	o.batchSize = 2
	o.flushInterval = time.Hour
	state, cancel := subscribePerfOutbox(t, o)
	for seq := uint64(1); seq <= 3; seq++ {
		sendPerfOutboxEvent(t, o, seq)
	}

	started := make(chan struct{})
	flushed := make(chan struct{})
	canceled := make(chan error, 1)
	func() {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.flushTimer.Stop()
		generation := state.flushGen
		// Start the callback while its recipient lock is held. Cancellation
		// must handle Stop returning false without losing the final batch.
		state.flushTimer = time.AfterFunc(0, func() {
			close(started)
			o.flushScratch(state, generation)
			close(flushed)
		})
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("timer callback did not start")
		}
		go func() { canceled <- cancel() }()
		deadline := time.Now().Add(5 * time.Second) //nolint:forbidigo // Bound actual worker execution during the benchmark.
		for len(*o.subs.Load()) != 0 {
			if time.Now().After(deadline) { //nolint:forbidigo // Fail stalled delivery using a wall-clock deadline.
				t.Fatal("cancellation did not remove subscription")
			}
			goruntime.Gosched()
		}
		select {
		case err := <-canceled:
			t.Fatalf("cancellation returned before acquiring recipient lock: %v", err)
		default:
		}
	}()
	select {
	case err := <-canceled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("timer callback did not finish")
	}
	requirePerfOutboxEvents(t, state.ch, []uint64{1, 2, 3})
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.closed || state.flushTimer != nil || state.scratch != nil {
		t.Fatal("cancellation left recipient state active")
	}
}

func TestPerfOutboxSendFromCanceledSnapshot(t *testing.T) {
	for _, batchSize := range []int{1, 4} {
		t.Run(fmt.Sprintf("batchSize=%d", batchSize), func(t *testing.T) {
			o := newPerfOutbox(128)
			o.batchSize = batchSize
			o.flushInterval = time.Hour
			state, cancel := subscribePerfOutbox(t, o)
			sendPerfOutboxEvent(t, o, 1)

			// Retain the immutable subscriber snapshot to deterministically
			// model Send loading it before cancel and taking state.mu after.
			stale := newPerfOutbox(128)
			stale.batchSize = o.batchSize
			stale.flushInterval = o.flushInterval
			stale.subs.Store(o.subs.Load())
			if err := cancel(); err != nil {
				t.Fatal(err)
			}
			replacement, cancelReplacement := subscribePerfOutbox(t, o)
			sendPerfOutboxEvent(t, stale, 2)
			sendPerfOutboxEvent(t, o, 3)
			if err := cancel(); err != nil {
				t.Fatal(err)
			}
			if (*o.subs.Load())[fedHandleKey{fed: "test", h: 1}] != replacement {
				t.Fatal("old cancellation removed replacement subscription")
			}
			if err := cancelReplacement(); err != nil {
				t.Fatal(err)
			}
			requirePerfOutboxEvents(t, state.ch, []uint64{1})
			requirePerfOutboxEvents(t, replacement.ch, []uint64{3})
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.scratch != nil || state.flushTimer != nil {
				t.Fatal("stale sender appended scratch or armed a timer after cancellation")
			}
		})
	}
}

func TestPerfOutboxStaleTimerPreservesNewBatch(t *testing.T) {
	o := newPerfOutbox(128)
	o.batchSize = 2
	o.flushInterval = time.Hour
	state, cancel := subscribePerfOutbox(t, o)
	sendPerfOutboxEvent(t, o, 1)
	state.mu.Lock()
	oldGeneration := state.flushGen
	state.mu.Unlock()
	sendPerfOutboxEvent(t, o, 2)
	sendPerfOutboxEvent(t, o, 3)
	state.mu.Lock()
	newGeneration, newTimer := state.flushGen, state.flushTimer
	state.mu.Unlock()

	// A stopped timer may already have dispatched its callback. Run that
	// callback after the next batch has armed its own timer.
	o.flushScratch(state, oldGeneration)
	func() {
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.flushTimer != newTimer || len(state.scratch) != 1 || len(state.ch) != 1 {
			t.Fatal("stale timer disturbed the next batch")
		}
	}()
	o.flushScratch(state, newGeneration)
	if err := cancel(); err != nil {
		t.Fatal(err)
	}
	requirePerfOutboxEvents(t, state.ch, []uint64{1, 2, 3})
}

func TestPerfOutboxTimerFlush(t *testing.T) {
	o := newPerfOutbox(128)
	state, cancel := subscribePerfOutbox(t, o)
	sendPerfOutboxEvent(t, o, 1)
	select {
	case batch := <-state.ch:
		if len(batch) != 1 || batch[0].Seq() != 1 {
			t.Fatalf("timer delivered %v, want event 1", batch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timer did not flush partial batch")
	}
	if err := cancel(); err != nil {
		t.Fatal(err)
	}
	requirePerfOutboxEvents(t, state.ch, nil)
}

func TestPerfOutboxFullChannelRemainsNonblocking(t *testing.T) {
	for _, flush := range []string{"send", "timer", "cancel"} {
		t.Run(flush, func(t *testing.T) {
			o := newPerfOutbox(1)
			o.batchSize = 2
			o.flushInterval = time.Hour
			state, cancel := subscribePerfOutbox(t, o)
			done := make(chan error, 1)
			go func() {
				for seq := uint64(1); seq <= 3; seq++ {
					if err := o.Send(context.Background(), "test", 1, perfOutboxTestEvent(seq)); err != nil {
						done <- err
						return
					}
				}
				switch flush {
				case "send":
					if err := o.Send(context.Background(), "test", 1, perfOutboxTestEvent(4)); err != nil {
						done <- err
						return
					}
				case "timer":
					state.mu.Lock()
					generation := state.flushGen
					state.mu.Unlock()
					o.flushScratch(state, generation)
				}
				done <- cancel()
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				// Release a mistakenly blocking flush before cleanup cancels.
				<-state.ch
				t.Fatal("full receiver channel blocked flush or cancellation")
			}
			requirePerfOutboxEvents(t, state.ch, []uint64{1, 2})
		})
	}
}
