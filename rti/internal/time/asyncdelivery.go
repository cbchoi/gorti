// Package time — async-delivery (M22 W2 / TASK-228..233).
//
// Implements the IEEE 1516.1 §8.16-8.17 "asynchronous delivery" toggle
// + the underlying TSO delivery gate. With async OFF (the spec default
// and the M22 default), TSO messages with timestamp t are buffered
// server-side until the federate's logical time advances past t. With
// async ON (gorti's pre-M22 behavior), TSO messages are delivered
// immediately.
//
// The gate is consumed by object.Registry's update + interaction paths
// (TASK-237) via the core.TSODeliveryGate interface (TASK-228); the
// Manager satisfies that interface.

package time

import (
	"context"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

type tsoBufferReservation struct {
	mu        sync.Mutex
	manager   *Manager
	fed       core.FederationName
	evalLock  *sync.Mutex
	buffered  []core.TSOBufferedDelivery
	immediate []core.TSOBufferedDelivery
	done      bool
}

func (r *tsoBufferReservation) Immediate() []core.TSOBufferedDelivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.TSOBufferedDelivery(nil), r.immediate...)
}

func (r *tsoBufferReservation) Buffered() []core.TSOBufferedDelivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.TSOBufferedDelivery(nil), r.buffered...)
}

func (r *tsoBufferReservation) Commit(ctx context.Context) {
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		return
	}
	ext := extOf(r.manager)
	ext.mu.Lock()
	poke := false
	for _, delivery := range r.buffered {
		ns := ext.getOrCreateLocked(r.fed, delivery.Recipient)
		ns.tsoBuffer = append(ns.tsoBuffer, bufferedTSOEvent{
			timestamp:        delivery.Timestamp,
			event:            delivery.Event,
			sender:           delivery.Sender,
			retractionHandle: delivery.RetractionHandle,
		})
		poke = poke || (ns.pendingNER && ns.mode.usesNextMessageTarget())
	}
	ext.mu.Unlock()
	r.done = true
	r.evalLock.Unlock()
	r.mu.Unlock()
	if poke {
		_ = r.manager.tryGrantPending(ctx, r.fed)
	}
}

func (r *tsoBufferReservation) Release() {
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
type bufferedTSOEvent struct {
	timestamp core.LogicalTime
	event     core.OutboundEvent
	// M20.2 — retraction tracking. ``sender`` + ``retractionHandle``
	// identify the originating send so RetractMessage can find and
	// drop this entry. Zero retractionHandle means "no retraction
	// available" (caller didn't supply a handle).
	sender           core.FederateHandle
	retractionHandle uint64
}

// ShouldDeliverNow implements core.TSODeliveryGate. Returns true when
// the federate's async delivery is on OR when the event's timestamp
// is at-or-before the federate's currentTime. Otherwise the event
// must be buffered via BufferTSO.
//
// Per IEEE 1516.1 §8.16-8.17, asynchronous delivery governs TSO
// delivery only for federates that participate in time coordination
// (regulating or constrained). A federate that has never enabled
// either has no logical-time progression, so TSO ordering is
// meaningless and events deliver immediately. M25 Phase A: this
// removes the M22 "all federates must opt-in to async on join" trap
// that silently dropped TSO events for non-time-aware subscribers
// (regression in M5 verbose-mode test).
//
// For federates that ARE time-engaged, asyncDelivery starts OFF (the
// IEEE 1516.1 §8.17 default); events at-or-before currentTime
// deliver, later events buffer until grant.
//
// Concurrency: lock order is stateStore.mu BEFORE nerStore.mu (see
// nerStore docs). Both are released before return.
func (m *Manager) ShouldDeliverNow(fed core.FederationName, h core.FederateHandle, ts core.LogicalTime) bool {
	m.states.mu.Lock()
	fs := m.states.getLocked(fed, h)
	timeEngaged := fs != nil && (fs.regulating || fs.constrained)
	m.states.mu.Unlock()
	if !timeEngaged {
		return true
	}
	ext := extOf(m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getLocked(fed, h)
	if ns == nil {
		// Time-engaged federate but no advance-request state yet:
		// per §8.17 default, currentTime=0 + asyncDelivery=false.
		return float64(ts) <= 0
	}
	if ns.asyncDelivery {
		return true
	}
	return float64(ts) <= float64(ns.currentTime)
}

// ReserveTSO takes the same per-federation evaluation boundary used by grant
// emission, then repeats delivery classification against the latest logical
// time. The lease remains held through the caller's WAL and outbox admission.
func (m *Manager) ReserveTSO(fed core.FederationName, deliveries []core.TSOBufferedDelivery) core.TSOBufferReservation {
	return m.reserveTSO(fed, deliveries, true)
}

func (m *Manager) reserveTSO(
	fed core.FederationName,
	deliveries []core.TSOBufferedDelivery,
	classifyTimeEngagement bool,
) core.TSOBufferReservation {
	ext := extOf(m)
	evalLock := ext.evaluatorLock(fed)
	evalLock.Lock()
	reservation := &tsoBufferReservation{manager: m, fed: fed, evalLock: evalLock}
	// Match ShouldDeliverNow for every recipient while the evaluator lease
	// prevents a grant from overtaking this classification. Non-time-engaged
	// federates receive TSO immediately; time-engaged federates use their
	// asynchronous-delivery flag and current logical time.
	if classifyTimeEngagement {
		m.states.mu.Lock()
	}
	for _, delivery := range deliveries {
		timeEngaged := true
		if classifyTimeEngagement {
			fs := m.states.getLocked(fed, delivery.Recipient)
			timeEngaged = fs != nil && (fs.regulating || fs.constrained)
		}
		ext.mu.Lock()
		ns := ext.getLocked(fed, delivery.Recipient)
		current := core.LogicalTime(0)
		deliverNow := !timeEngaged
		if ns != nil {
			current = ns.currentTime
			deliverNow = deliverNow || ns.asyncDelivery
		}
		if timeEngaged {
			deliverNow = deliverNow || float64(delivery.Timestamp) <= float64(current)
		}
		ext.mu.Unlock()
		if deliverNow {
			reservation.immediate = append(reservation.immediate, delivery)
		} else {
			reservation.buffered = append(reservation.buffered, delivery)
		}
	}
	if classifyTimeEngagement {
		m.states.mu.Unlock()
	}
	return reservation
}

// BufferTSO implements core.TSODeliveryGate. Enqueues an event whose
// ShouldDeliverNow returned false. The buffer is per-federate, FIFO,
// unbounded (M22 ships without a cap; bound is an M23 follow-up).
//
// Concurrency: takes nerStore.mu briefly to append. The send path that
// consults the gate has already finished its outbox lookup; this call
// just stores. Release happens later via releaseBufferedTSO (called
// from emitGrant or EnableAsynchronousDelivery).
//
// M38 GA — §8.8 arrival poke: a newly buffered TSO message may be
// exactly the "next message" a pending NMR/NMRA is waiting on (its
// timestamp can pull the grant target down below LBTS), so buffering
// re-evaluates the federation's pending grants. Best-effort like the
// M36 DB-2 resign re-run: the buffering itself already succeeded, and
// grant-emission failures surface through the stream layer.
func (m *Manager) BufferTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle, ts core.LogicalTime, evt core.OutboundEvent) error {
	reservation := m.reserveTSO(
		fed,
		[]core.TSOBufferedDelivery{{Recipient: h, Timestamp: ts, Event: evt}},
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
func (m *Manager) BufferTSOWithRetraction(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	ts core.LogicalTime,
	evt core.OutboundEvent,
	sender core.FederateHandle,
	retractionHandle uint64,
) error {
	reservation := m.reserveTSO(
		fed,
		[]core.TSOBufferedDelivery{{
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
func (m *Manager) RetractMessage(
	fed core.FederationName,
	sender core.FederateHandle,
	retractionHandle uint64,
) int {
	if retractionHandle == 0 {
		return 0
	}
	ext := extOf(m)
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
		ns.tsoBuffer = append([]bufferedTSOEvent(nil), filtered...)
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
// If a Send fails, the remaining buffered events are discarded — the
// federate's stream is gone, so retrying would just block. The discard
// is silent (no error returned) because emitGrant cannot meaningfully
// recover from a stream-side failure either.
func (m *Manager) takeBufferedTSO(fed core.FederationName, h core.FederateHandle, t *core.LogicalTime) []bufferedTSOEvent {
	ext := extOf(m)
	ext.mu.Lock()
	ns := ext.getLocked(fed, h)
	if ns == nil || len(ns.tsoBuffer) == 0 {
		ext.mu.Unlock()
		return nil
	}
	// Partition eligible events in FIFO order. A nil bound drains all.
	keep := ns.tsoBuffer[:0]
	var release []bufferedTSOEvent
	for _, b := range ns.tsoBuffer {
		if t == nil || float64(b.timestamp) <= float64(*t) {
			release = append(release, b)
		} else {
			keep = append(keep, b)
		}
	}
	// keep aliased ns.tsoBuffer's backing array; reassign explicitly.
	ns.tsoBuffer = append([]bufferedTSOEvent(nil), keep...)
	ext.mu.Unlock()
	return release
}

func (m *Manager) restoreBufferedTSO(fed core.FederationName, h core.FederateHandle, events []bufferedTSOEvent) {
	if len(events) == 0 {
		return
	}
	ext := extOf(m)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	restored := make([]bufferedTSOEvent, 0, len(events)+len(ns.tsoBuffer))
	restored = append(restored, events...)
	restored = append(restored, ns.tsoBuffer...)
	ns.tsoBuffer = restored
	ext.mu.Unlock()
}

func (m *Manager) releaseBufferedTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	release := m.takeBufferedTSO(fed, h, &t)
	for i, b := range release {
		if err := m.opts.Outbox.Send(ctx, fed, h, b.event); err != nil {
			m.restoreBufferedTSO(fed, h, release[i:])
			return err
		}
	}
	return nil
}

// drainAllBufferedTSO releases EVERY buffered TSO event for the
// federate, regardless of timestamp. Called from EnableAsynchronousDelivery
// to satisfy the spec's "async-on becomes observable as soon as the
// call returns" requirement.
func (m *Manager) drainAllBufferedTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	release := m.takeBufferedTSO(fed, h, nil)
	for i, b := range release {
		if err := m.opts.Outbox.Send(ctx, fed, h, b.event); err != nil {
			m.restoreBufferedTSO(fed, h, release[i:])
			return err
		}
	}
	return nil
}

// EnableAsynchronousDelivery implements core.TimeManager (M22).
//
// Per IEEE 1516.1 §8.16: opt into immediate TSO delivery (gorti's
// pre-M22 behavior). Idempotent against already-async (returns
// ErrTimeAlreadyAsynchronous). Drains the federate's TSO buffer
// immediately so the toggle is observable as soon as the call returns.
//
// Errors:
//   - core.ErrFederationHalted if the federation is halted
//   - core.ErrTimeAlreadyAsynchronous if already async
func (m *Manager) EnableAsynchronousDelivery(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	ext := extOf(m)
	evalLock := ext.evaluatorLock(fed)
	evalLock.Lock()
	defer evalLock.Unlock()
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	if ns.asyncDelivery {
		ext.mu.Unlock()
		return core.ErrTimeAlreadyAsynchronous
	}
	ns.asyncDelivery = true
	ext.mu.Unlock()

	// Drain everything regardless of timestamp.
	if err := m.drainAllBufferedTSO(ctx, fed, h); err != nil {
		ext.mu.Lock()
		ns := ext.getOrCreateLocked(fed, h)
		ns.asyncDelivery = false
		ext.mu.Unlock()
		return err
	}
	return nil
}

// DisableAsynchronousDelivery implements core.TimeManager (M22).
//
// Per IEEE 1516.1 §8.17: opt into buffered TSO delivery (the spec
// default). State mutation only; does not retroactively recall
// already-delivered events.
//
// Errors:
//   - core.ErrFederationHalted if the federation is halted
//   - core.ErrTimeNotAsynchronous if already off
func (m *Manager) DisableAsynchronousDelivery(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	ext := extOf(m)
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
