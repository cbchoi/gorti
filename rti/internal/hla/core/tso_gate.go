// Package core — TSODeliveryGate (M22 W2 / TASK-228).
//
// The TSO delivery gate decides whether a TimeStampOrdered outbound
// event may go directly to the recipient federate's outbox now, or
// must be buffered until the federate's logical time catches up.
//
// Time-constrained recipients buffer future TSO events until advancement.
// Asynchronous delivery does not bypass this gate. Recipients that are not
// constrained receive timestamped messages in receive order.
//
// Reservation-capable gates also admit RO messages under the same evaluation
// boundary. Control callbacks do not use this message-delivery gate.
package core

import "context"

// TSODeliveryGate is implemented by *time.Manager and consumed by
// object.Registry's TSO send sites (update.go / interaction.go). When
// Options.TSOGate is nil, callers fall back to direct Outbox.Send,
// preserving pre-M22 behavior in tests + in-process fixtures that do
// not wire a time manager.
type TSODeliveryGateOf[T LogicalTimeScalar] interface {
	// ShouldDeliverNow reports whether a TSO event with timestamp ts
	// may be Sent immediately to the recipient federate. Returns true
	// when the federate is unconstrained OR currentTime >= ts, subject to saves.
	// Returns false when the event must be buffered.
	ShouldDeliverNow(fed FederationName, h FederateHandle, ts T) bool

	// BufferTSO enqueues an event whose ShouldDeliverNow returned false.
	// The gate owns the event from this point and Sends it via the
	// configured Outbox when the federate's currentTime advances past
	// ts (advance grant) or when time constraint is disabled.
	//
	// Buffer is per-federate, FIFO. M22 ships unbounded; bound is an
	// M23 follow-up.
	BufferTSO(ctx context.Context, fed FederationName, h FederateHandle, ts T, evt OutboundEvent) error

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
		ts T,
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

// MessageDelivery is an object-management message considered for buffering.
// ReceiveOrder selects the RO queue independently of the callback's optional
// original timestamp. RO entries cannot be retracted. Timestamp is the TSO
// ordering key only; an RO entry uses zero and preserves any supplied time in Event.
type MessageDeliveryOf[T LogicalTimeScalar] struct {
	Recipient        FederateHandle
	Timestamp        T
	Event            OutboundEvent
	Sender           FederateHandle
	RetractionHandle uint64
	ReceiveOrder     bool
}

// TSOBufferedDelivery retains the existing reservation interface name.

// QueuedMessageFilter reevaluates subscription interest when a queued message
// becomes eligible. It returns nil to discard a message. It must not mutate the
// original event or call the time manager; the evaluator lease is held.
type QueuedMessageFilter interface {
	FilterQueuedMessage(context.Context, FederationName, FederateHandle, OutboundEvent) OutboundEvent
}

// TSOBufferReservation holds the federation's time-evaluation boundary from
// final delivery classification through WAL and fanout admission.
type TSOBufferReservationOf[T LogicalTimeScalar] interface {
	// Immediate returns deliveries promoted because logical time advanced
	// between the caller's optimistic classification and this reservation.
	Immediate() []MessageDeliveryOf[T]
	Buffered() []MessageDeliveryOf[T]
	// Commit transfers all remaining deliveries to the appropriate queue and releases
	// the evaluation boundary. It is idempotent and cannot fail.
	Commit(ctx context.Context)
	Release()
}

// ReservableTSODeliveryGate closes the ShouldDeliverNow/BufferTSO race for
// fanout paths that must append their WAL record between classification and
// ownership transfer.
type ReservableTSODeliveryGateOf[T LogicalTimeScalar] interface {
	TSODeliveryGateOf[T]

	ReserveTSO(fed FederationName, deliveries []MessageDeliveryOf[T]) TSOBufferReservationOf[T]
}

// AtomicOutgoingTSODeliveryGate revalidates the caller's timestamp at the same
// boundary used for delivery admission. An empty recipient set still validates
// the send. Previously admitted internal deliveries use ReserveTSO instead.
type AtomicOutgoingTSODeliveryGateOf[T LogicalTimeScalar] interface {
	ReservableTSODeliveryGateOf[T]

	ReserveOutgoingTSO(fed FederationName, sender FederateHandle, timestamp T, deliveries []MessageDeliveryOf[T]) (TSOBufferReservationOf[T], error)
}

// PreparingMessageDeliveryGate acquires the evaluator before a composition
// transaction takes service-state locks. The preparation owns its rollback;
// the returned reservation retains the evaluator through final admission.
type PreparingMessageDeliveryGateOf[T LogicalTimeScalar] interface {
	ReservableTSODeliveryGateOf[T]

	ReserveMessagePreparation(FederationName, func() ([]MessageDeliveryOf[T], error)) (TSOBufferReservationOf[T], error)
}

type TSODeliveryGate = TSODeliveryGateOf[LogicalTime]

type MessageDelivery = MessageDeliveryOf[LogicalTime]

type TSOBufferedDelivery = MessageDeliveryOf[LogicalTime]

type TSOBufferReservation = TSOBufferReservationOf[LogicalTime]

type ReservableTSODeliveryGate = ReservableTSODeliveryGateOf[LogicalTime]

type AtomicOutgoingTSODeliveryGate = AtomicOutgoingTSODeliveryGateOf[LogicalTime]

type PreparingMessageDeliveryGate = PreparingMessageDeliveryGateOf[LogicalTime]

type TSOGateOf[T LogicalTimeScalar] interface{ TSODeliveryGateOf[T] }
type ReservationOf[T LogicalTimeScalar] interface{ TSOBufferReservationOf[T] }
