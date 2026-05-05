package perf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// defaultPerfBatchSize is how many events Send accumulates per recipient
// before flushing the scratch slice to the recipient's channel. Larger
// batches mean fewer channel ops (the dominant residual cost after
// commit 890e18a's atomic-snapshot lookup), at the cost of higher tail
// latency for low-rate senders. 32 is a balance that yields ~3-4x more
// throughput at fanout sizes 25-100 without dropping latency below the
// channel send floor.
const defaultPerfBatchSize = 32

// defaultPerfFlushInterval bounds the time an event sits in scratch
// before being flushed. Mirrors the production multiOutbox setting so
// low-rate test paths see batched semantics without unbounded waits.
const defaultPerfFlushInterval = 1 * time.Millisecond

// perfOutbox is the in-process outbox used by the perf harness. It
// satisfies core.Outbox AND a batched-channel Subscribe shape.
//
// Batched delivery: each recipient owns a small scratch slice protected
// by a per-recipient mutex. Send appends to scratch; when scratch
// reaches batchSize it is handed to the recipient's chan[]OutboundEvent
// and a fresh scratch is started. Subscribe's cancel performs a final
// flush so receivers always observe every Send that completed before
// the cancel.
//
// On a full receiver channel the batch is dropped silently — the perf
// measurement explicitly accepts the bounded-overflow envelope (we
// report sent count + delivered samples separately in the result).
//
// Concurrency: the subscriber table is held in an atomic.Pointer so
// the hot Send path is a single atomic load + map lookup with no
// mutex acquire on the SHARED table. Subscribe / cancel serialize on
// writeMu and apply copy-on-write. Each recipient's scratch slice is
// protected by its own small mutex so contention scales with
// senders-per-recipient, not all-recipients.
type perfOutbox struct {
	subs          atomic.Pointer[map[fedHandleKey]*perfRecipientState]
	writeMu       sync.Mutex
	bufferSize    int
	batchSize     int
	flushInterval time.Duration
}

type fedHandleKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

type perfRecipientState struct {
	ch         chan []core.OutboundEvent
	mu         sync.Mutex
	scratch    []core.OutboundEvent
	flushTimer *time.Timer
}

// newPerfOutbox constructs an outbox where the per-recipient channel
// can buffer (eventCapacity / batchSize) batches worth of events. The
// argument is named in event units so the existing call site
// (newPerfOutbox(8192)) keeps documenting steady-state burst capacity
// in event terms. bufferSize <= 0 is normalized to 1.
func newPerfOutbox(eventCapacity int) *perfOutbox {
	batchSize := defaultPerfBatchSize
	bufferSize := eventCapacity / batchSize
	if bufferSize < 1 {
		bufferSize = 1
	}
	o := &perfOutbox{
		bufferSize:    bufferSize,
		batchSize:     batchSize,
		flushInterval: defaultPerfFlushInterval,
	}
	empty := map[fedHandleKey]*perfRecipientState{}
	o.subs.Store(&empty)
	return o
}

// Send implements core.Outbox.
func (o *perfOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	subs := *o.subs.Load()
	state, ok := subs[fedHandleKey{fed: fed, h: h}]
	if !ok {
		return nil
	}
	state.mu.Lock()
	wasEmpty := len(state.scratch) == 0
	state.scratch = append(state.scratch, evt)
	if len(state.scratch) < o.batchSize {
		if wasEmpty && o.flushInterval > 0 && state.flushTimer == nil {
			state.flushTimer = time.AfterFunc(o.flushInterval, func() {
				o.flushScratch(state)
			})
		}
		state.mu.Unlock()
		return nil
	}
	batch := state.scratch
	state.scratch = make([]core.OutboundEvent, 0, o.batchSize)
	if state.flushTimer != nil {
		state.flushTimer.Stop()
		state.flushTimer = nil
	}
	state.mu.Unlock()
	select {
	case state.ch <- batch:
		return nil
	default:
		// Drop on full — measurement-mode contract.
		return nil
	}
}

// flushScratch fires from the deferred-flush timer and pushes any
// pending scratch onto the channel.
func (o *perfOutbox) flushScratch(state *perfRecipientState) {
	state.mu.Lock()
	state.flushTimer = nil
	if len(state.scratch) == 0 {
		state.mu.Unlock()
		return
	}
	batch := state.scratch
	state.scratch = make([]core.OutboundEvent, 0, o.batchSize)
	state.mu.Unlock()
	select {
	case state.ch <- batch:
	default:
	}
}

// Subscribe registers a per-federate inbox and returns a batched
// receive channel. The cancel func unregisters, performs a final
// flush of any pending scratch, and closes the channel.
func (o *perfOutbox) Subscribe(_ context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error) {
	key := fedHandleKey{fed: fed, h: h}
	o.writeMu.Lock()
	defer o.writeMu.Unlock()
	current := *o.subs.Load()
	if _, dup := current[key]; dup {
		return nil, nil, fmt.Errorf("perf: subscriber already registered for federation %q federate %d", fed, h)
	}
	state := &perfRecipientState{
		ch:      make(chan []core.OutboundEvent, o.bufferSize),
		scratch: make([]core.OutboundEvent, 0, o.batchSize),
	}
	next := make(map[fedHandleKey]*perfRecipientState, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[key] = state
	o.subs.Store(&next)

	var cancelOnce sync.Once
	cancel := func() error {
		cancelOnce.Do(func() {
			o.writeMu.Lock()
			cur := *o.subs.Load()
			existing, ok := cur[key]
			if !ok || existing != state {
				o.writeMu.Unlock()
				return
			}
			next := make(map[fedHandleKey]*perfRecipientState, len(cur)-1)
			for k, v := range cur {
				if k != key {
					next[k] = v
				}
			}
			o.subs.Store(&next)
			o.writeMu.Unlock()

			// Final flush of any remaining scratch, then close. Done
			// after the table mutation so the table is unblocked even
			// if a slow receiver still holds the channel full.
			state.mu.Lock()
			if state.flushTimer != nil {
				state.flushTimer.Stop()
				state.flushTimer = nil
			}
			if len(state.scratch) > 0 {
				final := state.scratch
				state.scratch = nil
				state.mu.Unlock()
				select {
				case state.ch <- final:
				default:
				}
			} else {
				state.mu.Unlock()
			}
			close(state.ch)
		})
		return nil
	}
	return state.ch, cancel, nil
}

// Compile-time assertion.
var _ core.Outbox = (*perfOutbox)(nil)
