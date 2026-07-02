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

	"github.com/cbchoi/gorti/rti/internal/core"
)

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

// BufferTSO implements core.TSODeliveryGate. Enqueues an event whose
// ShouldDeliverNow returned false. The buffer is per-federate, FIFO,
// unbounded (M22 ships without a cap; bound is an M23 follow-up).
//
// Concurrency: takes nerStore.mu briefly to append. The send path that
// consults the gate has already finished its outbox lookup; this call
// just stores. Release happens later via releaseBufferedTSO (called
// from emitGrant or EnableAsynchronousDelivery).
func (m *Manager) BufferTSO(_ context.Context, fed core.FederationName, h core.FederateHandle, ts core.LogicalTime, evt core.OutboundEvent) error {
	ext := extOf(m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getOrCreateLocked(fed, h)
	ns.tsoBuffer = append(ns.tsoBuffer, bufferedTSOEvent{timestamp: ts, event: evt})
	return nil
}

// BufferTSOWithRetraction is BufferTSO + retraction-handle tracking.
// M20.2 — see core.TSODeliveryGate interface comment.
func (m *Manager) BufferTSOWithRetraction(
	_ context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	ts core.LogicalTime,
	evt core.OutboundEvent,
	sender core.FederateHandle,
	retractionHandle uint64,
) error {
	ext := extOf(m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getOrCreateLocked(fed, h)
	ns.tsoBuffer = append(ns.tsoBuffer, bufferedTSOEvent{
		timestamp:        ts,
		event:            evt,
		sender:           sender,
		retractionHandle: retractionHandle,
	})
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
func (m *Manager) releaseBufferedTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) {
	ext := extOf(m)
	ext.mu.Lock()
	ns := ext.getLocked(fed, h)
	if ns == nil || len(ns.tsoBuffer) == 0 {
		ext.mu.Unlock()
		return
	}
	// Partition: events with ts <= t get released; the rest stay.
	keep := ns.tsoBuffer[:0]
	var release []bufferedTSOEvent
	for _, b := range ns.tsoBuffer {
		if float64(b.timestamp) <= float64(t) {
			release = append(release, b)
		} else {
			keep = append(keep, b)
		}
	}
	// keep aliased ns.tsoBuffer's backing array; reassign explicitly.
	ns.tsoBuffer = append([]bufferedTSOEvent(nil), keep...)
	ext.mu.Unlock()

	for _, b := range release {
		_ = m.opts.Outbox.Send(ctx, fed, h, b.event)
	}
}

// drainAllBufferedTSO releases EVERY buffered TSO event for the
// federate, regardless of timestamp. Called from EnableAsynchronousDelivery
// to satisfy the spec's "async-on becomes observable as soon as the
// call returns" requirement.
func (m *Manager) drainAllBufferedTSO(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
	ext := extOf(m)
	ext.mu.Lock()
	ns := ext.getLocked(fed, h)
	if ns == nil || len(ns.tsoBuffer) == 0 {
		ext.mu.Unlock()
		return
	}
	release := ns.tsoBuffer
	ns.tsoBuffer = nil
	ext.mu.Unlock()

	for _, b := range release {
		_ = m.opts.Outbox.Send(ctx, fed, h, b.event)
	}
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
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	if ns.asyncDelivery {
		ext.mu.Unlock()
		return core.ErrTimeAlreadyAsynchronous
	}
	ns.asyncDelivery = true
	ext.mu.Unlock()

	// Drain everything regardless of timestamp.
	m.drainAllBufferedTSO(ctx, fed, h)
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
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ns := ext.getOrCreateLocked(fed, h)
	if !ns.asyncDelivery {
		return core.ErrTimeNotAsynchronous
	}
	ns.asyncDelivery = false
	return nil
}
