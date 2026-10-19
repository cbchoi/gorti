// Timestamp-order delivery is controlled by time constraint, independently of
// asynchronous delivery. IEEE 1516.1-2010 8.1.7 permits asynchronous RO delivery;
// it does not permit a constrained federate to receive future TSO messages.

package time

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type tsoBufferReservationOf[T core.LogicalTimeScalar] struct {
	mu        sync.Mutex
	manager   *ManagerOf[T]
	fed       core.FederationName
	evalLock  *sync.Mutex
	buffered  []core.MessageDeliveryOf[T]
	immediate []core.MessageDeliveryOf[T]
	done      bool
}

func (r *tsoBufferReservationOf[T]) Immediate() []core.MessageDeliveryOf[T] {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.MessageDeliveryOf[T](nil), r.immediate...)
}

func (r *tsoBufferReservationOf[T]) Buffered() []core.MessageDeliveryOf[T] {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.MessageDeliveryOf[T](nil), r.buffered...)
}

func (r *tsoBufferReservationOf[T]) Commit(ctx context.Context) {
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		return
	}
	ext := extOfOf[T](r.manager)
	ext.mu.Lock()
	poke := false
	for _, delivery := range r.buffered {
		ns := ext.getOrCreateLocked(r.fed, delivery.Recipient)
		buffered := bufferedTSOEventOf[T]{
			timestamp:        delivery.Timestamp,
			event:            delivery.Event,
			sender:           delivery.Sender,
			retractionHandle: delivery.RetractionHandle,
			receiveOrder:     delivery.ReceiveOrder,
		}
		if delivery.ReceiveOrder {
			ns.roBuffer = append(ns.roBuffer, buffered)
		} else {
			ns.tsoBuffer = append(ns.tsoBuffer, buffered)
		}
		poke = poke || (ns.pendingNER && ns.mode.usesNextMessageTarget())
	}
	ext.mu.Unlock()
	r.manager.RecordRetractionDeliveries(r.fed, r.immediate)
	r.done = true
	r.evalLock.Unlock()
	r.mu.Unlock()
	if poke {
		_ = r.manager.tryGrantPending(ctx, r.fed)
	}
}

func (r *tsoBufferReservationOf[T]) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	r.evalLock.Unlock()
}

// bufferedTSOEvent holds one TSO event awaiting release. arrival order
// in the slice IS delivery order — when releaseBufferedTSO drains, it
// preserves FIFO.
type bufferedTSOEventOf[T core.LogicalTimeScalar] struct {
	timestamp T
	event     core.OutboundEvent
	// M20.2 — retraction tracking. ``sender`` + ``retractionHandle``
	// identify the originating send so RetractMessage can find and
	// drop this entry. Zero retractionHandle means "no retraction
	// available" (caller didn't supply a handle).
	sender           core.FederateHandle
	retractionHandle uint64
	receiveOrder     bool
}

// ShouldDeliverNow classifies timestamped messages by the recipient's time
// constraint. Regulation controls outgoing timestamps, not incoming delivery.
//
// Concurrency: lock order is stateStore.mu BEFORE nerStore.mu (see
// nerStore docs). Both are released before return.
func (m *ManagerOf[T]) ShouldDeliverNow(fed core.FederationName, h core.FederateHandle, ts T) bool {
	m.states.mu.Lock()
	fs := m.states.getLocked(fed, h)
	constrained := fs != nil && fs.constrained
	m.states.mu.Unlock()
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if barrier := ext.scheduledSaves[fed]; barrier != nil && (ts > barrier.target || scheduledRecipientPausedLocked(ext, fed, h)) {
		return false
	}
	// Constrained TSO is released by advance evaluation, not merely because
	// its timestamp equals the last grant. Asynchronous delivery concerns RO.
	return !constrained
}

// ReserveTSO takes the same per-federation evaluation boundary used by grant
// emission, then repeats delivery classification against the latest logical
// time. The lease remains held through the caller's WAL and outbox admission.
func (m *ManagerOf[T]) ReserveTSO(fed core.FederationName, deliveries []core.MessageDeliveryOf[T]) core.TSOBufferReservationOf[T] {
	return m.reserveTSO(fed, deliveries, true)
}

// ReserveMessagePreparation establishes evaluator -> service-state lock order
// for lifecycle transactions that discover recipients while preparing changes.
func (m *ManagerOf[T]) ReserveMessagePreparation(fed core.FederationName, prepare func() ([]core.MessageDeliveryOf[T], error)) (core.TSOBufferReservationOf[T], error) {
	lease := extOfOf[T](m).evaluatorLock(fed)
	lease.Lock()
	transferred := false
	defer func() {
		if !transferred {
			lease.Unlock()
		}
	}()
	deliveries, err := prepare()
	if err != nil {
		return nil, err
	}
	reservation := m.reserveTSOLocked(fed, deliveries, true, lease)
	transferred = true
	return reservation, nil
}

// ReserveOutgoingTSO retains the evaluator lease between sender validation and
// publication, so a concurrent grant or lookahead change cannot stale the check.
func (m *ManagerOf[T]) ReserveOutgoingTSO(fed core.FederationName, sender core.FederateHandle, timestamp T, deliveries []core.MessageDeliveryOf[T]) (core.TSOBufferReservationOf[T], error) {
	lease := extOfOf[T](m).evaluatorLock(fed)
	lease.Lock()
	if err := m.ValidateOutgoingTSO(fed, sender, timestamp); err != nil {
		lease.Unlock()
		return nil, err
	}
	return m.reserveTSOLocked(fed, deliveries, true, lease), nil
}

func (m *ManagerOf[T]) reserveTSO(
	fed core.FederationName,
	deliveries []core.MessageDeliveryOf[T],
	classifyTimeEngagement bool,
) core.TSOBufferReservationOf[T] {
	ext := extOfOf[T](m)
	evalLock := ext.evaluatorLock(fed)
	evalLock.Lock()
	return m.reserveTSOLocked(fed, deliveries, classifyTimeEngagement, evalLock)
}

func (m *ManagerOf[T]) reserveTSOLocked(fed core.FederationName, deliveries []core.MessageDeliveryOf[T], classifyTimeEngagement bool, evalLock *sync.Mutex) core.TSOBufferReservationOf[T] {
	ext := extOfOf[T](m)
	reservation := &tsoBufferReservationOf[T]{manager: m, fed: fed, evalLock: evalLock}
	// Match ShouldDeliverNow for every recipient while the evaluator lease
	// prevents a grant from overtaking this classification. Only constrained
	// recipients use incoming timestamp order.
	m.states.mu.Lock()
	for _, delivery := range deliveries {
		fs := m.states.getLocked(fed, delivery.Recipient)
		timeEngaged := true
		if classifyTimeEngagement {
			timeEngaged = fs != nil && fs.constrained
		}
		ext.mu.Lock()
		ns := ext.getLocked(fed, delivery.Recipient)
		deliverNow := classifyImmediateDelivery(ext, fed, fs, ns, delivery, timeEngaged)
		ext.mu.Unlock()
		if event, ok := delivery.Event.(core.ReceivedOrderEvent); ok {
			order := core.OrderReceive
			if !delivery.ReceiveOrder && fs != nil && fs.constrained {
				order = core.OrderTimestamp
			}
			event.SetReceivedOrder(order)
		}
		if deliverNow {
			reservation.immediate = append(reservation.immediate, delivery)
		} else {
			reservation.buffered = append(reservation.buffered, delivery)
		}
	}
	m.states.mu.Unlock()
	return reservation
}

// BufferTSO implements core.TSODeliveryGate. Enqueues an event whose
// ShouldDeliverNow returned false. The buffer is per-federate, FIFO,
// unbounded (M22 ships without a cap; bound is an M23 follow-up).
//
// Concurrency: takes nerStore.mu briefly to append. The send path that
// consults the gate has already finished its outbox lookup; this call
// just stores. Release happens later via releaseBufferedTSO (called
// from emitGrant or when time constraint is disabled).
//
// M38 GA — §8.8 arrival poke: a newly buffered TSO message may be
// exactly the "next message" a pending NMR/NMRA is waiting on (its
// timestamp can pull the grant target down below LBTS), so buffering
// re-evaluates the federation's pending grants. Best-effort like the
// M36 DB-2 resign re-run: the buffering itself already succeeded, and
// grant-emission failures surface through the stream layer.
func (m *ManagerOf[T]) BufferTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle, ts T, evt core.OutboundEvent) error {
	reservation := m.reserveTSO(
		fed,
		[]core.MessageDeliveryOf[T]{{Recipient: h, Timestamp: ts, Event: evt}},
		false,
	)
	if len(reservation.Immediate()) > 0 {
		if err := m.opts.Outbox.Send(ctx, fed, h, evt); err != nil {
			reservation.Release()
			return err
		}
	}
	reservation.Commit(ctx)
	return nil
}

// BufferTSOWithRetraction is BufferTSO + retraction-handle tracking.
// M20.2 — see core.TSODeliveryGate interface comment. Carries the same
// M38 GA §8.8 arrival poke as BufferTSO.
func (m *ManagerOf[T]) BufferTSOWithRetraction(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	ts T,
	evt core.OutboundEvent,
	sender core.FederateHandle,
	retractionHandle uint64,
) error {
	reservation := m.reserveTSO(
		fed,
		[]core.MessageDeliveryOf[T]{{
			Recipient: h, Timestamp: ts, Event: evt, Sender: sender, RetractionHandle: retractionHandle,
		}},
		false,
	)
	if len(reservation.Immediate()) > 0 {
		if err := m.opts.Outbox.Send(ctx, fed, h, evt); err != nil {
			reservation.Release()
			return err
		}
	}
	reservation.Commit(ctx)
	return nil
}

// RetractMessage walks every recipient federate's buffer in fed and
// removes any bufferedTSOEvent matching (sender, retractionHandle).
// Returns the count removed. M20.2 (IEEE 1516.1 §8.21).
//
// Zero retractionHandle never matches — the caller didn't supply a
// retraction handle on the original send.
func (m *ManagerOf[T]) RetractMessage(
	fed core.FederationName,
	sender core.FederateHandle,
	retractionHandle uint64,
) int {
	if retractionHandle == 0 {
		return 0
	}
	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	removed := 0
	for key, ns := range ext.states {
		if key.fed != fed {
			continue
		}
		filtered := ns.tsoBuffer[:0]
		for _, b := range ns.tsoBuffer {
			if b.sender == sender && b.retractionHandle == retractionHandle {
				removed++
				continue
			}
			filtered = append(filtered, b)
		}
		// Same backing-array trick as releaseBufferedTSO: keep the
		// filtered slice but allocate a fresh array so we don't pin
		// the old capacity.
		ns.tsoBuffer = append([]bufferedTSOEventOf[T](nil), filtered...)
	}
	return removed
}

// releaseBufferedTSO drains any buffered TSO events with timestamp <= t
// for the given federate, sending them through the manager's Outbox in
// FIFO order. Called from emitGrant BEFORE the grant is sent (M37 EB-2,
// §8.14: the federate must hold all TSO <= grant-time before the grant
// fires). The lock is held only for the slice copy + buffer truncation;
// Outbox.Send happens outside the lock to avoid blocking the wire path.
//
// A failed delivery restores the remaining events and prevents grant emission.
func (m *ManagerOf[T]) takeBufferedTSO(fed core.FederationName, h core.FederateHandle, t *T, advancing bool) []bufferedTSOEventOf[T] {
	return m.takeBufferedTSOThrough(fed, h, t, advancing, t)
}

// Flush may deliver speculative user callbacks, but cannot move the sender's
// internal deletion deadline beyond the time actually granted.
func (m *ManagerOf[T]) takeBufferedTSOThrough(fed core.FederationName, h core.FederateHandle, t *T, advancing bool, internalDeadline *T) []bufferedTSOEventOf[T] {
	m.states.mu.Lock()
	state := m.states.getLocked(fed, h)
	constrained := state != nil && state.constrained
	m.states.mu.Unlock()
	ext := extOfOf[T](m)
	ext.mu.Lock()
	ns := ext.getLocked(fed, h)
	if ns == nil || len(ns.tsoBuffer) == 0 || scheduledRecipientPausedLocked(ext, fed, h) {
		ext.mu.Unlock()
		return nil
	}
	// Partition eligible events in FIFO order. A nil bound drains all.
	order := core.OrderReceive
	if constrained {
		order = core.OrderTimestamp
	}
	keep := ns.tsoBuffer[:0]
	var release []bufferedTSOEventOf[T]
	for _, b := range ns.tsoBuffer {
		eligible := bufferedTSOEligible(b, t, advancing, internalDeadline, ns.currentTime)
		if eligible {
			release = append(release, b)
		} else {
			keep = append(keep, b)
		}
	}
	// keep aliased ns.tsoBuffer's backing array; reassign explicitly.
	ns.tsoBuffer = append([]bufferedTSOEventOf[T](nil), keep...)
	ext.mu.Unlock()
	if order == core.OrderTimestamp {
		slices.SortStableFunc(release, func(a, b bufferedTSOEventOf[T]) int {
			if a.timestamp < b.timestamp {
				return -1
			}
			if a.timestamp > b.timestamp {
				return 1
			}
			return 0
		})
		orderReflectionDeletionTies(release)
	}
	// Time constraint may have changed since this callback was buffered.
	for _, buffered := range release {
		if event, ok := buffered.event.(core.ReceivedOrderEvent); ok {
			event.SetReceivedOrder(order)
		}
	}
	return release
}

func (m *ManagerOf[T]) restoreBufferedTSO(fed core.FederationName, h core.FederateHandle, events []bufferedTSOEventOf[T]) {
	if len(events) == 0 {
		return
	}
	ext := extOfOf[T](m)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	restored := make([]bufferedTSOEventOf[T], 0, len(events)+len(ns.tsoBuffer))
	restored = append(restored, events...)
	restored = append(restored, ns.tsoBuffer...)
	ns.tsoBuffer = restored
	ext.mu.Unlock()
}

func (m *ManagerOf[T]) releaseBufferedTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T) error {
	release := m.takeBufferedTSO(fed, h, &t, true)
	for i, b := range release {
		if err := m.deliverBufferedEvent(ctx, fed, h, b); err != nil {
			m.restoreBufferedTSO(fed, h, release[i:])
			return err
		}
	}
	return nil
}

// drainAllBufferedTSO releases queued messages as RO only for an unconstrained
// recipient. Scheduled saves and internal deletion deadlines still apply.
func (m *ManagerOf[T]) drainAllBufferedTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	m.states.mu.Lock()
	state := m.states.getLocked(fed, h)
	constrained := state != nil && state.constrained
	m.states.mu.Unlock()
	if constrained {
		return nil
	}
	ext := extOfOf[T](m)
	ext.mu.Lock()
	var bound *T
	if barrier := ext.scheduledSaves[fed]; barrier != nil {
		target := barrier.target
		bound = &target
	}
	ext.mu.Unlock()
	release := append(m.takeBufferedRO(fed, h), m.takeBufferedTSO(fed, h, bound, false)...)
	if len(release) == 0 {
		return nil
	}
	// Reserve the entire production drain before publishing any callback or
	// retiring objects, so a failed disable can be retried without partial delivery.
	if _, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
		return m.drainReservedTSO(ctx, fed, h, release)
	}
	for i, b := range release {
		if event, ok := b.event.(core.ReceivedOrderEvent); ok {
			event.SetReceivedOrder(core.OrderReceive)
		}
		if err := m.deliverBufferedEvent(ctx, fed, h, b); err != nil {
			m.restoreBufferedMessages(fed, h, release[i:])
			return err
		}
	}
	return nil
}

// EnableAsynchronousDelivery implements core.TimeManager (M22).
//
// The asynchronous flag affects RO delivery, never the TSO timestamp gate.
//
// Errors:
//   - core.ErrFederationHalted if the federation is halted
//   - core.ErrTimeAlreadyAsynchronous if already async
func (m *ManagerOf[T]) EnableAsynchronousDelivery(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	if extOfOf[T](m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	ext := extOfOf[T](m)
	evalLock := ext.evaluatorLock(fed)
	evalLock.Lock()
	defer func() { evalLock.Unlock(); m.finishDeletionDelivery(fed) }()
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	if ns.asyncDelivery {
		ext.mu.Unlock()
		return core.ErrTimeAlreadyAsynchronous
	}
	ns.asyncDelivery = true
	ext.mu.Unlock()
	if err := m.drainBufferedRO(ctx, fed, h); err != nil {
		if errors.Is(err, core.ErrFederateOverflow) {
			m.markDeliveryRetry(fed)
			return nil
		}
		ext.mu.Lock()
		ns.asyncDelivery = false
		ext.mu.Unlock()
		return err
	}
	return nil
}

// DisableAsynchronousDelivery implements core.TimeManager (M22).
//
// Disabling asynchronous delivery does not recall already-delivered messages.
//
// Errors:
//   - core.ErrFederationHalted if the federation is halted
//   - core.ErrTimeNotAsynchronous if already off
func (m *ManagerOf[T]) DisableAsynchronousDelivery(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
	if extOfOf[T](m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	ext := extOfOf[T](m)
	evalLock := ext.evaluatorLock(fed)
	evalLock.Lock()
	defer evalLock.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getOrCreateLocked(fed, h)
	if !ns.asyncDelivery {
		return core.ErrTimeNotAsynchronous
	}
	ns.asyncDelivery = false
	return nil
}

// classifyImmediateDelivery requires both time-state locks and the evaluator lease.
func classifyImmediateDelivery[T core.LogicalTimeScalar](ext *nerStoreOf[T], fed core.FederationName, fs *federateStateOf[T], ns *nerStateOf[T], delivery core.MessageDeliveryOf[T], timeEngaged bool) bool {
	current := T(0)
	deliverNow := !timeEngaged
	if ns != nil {
		current = ns.currentTime
	}
	if delivery.ReceiveOrder {
		deliverNow = fs == nil || !fs.constrained || (ns != nil && (ns.pendingNER || ns.asyncDelivery))
	}
	if internalTSO(delivery.Event) {
		deliverNow = delivery.Timestamp <= current
	}
	if barrier := ext.scheduledSaves[fed]; !delivery.ReceiveOrder && barrier != nil && delivery.Timestamp > barrier.target {
		deliverNow = false
	}
	if scheduledRecipientPausedLocked(ext, fed, delivery.Recipient) {
		deliverNow = false
	}
	return deliverNow
}

func bufferedTSOEligible[T core.LogicalTimeScalar](b bufferedTSOEventOf[T], t *T, advancing bool, internalDeadline *T, currentTime T) bool {
	eligible := t == nil || b.timestamp <= *t
	if internalTSO(b.event) && internalDeadline != nil {
		eligible = eligible && b.timestamp <= *internalDeadline
	}
	if !advancing && internalTSO(b.event) {
		eligible = eligible && b.timestamp <= currentTime
	}
	return eligible
}

func (m *ManagerOf[T]) drainReservedTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle, release []bufferedTSOEventOf[T]) error {
	deliveries := make([]core.OutboxDelivery, 0, len(release))
	for _, b := range release {
		if !internalTSO(b.event) {
			if event := m.queuedMessage(ctx, fed, h, b); event != nil {
				deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: event})
			}
		}
	}
	var reservation core.OutboxReservation
	if len(deliveries) > 0 || len(release) > 0 {
		var err error
		reservation, err = m.reserveBufferedDeliveries(ctx, fed, h, release, deliveries)
		if errors.Is(err, core.ErrFederateOverflow) {
			if reservation != nil {
				reservation.Release()
			}
			return m.deliverBufferedSequence(ctx, fed, h, release)
		}
		if reservation != nil {
			defer reservation.Release()
		}
		if err != nil {
			m.restoreBufferedMessages(fed, h, release)
			return err
		}
	}
	lifecycle := m.prepareDeletionDelivery(fed, h, release)
	if lifecycle != nil {
		defer lifecycle.Release()
	}
	if reservation != nil {
		if err := reservation.Commit(); err != nil {
			m.restoreBufferedMessages(fed, h, release)
			return err
		}
	}
	m.recordBufferedRetractionDeliveries(ctx, fed, h, release)
	if lifecycle != nil {
		return lifecycle.Commit()
	}
	return nil
}
