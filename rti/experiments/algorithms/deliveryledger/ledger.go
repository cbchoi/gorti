// Package deliveryledger explores bounded delivery accounting. It is a tested
// experimental foundation, not a production queue-length source or completed
// R3 feature. Integration requires a shared envelope and SDK reporting protocol.
package deliveryledger

import (
	"errors"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// MaxOutstanding bounds this experiment's retained observation metadata per
// ledger independently of the production outbox's capacity and configuration.
const MaxOutstanding = 1 << 20

var (
	ErrDeliveryIdentity   = errors.New("delivery ledger: invalid identity or ticket")
	ErrDeliveryCapacity   = errors.New("delivery ledger: observation capacity exceeded")
	ErrDeliveryClosed     = errors.New("delivery ledger: epoch closed")
	ErrDeliveryTransition = errors.New("delivery ledger: invalid transition")
	ErrDeliveryOrder      = errors.New("delivery ledger: received order unavailable")
	ErrDeliverySequence   = errors.New("delivery ledger: sequence exhausted")
)

// DeliveryScope names one membership/callback epoch. CallbackEpoch may be zero
// before the first restore. Owners must not reuse generation or incarnation.
type DeliveryScope struct {
	Federation    core.FederationName
	Generation    uint64
	Federate      core.FederateHandle
	Incarnation   uint64
	CallbackEpoch uint64
}

type DeliveryStage uint8

const (
	DeliveryTimeBuffer DeliveryStage = iota + 1
	DeliveryOutbox
	DeliveryTransport
	DeliveryCallbackQueue
)

type DeliveryOutcome uint8

const (
	// DeliveryInvoked means the user callback ran, whether it returned success
	// or an exception. Stream sends, producer ACKs and SDK enqueue are not this.
	DeliveryInvoked DeliveryOutcome = iota + 1
	// DeliveryCanceled requires confirmed removal, not merely a requestRetract
	// callback or an ambiguous transport failure.
	DeliveryCanceled
)

// DeliveryTicket is an opaque, process-local per-recipient admission identity.
// It is deliberately unrelated to OutboundEvent.Seq: producers and split
// callbacks can share sequences. Carry this value across handoffs; never
// readmit a moved event. Wire receipt correlation belongs to the adapter.
type DeliveryTicket struct {
	owner    *DeliveryLedger
	sequence uint64
}

func (t DeliveryTicket) Valid() bool { return t.owner != nil && t.sequence != 0 }

// ObservedDeliveryCounts describes only admissions supplied to this ledger.
// It is NOT HLAROlength/HLATSOlength or an end-to-end queue-length source, even
// when CoverageLost is false. The production admission, time, transport, SDK
// outcome and recovery hooks are not yet connected. ByStage[0] is unused.
type ObservedDeliveryCounts struct {
	Scope                                    DeliveryScope
	Admitted, Invoked, Canceled, Invalidated uint64
	OutstandingRO, OutstandingTSO            uint64
	ByStage                                  [5]uint64
	CoverageLost, Closed                     bool
}

// DeliveryAccounting is a proposed experimental observation port. Missed observations
// must invalidate metric coverage, never fail an already-accepted delivery or
// cause its callback to be invoked again. Methods do not call external code.
// Rejected foreign/expired tickets do not invalidate the current epoch. This
// port is not an authentication boundary for client-supplied tickets.
type DeliveryAccounting interface {
	Admit(core.OutboundEvent, DeliveryStage) (DeliveryTicket, error)
	Transfer(DeliveryTicket, DeliveryStage, DeliveryStage, core.OrderType) error
	Settle([]DeliveryTicket, DeliveryOutcome) error
	LoseCoverage()
	Close()
}

// DeliveryLedger retains at most capacity outstanding metadata entries, no
// callback payloads or completed-ticket tombstones. First terminal observation
// wins; duplicate/late outcomes cannot resurrect or recount a retired ticket.
// A separate ledger is needed after an invalidated epoch. It must not be copied.
type DeliveryLedger struct {
	mu       sync.Mutex
	capacity int
	sequence uint64
	entries  map[uint64]deliveryPosition
	counts   ObservedDeliveryCounts
}

type deliveryPosition struct {
	stage DeliveryStage
	order core.OrderType
}

func NewDeliveryLedger(scope DeliveryScope, capacity int) (*DeliveryLedger, error) {
	if scope.Federation == "" || scope.Generation == 0 || scope.Federate == 0 || scope.Incarnation == 0 {
		return nil, ErrDeliveryIdentity
	}
	if capacity < 1 || capacity > MaxOutstanding {
		return nil, ErrDeliveryCapacity
	}
	return &DeliveryLedger{
		capacity: capacity, entries: make(map[uint64]deliveryPosition),
		counts: ObservedDeliveryCounts{Scope: scope},
	}, nil
}

// Admit observes one committed per-recipient reflect, interaction or removal.
// Reserve/release before fanout must not call it. Non-user/control events return
// a zero ticket. The event's metadata must be stable during this call; no event
// reference is retained. A missed admission makes CoverageLost sticky, including
// after space becomes available. Capacity is observation-only, not backpressure.
func (l *DeliveryLedger) Admit(event core.OutboundEvent, stage DeliveryStage) (DeliveryTicket, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts.Closed {
		return DeliveryTicket{}, ErrDeliveryClosed
	}
	order, user, err := deliveryEventOrder(event, stage)
	if err != nil {
		l.counts.CoverageLost = true
		return DeliveryTicket{}, err
	}
	if !user {
		return DeliveryTicket{}, nil
	}
	if !validDeliveryStage(stage) {
		l.counts.CoverageLost = true
		return DeliveryTicket{}, ErrDeliveryTransition
	}
	if len(l.entries) >= l.capacity {
		l.counts.CoverageLost = true
		return DeliveryTicket{}, ErrDeliveryCapacity
	}
	if l.sequence == ^uint64(0) {
		l.counts.CoverageLost = true
		return DeliveryTicket{}, ErrDeliverySequence
	}
	l.sequence++
	l.entries[l.sequence] = deliveryPosition{stage: stage, order: order}
	l.counts.Admitted++
	return DeliveryTicket{owner: l, sequence: l.sequence}, nil
}

// Transfer observes a committed ownership handoff. A failed drain/reservation
// keeps its original position without a transfer. Only time-buffer release can
// change order: a timestamped send may actually be received in RO. Scratch,
// channel batching and wire frame splitting retain the same ticket. Repeated
// transfers to the current position and transfers of retired tickets are no-ops.
// Retired tickets ignore stage/order metadata; closed epochs and foreign or
// unissued tickets are still rejected before this no-op check.
func (l *DeliveryLedger) Transfer(ticket DeliveryTicket, from, to DeliveryStage, order core.OrderType) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts.Closed {
		return ErrDeliveryClosed
	}
	if !l.owns(ticket) {
		return ErrDeliveryIdentity
	}
	position, outstanding := l.entries[ticket.sequence]
	if !outstanding {
		return nil
	}
	if order != core.OrderReceive && order != core.OrderTimestamp {
		l.counts.CoverageLost = true
		return ErrDeliveryOrder
	}
	if !validDeliveryStage(from) || !validDeliveryStage(to) || to != from+1 {
		l.counts.CoverageLost = true
		return ErrDeliveryTransition
	}
	if position.stage == to && position.order == order {
		return nil
	}
	if position.stage != from || (from != DeliveryTimeBuffer && position.order != order) {
		l.counts.CoverageLost = true
		return ErrDeliveryTransition
	}
	l.entries[ticket.sequence] = deliveryPosition{stage: to, order: order}
	return nil
}

// Settle atomically validates a batch of explicit invocation or cancellation
// observations before retiring anything. Batched receipts need not be ordered
// or contiguous; duplicate tickets, including duplicates within a batch, are
// counted once. The first outcome wins, so this is not an outcome audit log.
// Invocation must be reported by a trusted actual-callback hook, even if its
// report races intermediate transport observations. No producer ACK API exists.
// A failed terminal observation for this epoch must be retained and retried by
// the owner or followed by LoseCoverage; never retry the callback itself.
func (l *DeliveryLedger) Settle(tickets []DeliveryTicket, outcome DeliveryOutcome) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts.Closed {
		return ErrDeliveryClosed
	}
	if outcome != DeliveryInvoked && outcome != DeliveryCanceled {
		return ErrDeliveryTransition
	}
	for _, ticket := range tickets {
		if !l.owns(ticket) {
			return ErrDeliveryIdentity
		}
	}
	for _, ticket := range tickets {
		if _, outstanding := l.entries[ticket.sequence]; !outstanding {
			continue
		}
		delete(l.entries, ticket.sequence)
		if outcome == DeliveryInvoked {
			l.counts.Invoked++
		} else {
			l.counts.Canceled++
		}
	}
	return nil
}

// LoseCoverage records an unobserved SDK, ambiguous loss or failed receipt
// correlation. Draining the observed subset cannot restore complete coverage.
func (l *DeliveryLedger) LoseCoverage() {
	l.mu.Lock()
	l.counts.CoverageLost = true
	l.mu.Unlock()
}

// Close invalidates every outstanding observation and rejects late reports.
// Use on committed restore/resign/destroy or abandoned reconnect state, never
// on a prepared-but-released lifecycle transaction. For a resumable reconnect,
// retain this ledger and its tickets; do not close/readmit preserved deliveries.
// This does not claim that callbacks already running remotely were canceled.
func (l *DeliveryLedger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts.Closed {
		return
	}
	l.counts.Invalidated += uint64(len(l.entries))
	clear(l.entries)
	l.counts.Closed = true
}

// Inspect returns a detached, mutually consistent diagnostic snapshot. Never
// sum it with time/outbox/client queue snapshots: those may own the same event.
func (l *DeliveryLedger) Inspect() ObservedDeliveryCounts {
	l.mu.Lock()
	defer l.mu.Unlock()
	counts := l.counts
	for _, position := range l.entries {
		counts.ByStage[position.stage]++
		if position.order == core.OrderReceive {
			counts.OutstandingRO++
		} else {
			counts.OutstandingTSO++
		}
	}
	return counts
}

func (l *DeliveryLedger) owns(ticket DeliveryTicket) bool {
	return ticket.owner == l && ticket.sequence != 0 && ticket.sequence <= l.sequence
}

func validDeliveryStage(stage DeliveryStage) bool {
	return stage >= DeliveryTimeBuffer && stage <= DeliveryCallbackQueue
}

var _ DeliveryAccounting = (*DeliveryLedger)(nil)
