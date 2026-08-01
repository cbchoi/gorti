package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// defaultMultiBatchSize is how many events Send accumulates per
// recipient before flushing the scratch slice to the recipient's
// channel. Mirrors rti/internal/perf.defaultPerfBatchSize after the
// microbench sweep that picked 32 as the throughput plateau on the
// in-process harness; the production gRPC streaming loop is the
// downstream consumer and benefits identically from amortized
// channel ops.
const (
	defaultMultiEventCapacity = 8192
	maxMultiEventCapacity     = 1 << 20
	defaultMultiBatchSize     = 32
	maxMultiBatchSize         = 1024
)

// defaultMultiFlushInterval bounds the time an event may sit in a
// per-recipient scratch slice before being flushed to the channel.
// Without this bound a low-rate sender (1 event/sec) would wait
// batchSize seconds to be visible to the receiver. The interval is
// short enough that the wire-visible latency is dominated by the
// channel send + gRPC frame cost, not the batching delay.
const defaultMultiFlushInterval = 1 * time.Millisecond

// resolveMultiOutboxConfig applies production defaults to zero-valued
// internal configuration and rejects values that cannot be used safely.
// The explicit constructor remains permissive because tests use a zero
// flush interval to disable its timer.
func resolveMultiOutboxConfig(batchSize int, flushInterval time.Duration) (int, time.Duration, error) {
	if batchSize == 0 {
		batchSize = defaultMultiBatchSize
	}
	if flushInterval == 0 {
		flushInterval = defaultMultiFlushInterval
	}
	if batchSize < 1 || batchSize > maxMultiBatchSize {
		return 0, 0, fmt.Errorf("outbox batch size must be between 1 and %d (got %d)", maxMultiBatchSize, batchSize)
	}
	if flushInterval <= 0 {
		return 0, 0, fmt.Errorf("outbox flush interval must be greater than zero (got %s)", flushInterval)
	}
	return batchSize, flushInterval, nil
}

func resolveMultiOutboxCapacity(eventCapacity int) (int, error) {
	if eventCapacity == 0 {
		return defaultMultiEventCapacity, nil
	}
	if eventCapacity < 1 || eventCapacity > maxMultiEventCapacity {
		return 0, fmt.Errorf(
			"outbox event capacity must be between 1 and %d (got %d)",
			maxMultiEventCapacity,
			eventCapacity,
		)
	}
	return eventCapacity, nil
}

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
	closed  bool
	// flushTimer is the PERSISTENT deferred-flush timer (W6d): created
	// lazily by the first arm and re-armed with Reset thereafter, so the
	// steady state allocates no per-batch timer or closure. nil until the
	// first arm; never cleared afterwards (Stop leaves it reusable).
	flushTimer *time.Timer
	// flushArmed is true while a deferred flush is wanted. Guarded by mu.
	flushArmed bool
	// flushGen replaces the retired per-arm token-identity staleness
	// check (W6d): it is bumped under mu on every arm, disarm, and
	// consumed fire. Because timer.Reset does NOT await an in-flight
	// callback, a fire can be dequeued for an arm that has since been
	// disarmed; the callback snapshots flushGen before locking and
	// re-validates it (plus flushArmed) under mu, discarding fires whose
	// arm was superseded in the window it observed. The one undetectable
	// interleaving — disarm + re-arm BOTH completing before the stale
	// callback's snapshot — flushes the new arm's scratch early, which is
	// benign: the flush runs under mu with the identical channel handoff
	// a fresh fire would perform, order is preserved, and the
	// re-scheduled fire then no-ops on the bumped generation.
	flushGen atomic.Uint64
	// dropsTotal counts pending events that cannot be flushed during
	// recipient shutdown. Live channel saturation is backpressured or
	// explicitly rejected; timer flush does not discard accepted events.
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
// to the pre-batching design; production callers pass values resolved
// and validated by resolveMultiOutboxConfig.
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
// A missing or closed recipient returns ErrOutboxUnavailable. Bind is called
// during join so the normal pre-stream startup window has bounded state.
func (m *multiOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	subs := *m.subs.Load()
	state, ok := subs[fedHandleKey{fed: fed, h: h}]
	if !ok {
		return fmt.Errorf("%w: federation %q federate %d", core.ErrOutboxUnavailable, fed, h)
	}
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return fmt.Errorf("%w: federation %q federate %d", core.ErrOutboxUnavailable, fed, h)
	}
	if m.availableLocked(state) < 1 {
		state.mu.Unlock()
		return fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, fed, h)
	}
	wasEmpty := len(state.scratch) == 0
	state.scratch = append(state.scratch, evt)
	_, isTimeAdvanceGrant := evt.(*timepkg.TimeAdvanceGrant)
	if len(state.scratch) < m.batchSize && !isTimeAdvanceGrant {
		// Arm the deferred-flush timer when the first event lands in
		// an empty scratch. It fires after flushInterval to bound the
		// time low-rate events spend waiting for batchSize to fill.
		if wasEmpty {
			m.armFlushLocked(state)
		}
		state.mu.Unlock()
		return nil
	}
	select {
	case state.ch <- state.scratch:
		state.scratch = make([]core.OutboundEvent, 0, m.batchSize)
		m.disarmFlushLocked(state)
		state.mu.Unlock()
		return nil
	default:
		// The final element belongs to the current call and is rejected.
		// Earlier elements were accepted by previous calls and remain owned.
		state.scratch = state.scratch[:len(state.scratch)-1]
		m.armFlushLocked(state)
		state.mu.Unlock()
		return fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, fed, h)
	}
}

// availableLocked returns conservative capacity in event units. Every queued
// channel batch is charged as a full batch, so a short timer/grant batch can
// reduce utilization but can never cause reservation overcommit.
func (m *multiOutbox) availableLocked(state *multiRecipientState) int {
	capacity := cap(state.ch)*m.batchSize + (m.batchSize - 1)
	used := len(state.ch)*m.batchSize + len(state.scratch)
	return capacity - used
}

type multiOutboxReservation struct {
	mu         sync.Mutex
	m          *multiOutbox
	deliveries []core.OutboxDelivery
	states     []*multiRecipientState
	unique     []*multiRecipientState
	done       bool
}

type singleOutboxReservation struct {
	mu       sync.Mutex
	m        *multiOutbox
	delivery core.OutboxDelivery
	state    *multiRecipientState
	done     bool
}

// Reserve locks recipient states in handle order and holds those locks until
// Commit or Release. This is deliberately a short transaction spanning the
// caller's write-ahead append: cancel/unbind and unrelated sends cannot consume
// or invalidate capacity after admission succeeds.
func (m *multiOutbox) Reserve(_ context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if len(deliveries) == 0 {
		return &multiOutboxReservation{m: m}, nil
	}
	if len(deliveries) == 1 {
		return m.reserveSingle(fed, deliveries[0])
	}
	recipients := make(map[core.FederateHandle]struct{}, len(deliveries))
	for _, delivery := range deliveries {
		recipients[delivery.Recipient] = struct{}{}
	}
	handles := make([]core.FederateHandle, 0, len(recipients))
	for h := range recipients {
		handles = append(handles, h)
	}
	sort.Slice(handles, func(i, j int) bool { return handles[i] < handles[j] })

	subs := *m.subs.Load()
	byHandle := make(map[core.FederateHandle]*multiRecipientState, len(handles))
	unique := make([]*multiRecipientState, 0, len(handles))
	for _, h := range handles {
		state, ok := subs[fedHandleKey{fed: fed, h: h}]
		if !ok {
			for i := len(unique) - 1; i >= 0; i-- {
				unique[i].mu.Unlock()
			}
			return nil, fmt.Errorf("%w: federation %q federate %d", core.ErrOutboxUnavailable, fed, h)
		}
		state.mu.Lock()
		unique = append(unique, state)
		byHandle[h] = state
		if state.closed {
			for i := len(unique) - 1; i >= 0; i-- {
				unique[i].mu.Unlock()
			}
			return nil, fmt.Errorf("%w: federation %q federate %d", core.ErrOutboxUnavailable, fed, h)
		}
	}

	// Simulate exact batching while the recipient locks exclude sends and
	// timer flushes. Grants force short batches, so event-count arithmetic is
	// insufficient for proving that Commit cannot overflow.
	states := make([]*multiRecipientState, len(deliveries))
	queued := make(map[*multiRecipientState]int, len(unique))
	scratch := make(map[*multiRecipientState]int, len(unique))
	for _, state := range unique {
		queued[state] = len(state.ch)
		scratch[state] = len(state.scratch)
	}
	for i, delivery := range deliveries {
		state := byHandle[delivery.Recipient]
		states[i] = state
		scratch[state]++
		_, isGrant := delivery.Event.(*timepkg.TimeAdvanceGrant)
		if scratch[state] < m.batchSize && !isGrant {
			continue
		}
		if queued[state] >= cap(state.ch) {
			for j := len(unique) - 1; j >= 0; j-- {
				unique[j].mu.Unlock()
			}
			return nil, fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, fed, delivery.Recipient)
		}
		queued[state]++
		scratch[state] = 0
	}
	owned := append([]core.OutboxDelivery(nil), deliveries...)
	return &multiOutboxReservation{m: m, deliveries: owned, states: states, unique: unique}, nil
}

func (m *multiOutbox) reserveSingle(
	fed core.FederationName,
	delivery core.OutboxDelivery,
) (core.OutboxReservation, error) {
	subs := *m.subs.Load()
	state, ok := subs[fedHandleKey{fed: fed, h: delivery.Recipient}]
	if !ok {
		return nil, fmt.Errorf("%w: federation %q federate %d", core.ErrOutboxUnavailable, fed, delivery.Recipient)
	}
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return nil, fmt.Errorf("%w: federation %q federate %d", core.ErrOutboxUnavailable, fed, delivery.Recipient)
	}
	_, isGrant := delivery.Event.(*timepkg.TimeAdvanceGrant)
	flushes := len(state.scratch)+1 >= m.batchSize || isGrant
	if flushes && len(state.ch) >= cap(state.ch) {
		state.mu.Unlock()
		return nil, fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, fed, delivery.Recipient)
	}
	return &singleOutboxReservation{
		m:        m,
		delivery: delivery,
		state:    state,
	}, nil
}

func (r *multiOutboxReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	for i, delivery := range r.deliveries {
		if err := commitOutboxDeliveryLocked(r.m, r.states[i], delivery); err != nil {
			r.finishLocked()
			return err
		}
	}
	r.finishLocked()
	return nil
}

func commitOutboxDeliveryLocked(
	m *multiOutbox,
	state *multiRecipientState,
	delivery core.OutboxDelivery,
) error {
	wasEmpty := len(state.scratch) == 0
	state.scratch = append(state.scratch, delivery.Event)
	_, isGrant := delivery.Event.(*timepkg.TimeAdvanceGrant)
	if len(state.scratch) < m.batchSize && !isGrant {
		if wasEmpty {
			m.armFlushLocked(state)
		}
		return nil
	}
	select {
	case state.ch <- state.scratch:
		state.scratch = make([]core.OutboundEvent, 0, m.batchSize)
		m.disarmFlushLocked(state)
		return nil
	default:
		// Reserve's capacity calculation makes this unreachable unless an
		// implementation invariant is broken. Keep the event owned in
		// scratch and report the invariant failure without dropping it.
		return fmt.Errorf("outbox reservation commit: %w", core.ErrFederateOverflow)
	}
}

func (r *multiOutboxReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.finishLocked()
	}
}

func (r *multiOutboxReservation) finishLocked() {
	if r.done {
		return
	}
	for i := len(r.unique) - 1; i >= 0; i-- {
		r.unique[i].mu.Unlock()
	}
	r.done = true
}

func (r *singleOutboxReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	err := commitOutboxDeliveryLocked(r.m, r.state, r.delivery)
	r.state.mu.Unlock()
	r.done = true
	return err
}

func (r *singleOutboxReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.state.mu.Unlock()
	r.done = true
}

// armFlushLocked and flushScratch retain accepted events in scratch while the
// bounded recipient channel is full. The timer retries without blocking its
// goroutine or detaching the batch from recipient ordering.
//
// W6d timer mechanics (interval semantics unchanged — the contract-fixed
// 1ms default is untouched): the recipient owns ONE persistent
// *time.Timer, created by the first arm and re-armed with Reset. Stale
// fires are rejected by the flushGen generation counter — see the field
// comment on multiRecipientState.
func (m *multiOutbox) armFlushLocked(state *multiRecipientState) {
	if state.closed || len(state.scratch) == 0 || m.flushInterval <= 0 || state.flushArmed {
		return
	}
	state.flushArmed = true
	state.flushGen.Add(1)
	if state.flushTimer == nil {
		state.flushTimer = time.AfterFunc(m.flushInterval, func() {
			// Snapshot the generation BEFORE taking the lock: any arm or
			// disarm that lands between this load and the lock acquire
			// bumps flushGen and invalidates the fire.
			m.flushScratch(state, state.flushGen.Load())
		})
		return
	}
	state.flushTimer.Reset(m.flushInterval)
}

// disarmFlushLocked cancels a pending deferred flush. Reset/Stop never
// await an in-flight callback, so the generation bump — not the Stop —
// is what invalidates a fire that has already been dequeued.
func (m *multiOutbox) disarmFlushLocked(state *multiRecipientState) {
	if !state.flushArmed {
		return
	}
	state.flushArmed = false
	state.flushGen.Add(1)
	state.flushTimer.Stop()
}

func (m *multiOutbox) flushScratch(state *multiRecipientState, gen uint64) {
	state.mu.Lock()
	if state.closed || !state.flushArmed || state.flushGen.Load() != gen {
		state.mu.Unlock()
		return
	}
	state.flushArmed = false
	state.flushGen.Add(1)
	if len(state.scratch) == 0 {
		state.mu.Unlock()
		return
	}
	select {
	case state.ch <- state.scratch:
		state.scratch = make([]core.OutboundEvent, 0, m.batchSize)
	default:
		m.armFlushLocked(state)
	}
	state.mu.Unlock()
}

func (m *multiOutbox) closeRecipientState(state *multiRecipientState, flush bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return
	}
	state.closed = true
	state.readerAttached = false
	m.disarmFlushLocked(state)
	if flush && len(state.scratch) > 0 {
		select {
		case state.ch <- state.scratch:
		default:
			atomic.AddUint64(&state.dropsTotal, uint64(len(state.scratch)))
		}
	}
	state.scratch = nil
	close(state.ch)
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
// Idempotent. Safe when Subscribe is active: the state is unmapped and its
// channel is closed under the recipient lock, terminating the reader. The
// stream's later cancel call sees that the table entry is already gone.
func (m *multiOutbox) Unbind(fed core.FederationName, h core.FederateHandle) {
	key := fedHandleKey{fed: fed, h: h}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	current := *m.subs.Load()
	state, exists := current[key]
	if !exists {
		return
	}
	next := make(map[fedHandleKey]*multiRecipientState, len(current)-1)
	for k, v := range current {
		if k != key {
			next[k] = v
		}
	}
	m.subs.Store(&next)
	m.closeRecipientState(state, false)
}

// UnbindFederation removes every recipient binding owned by fed, stops pending
// timers, and closes active recipient channels.
func (m *multiOutbox) UnbindFederation(fed core.FederationName) {
	m.writeMu.Lock()
	current := *m.subs.Load()
	next := make(map[fedHandleKey]*multiRecipientState, len(current))
	removed := make([]*multiRecipientState, 0)
	for key, state := range current {
		if key.fed == fed {
			removed = append(removed, state)
			continue
		}
		next[key] = state
	}
	m.subs.Store(&next)
	m.writeMu.Unlock()
	for _, state := range removed {
		m.closeRecipientState(state, false)
	}
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
			m.closeRecipientState(state, true)
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
var _ core.ReservableOutbox = (*multiOutbox)(nil)
