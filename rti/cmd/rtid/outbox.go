package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// defaultMultiBatchSize is how many events Send accumulates per
// recipient before flushing the scratch slice to the recipient's
// channel. Mirrors rti/internal/perf.defaultPerfBatchSize after the
// microbench sweep that picked 32 as the throughput plateau on the
// in-process harness; the production gRPC streaming loop is the
// downstream consumer and benefits identically from amortized
// channel ops.
const defaultMultiBatchSize = 32

// defaultMultiFlushInterval bounds the time an event may sit in a
// per-recipient scratch slice before being flushed to the channel.
// Without this bound a low-rate sender (1 event/sec) would wait
// batchSize seconds to be visible to the receiver. The interval is
// short enough that the wire-visible latency is dominated by the
// channel send + gRPC frame cost, not the batching delay.
const defaultMultiFlushInterval = 1 * time.Millisecond

// multiOutbox is the production implementation of both core.Outbox and
// grpc.SubscribableOutbox. It maintains one bounded channel per
// (federation, federate) pair carrying batches of events; Send appends
// to a per-recipient scratch slice, and when the scratch reaches
// batchSize it is handed to the recipient's chan[]OutboundEvent and a
// fresh scratch is started.
//
// Concurrency: the subscriber table is held in an atomic.Pointer so
// the hot Send path is a single atomic load + map lookup with no
// mutex acquire on the SHARED table. Subscribe / cancel serialize on
// writeMu and apply copy-on-write. Each recipient's scratch is
// protected by its own small mutex so contention scales with
// senders-per-recipient, not all-recipients. The per-recipient
// channel itself is goroutine-safe by Go's channel semantics.
//
// Overflow: when the recipient's batch channel is full, Send returns
// core.ErrFederateOverflow (matching the cut-1 crash-on-overflow
// contract — federation manager treats the federate as crashed).
// The scratch is preserved on overflow so a retry sees the same
// pending events, but the production federation manager treats the
// first overflow as terminal and never retries.
type multiOutbox struct {
	subs          atomic.Pointer[map[fedHandleKey]*multiRecipientState]
	writeMu       sync.Mutex
	bufferSize    int
	batchSize     int
	flushInterval time.Duration
}

type fedHandleKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

type multiRecipientState struct {
	ch      chan []core.OutboundEvent
	mu      sync.Mutex
	scratch []core.OutboundEvent
	// flushTimer schedules a deferred flush so events do not sit in
	// scratch indefinitely under low-rate workloads. nil means no
	// flush is pending; the first Send into an empty scratch arms it.
	flushTimer *time.Timer
	// dropsTotal counts events dropped because the batch channel was
	// full at the moment Send / flushScratch tried to enqueue. Read by
	// the AdminService Snapshot RPC; mutated only via atomic.AddUint64
	// to avoid acquiring the recipient mutex on the overflow path.
	// Phase 1 of the rtid-TUI plan (docs/rtid-tui.md): exposed as
	// FederateSnapshot.drops_total. The accumulator is the count of
	// batched events lost (each batch may carry up to batchSize
	// individual events), so on a full-channel drop we add len(batch)
	// to the counter — that matches the per-event semantics callers
	// expect from "drops_total".
	dropsTotal uint64
	// readerAttached: M27 Phase A — set to true when a Subscribe call
	// attaches a reader (the gRPC streamService.Events loop). Used to
	// reject a duplicate Subscribe for the same (fed, h) while
	// permitting the Bind→Subscribe sequence that pre-creates the
	// channel before the federate's stream opens. Held under mu.
	readerAttached bool
}

// newMultiOutbox constructs an outbox where the per-federate batch
// channel can buffer (eventCapacity / batchSize) batches. The argument
// is named in event units so existing callers continue to document
// steady-state burst capacity in event terms. bufferSize <= 0 is
// normalized to 1 (a degenerate but legal value used by tests that
// exercise the overflow path).
func newMultiOutbox(eventCapacity int) *multiOutbox {
	return newMultiOutboxWithBatch(eventCapacity, defaultMultiBatchSize, defaultMultiFlushInterval)
}

// newMultiOutboxWithBatch is the explicit-knobs constructor. Tests use
// it with batchSize=1 (no accumulation) and flushInterval=0 (no
// deferred flush) to keep their per-event channel semantics identical
// to the pre-batching design; production callers use newMultiOutbox
// with the defaults.
func newMultiOutboxWithBatch(eventCapacity, batchSize int, flushInterval time.Duration) *multiOutbox {
	if batchSize < 1 {
		batchSize = 1
	}
	bufferSize := eventCapacity / batchSize
	if bufferSize < 1 {
		bufferSize = 1
	}
	m := &multiOutbox{
		bufferSize:    bufferSize,
		batchSize:     batchSize,
		flushInterval: flushInterval,
	}
	empty := map[fedHandleKey]*multiRecipientState{}
	m.subs.Store(&empty)
	return m
}

// Send implements core.Outbox. The federate's batch channel is bounded;
// on a full channel Send returns core.ErrFederateOverflow per the cut-1
// contract.
//
// "No subscriber" is silently dropped — the federate may not have
// established its outbound stream yet, which is a normal startup window
// rather than a crash condition.
func (m *multiOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	subs := *m.subs.Load()
	state, ok := subs[fedHandleKey{fed: fed, h: h}]
	if !ok {
		return nil
	}
	state.mu.Lock()
	wasEmpty := len(state.scratch) == 0
	state.scratch = append(state.scratch, evt)
	if len(state.scratch) < m.batchSize {
		// Arm the deferred-flush timer when the first event lands in
		// an empty scratch. It fires after flushInterval to bound the
		// time low-rate events spend waiting for batchSize to fill.
		if wasEmpty && m.flushInterval > 0 && state.flushTimer == nil {
			state.flushTimer = time.AfterFunc(m.flushInterval, func() {
				m.flushScratch(state)
			})
		}
		state.mu.Unlock()
		return nil
	}
	batch := state.scratch
	state.scratch = make([]core.OutboundEvent, 0, m.batchSize)
	if state.flushTimer != nil {
		state.flushTimer.Stop()
		state.flushTimer = nil
	}
	state.mu.Unlock()
	select {
	case state.ch <- batch:
		return nil
	default:
		// Per-recipient drop counter — atomic so we don't reacquire
		// state.mu on the overflow path. The counter is in-event
		// units (batched events lost), matching what the TUI's
		// "drops_total" column reports.
		atomic.AddUint64(&state.dropsTotal, uint64(len(batch)))
		return fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, fed, h)
	}
}

// flushScratch is invoked by the deferred-flush timer. It hands any
// pending scratch events to the recipient's channel. On a full
// channel the batch is dropped silently — the production overflow
// path is exercised only by Send (the synchronous path that returns
// ErrFederateOverflow); a backlog in the timer-driven path means the
// receiver is genuinely overwhelmed, and we'd rather lose a low-rate
// flush than block the timer goroutine pool.
func (m *multiOutbox) flushScratch(state *multiRecipientState) {
	state.mu.Lock()
	state.flushTimer = nil
	if len(state.scratch) == 0 {
		state.mu.Unlock()
		return
	}
	batch := state.scratch
	state.scratch = make([]core.OutboundEvent, 0, m.batchSize)
	state.mu.Unlock()
	select {
	case state.ch <- batch:
	default:
		// Same counter as Send's overflow path.
		atomic.AddUint64(&state.dropsTotal, uint64(len(batch)))
	}
}

// Bind pre-creates the per-(fed, h) recipient state so events sent
// before the federate opens its outbound stream are buffered instead
// of silently dropped. M27 Phase A — closes the race where a
// service-group RPC (e.g. ReserveObjectInstanceName) fires immediately
// after JoinFederation returns but before StreamService.Events
// connects.
//
// Idempotent: if state already exists for (fed, h), Bind is a no-op.
// Called from the federation manager's OnFederateJoined hook so the
// state exists by the time the JoinFederation RPC returns to the
// client.
//
// No reader is attached by Bind itself; Subscribe still has to fire
// for events to be drained off state.ch. While unread, the channel
// fills to bufferSize batches then overflows per the existing Send
// contract (ErrFederateOverflow) — bounded memory.
func (m *multiOutbox) Bind(fed core.FederationName, h core.FederateHandle) {
	key := fedHandleKey{fed: fed, h: h}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	current := *m.subs.Load()
	if _, exists := current[key]; exists {
		return
	}
	state := &multiRecipientState{
		ch:      make(chan []core.OutboundEvent, m.bufferSize),
		scratch: make([]core.OutboundEvent, 0, m.batchSize),
	}
	next := make(map[fedHandleKey]*multiRecipientState, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[key] = state
	m.subs.Store(&next)
}

// Unbind drops the per-(fed, h) recipient state without going through
// a Subscribe cancel func. M27 Phase A — wired from the federation
// manager's OnFederateResigned hook so a federate that joined but
// never opened its Events stream still has its state cleaned up.
//
// Idempotent. Safe even when a Subscribe is currently active — the
// cancel func from Subscribe stays in scope via the streamService
// loop and runs its own cleanup; this Unbind just unmaps the state
// from the table so a subsequent Bind for the same (fed, h) won't
// collide. The buffered channel is left to be garbage-collected by
// the still-running reader.
func (m *multiOutbox) Unbind(fed core.FederationName, h core.FederateHandle) {
	key := fedHandleKey{fed: fed, h: h}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	current := *m.subs.Load()
	if _, exists := current[key]; !exists {
		return
	}
	next := make(map[fedHandleKey]*multiRecipientState, len(current)-1)
	for k, v := range current {
		if k != key {
			next[k] = v
		}
	}
	m.subs.Store(&next)
}

// Subscribe implements grpc.SubscribableOutbox. Returns the read-side
// batch channel and a cancel func that unregisters the subscriber,
// performs a final flush of any pending scratch, and closes the
// channel.
//
// M27 Phase A: if Bind was called first for the (fed, h) pair, this
// attaches a reader to the pre-existing state and returns its
// channel — so any events sent during the post-join, pre-stream
// window are delivered. A second Subscribe call while a reader is
// already attached is rejected — duplicate readers would split the
// event stream.
//
// The ctx parameter is preserved for symmetry with the gRPC handler;
// cancellation of the subscription is via the returned cancel func, not
// ctx, because the lifetime is owned by the streamService loop, not by
// the request context.
func (m *multiOutbox) Subscribe(_ context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error) {
	key := fedHandleKey{fed: fed, h: h}
	m.writeMu.Lock()
	current := *m.subs.Load()
	state, exists := current[key]
	if exists {
		// Pre-bound state (or a leftover from a previous Subscribe
		// that hasn't called cancel yet). Reject duplicate readers.
		state.mu.Lock()
		if state.readerAttached {
			state.mu.Unlock()
			m.writeMu.Unlock()
			return nil, nil, fmt.Errorf("rtid: subscriber already registered for federation %q federate %d", fed, h)
		}
		state.readerAttached = true
		state.mu.Unlock()
		m.writeMu.Unlock()
	} else {
		// Backwards-compat path: tests / cmd/rtid pingpong / load tests
		// that don't wire the Bind hook still get an on-demand state.
		state = &multiRecipientState{
			ch:             make(chan []core.OutboundEvent, m.bufferSize),
			scratch:        make([]core.OutboundEvent, 0, m.batchSize),
			readerAttached: true,
		}
		next := make(map[fedHandleKey]*multiRecipientState, len(current)+1)
		for k, v := range current {
			next[k] = v
		}
		next[key] = state
		m.subs.Store(&next)
		m.writeMu.Unlock()
	}

	var cancelOnce sync.Once
	cancel := func() error {
		cancelOnce.Do(func() {
			m.writeMu.Lock()
			cur := *m.subs.Load()
			existing, ok := cur[key]
			if !ok || existing != state {
				m.writeMu.Unlock()
				return
			}
			next := make(map[fedHandleKey]*multiRecipientState, len(cur)-1)
			for k, v := range cur {
				if k != key {
					next[k] = v
				}
			}
			m.subs.Store(&next)
			m.writeMu.Unlock()

			// Final flush of any remaining scratch, then close. Done
			// after the table mutation so the table is unblocked even
			// if a slow receiver still holds the channel full.
			state.mu.Lock()
			state.readerAttached = false
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

// OutboxStats implements grpc.OutboxStatsSource — Phase 1 of the
// rtid-TUI plan (docs/rtid-tui.md). Returns one entry per active
// subscriber: the channel's queue depth + capacity + cumulative
// drops.
//
// The hot Send path is unaffected: we acquire no per-recipient mutex
// (the queue-depth read is len(channel), which Go documents as safe
// under concurrent send/receive); we read dropsTotal via
// atomic.Load. The subscribers table is loaded via the existing
// atomic.Pointer, so concurrent Subscribe/cancel sees a consistent
// in-flight snapshot.
func (m *multiOutbox) OutboxStats() []grpcsvc.OutboxStat {
	subs := *m.subs.Load()
	out := make([]grpcsvc.OutboxStat, 0, len(subs))
	for k, state := range subs {
		out = append(out, grpcsvc.OutboxStat{
			Federation: k.fed,
			Handle:     k.h,
			QueueDepth: uint32(len(state.ch)),
			Capacity:   uint32(cap(state.ch)),
			DropsTotal: atomic.LoadUint64(&state.dropsTotal),
		})
	}
	return out
}

// Compile-time assertion that multiOutbox implements core.Outbox. The
// SubscribableOutbox assertion is performed at runtime by streamService
// (see rti/internal/transport/grpc/stream.go); we cannot assert it here
// without an import cycle (cmd/rtid imports transport/grpc to wire the
// composed Server, and SubscribableOutbox lives in that package).
var _ core.Outbox = (*multiOutbox)(nil)
