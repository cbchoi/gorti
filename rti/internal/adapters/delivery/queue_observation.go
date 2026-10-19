package delivery

import (
	"errors"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// OutboxQueueCounts counts envelopes only within one outbox binding and epoch.
// Drained means removed from the channel, never SDK enqueue or callback invocation.
// Invalidated ends observation, not necessarily delivery of already-buffered data.
// These are NOT HLAROlength/HLATSOlength or an end-to-end delivery metric.
type OutboxQueueCounts struct {
	Federation                     core.FederationName
	Handle                         core.FederateHandle
	CallbackEpoch                  uint64
	Admitted, Drained, Invalidated uint64
	Scratch, Queued                uint64
	Closed                         bool
}

var ErrQueueObservationDisabled = errors.New("outbox queue observation disabled")

// OutboxQueueObservation is an epoch-bound read port. It retains batch lengths,
// not event identities or payloads. Inspect reconciles FIFO channel drains;
// counts are exact at its channel-length sample, not a multi-recipient snapshot.
// Close/restore detach the channel and retain only a final scalar snapshot.
type OutboxQueueObservation struct {
	mu         sync.Mutex
	ch         <-chan []core.OutboundEvent
	batches    []uint64
	head, size int
	counts     OutboxQueueCounts
}

// NewObservedWithBatch opts into bounded event-count diagnostics. The ordinary
// constructors retain disabled accounting. No server composition is changed.
func NewObservedWithBatch(eventCapacity, batchSize int, flushInterval time.Duration) *MultiOutbox {
	m := NewWithBatch(eventCapacity, batchSize, flushInterval)
	m.observeQueues = true
	return m
}

func (m *MultiOutbox) ObserveQueue(fed core.FederationName, h core.FederateHandle) (*OutboxQueueObservation, error) {
	if !m.observeQueues {
		return nil, ErrQueueObservationDisabled
	}
	state := (*m.subs.Load())[fedHandleKey{fed: fed, h: h}]
	if state == nil {
		return nil, core.ErrOutboxUnavailable
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed || state.key != (fedHandleKey{fed: fed, h: h}) {
		return nil, core.ErrOutboxUnavailable
	}
	return state.observation, nil
}

func (o *OutboxQueueObservation) Inspect() OutboxQueueCounts {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.counts.Closed {
		o.drainLocked()
	}
	return o.counts
}

func (m *MultiOutbox) newQueueObservation(state *multiRecipientState) *OutboxQueueObservation {
	if !m.observeQueues {
		return nil
	}
	return &OutboxQueueObservation{
		ch: state.ch,
		// One extra slot permits appending a newly published batch before
		// reconciling a concurrent receive that made room for that batch.
		batches: make([]uint64, cap(state.ch)+1),
		counts:  OutboxQueueCounts{Federation: state.key.fed, Handle: state.key.h, CallbackEpoch: state.callbackEpoch},
	}
}

// Writers hold the recipient lock, then this observation's lock across channel
// publication. Inspect takes only the latter, so it cannot observe a half-update
// or wait on an uncommitted outbox reservation. No external code is called.
func (o *OutboxQueueObservation) admitLocked() {
	o.counts.Admitted++
	o.counts.Scratch++
}

func (o *OutboxQueueObservation) batchLocked(n uint64) {
	o.batches[(o.head+o.size)%len(o.batches)] = n
	o.size++
	o.counts.Scratch -= n
	o.counts.Queued += n
	o.drainLocked()
}

func (o *OutboxQueueObservation) drainLocked() {
	remaining := len(o.ch)
	for o.size > remaining {
		n := o.batches[o.head]
		o.batches[o.head] = 0
		o.head = (o.head + 1) % len(o.batches)
		o.size--
		o.counts.Queued -= n
		o.counts.Drained += n
	}
}

func (o *OutboxQueueObservation) closeLocked() {
	o.drainLocked()
	o.counts.Invalidated += o.counts.Scratch + o.counts.Queued
	o.detachLocked()
}

// Restore competes with the reader for channel batches. Only the envelopes
// actually removed by restore are invalidated; all other queued envelopes were
// drained by the reader. Summing removed lengths avoids assigning the wrong
// short batch to the reader when a receive races the purge.
func (o *OutboxQueueObservation) restoredLocked(removed uint64) {
	o.counts.Drained += o.counts.Queued - removed
	o.counts.Invalidated += o.counts.Scratch + removed
	o.detachLocked()
}

func (o *OutboxQueueObservation) detachLocked() {
	o.counts.Scratch, o.counts.Queued = 0, 0
	o.counts.Closed = true
	o.ch, o.batches = nil, nil
	o.head, o.size = 0, 0
}
