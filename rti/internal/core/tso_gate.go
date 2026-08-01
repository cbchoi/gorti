// Package core — TSODeliveryGate (M22 W2 / TASK-228).
//
// The TSO delivery gate decides whether a TimeStampOrdered outbound
// event may go directly to the recipient federate's outbox now, or
// must be buffered until the federate's logical time catches up.
//
// IEEE 1516.1 §8.16 (asynchronous delivery): when async ON, the gate
// always permits immediate delivery. When async OFF (the spec
// default and gorti's M22 default), the gate buffers TSO events with
// timestamp t > federate.currentTime; the buffer drains on advance
// grant or on toggle to async-on.
//
// RO (Receive-Order) events are unaffected by this gate — callers do
// not consult it for events without a timestamp.

package core

import "context"

// TSODeliveryGate is implemented by *time.Manager and consumed by
// object.Registry's TSO send sites (update.go / interaction.go). When
// Options.TSOGate is nil, callers fall back to direct Outbox.Send,
// preserving pre-M22 behavior in tests + in-process fixtures that do
// not wire a time manager.
type TSODeliveryGate interface {
	// ShouldDeliverNow reports whether a TSO event with timestamp ts
	// may be Sent immediately to the recipient federate. Returns true
	// when the federate has async delivery enabled OR currentTime >= ts.
	// Returns false when the event must be buffered.
	ShouldDeliverNow(fed FederationName, h FederateHandle, ts LogicalTime) bool

	// BufferTSO enqueues an event whose ShouldDeliverNow returned false.
	// The gate owns the event from this point and Sends it via the
	// configured Outbox when the federate's currentTime advances past
	// ts (advance grant) or when async delivery is toggled on.
	//
	// Buffer is per-federate, FIFO. M22 ships unbounded; bound is an
	// M23 follow-up.
	BufferTSO(ctx context.Context, fed FederationName, h FederateHandle, ts LogicalTime, evt OutboundEvent) error

	// BufferTSOWithRetraction is BufferTSO + retraction-handle
	// tracking (M20.2 §8.21). ``sender`` and ``retractionHandle``
	// identify the originating send; a future Retract RPC walks
	// every recipient's buffer looking for (sender, handle) and
	// removes matching entries. Zero retractionHandle is treated
	// as "no retraction wanted" — the event still buffers but
	// won't be findable. Idempotent over the base BufferTSO when
	// retractionHandle == 0.
	BufferTSOWithRetraction(
		ctx context.Context,
		fed FederationName,
		h FederateHandle,
		ts LogicalTime,
		evt OutboundEvent,
		sender FederateHandle,
		retractionHandle uint64,
	) error

	// RetractMessage removes every buffered TSO event matching
	// (fed, sender, retractionHandle). Returns the count of events
	// removed across all recipient buffers (zero when no buffered
	// event matches — the message may have already been delivered).
	RetractMessage(
		fed FederationName,
		sender FederateHandle,
		retractionHandle uint64,
	) int
}

// TSOBufferedDelivery is one timestamp-ordered callback considered for
// server-side buffering. Sender and RetractionHandle are optional together.
type TSOBufferedDelivery struct {
	Recipient        FederateHandle
	Timestamp        LogicalTime
	Event            OutboundEvent
	Sender           FederateHandle
	RetractionHandle uint64
}

// TSOBufferReservation holds the federation's time-evaluation boundary from
// final delivery classification through WAL and fanout admission.
type TSOBufferReservation interface {
	// Immediate returns deliveries promoted because logical time advanced
	// between the caller's optimistic classification and this reservation.
	Immediate() []TSOBufferedDelivery
	Buffered() []TSOBufferedDelivery
	// Commit transfers all remaining deliveries to the TSO buffer and releases
	// the evaluation boundary. It is idempotent and cannot fail.
	Commit(ctx context.Context)
	Release()
}

// ReservableTSODeliveryGate closes the ShouldDeliverNow/BufferTSO race for
// fanout paths that must append their WAL record between classification and
// ownership transfer.
type ReservableTSODeliveryGate interface {
	TSODeliveryGate
	ReserveTSO(fed FederationName, deliveries []TSOBufferedDelivery) TSOBufferReservation
}
