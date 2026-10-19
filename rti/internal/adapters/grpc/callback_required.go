package grpc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// MandatoryCallbackOutbox reserves invocation evidence and callback capacity
// together. Its caller already holds normal RTI lifecycle/checkpoint admission.
type MandatoryCallbackOutbox struct {
	next     core.ReservableOutbox
	receipts func() *CallbackReceipts
	required func(core.OutboundEvent) bool
}

func NewMandatoryCallbackOutbox(next core.ReservableOutbox, receipts func() *CallbackReceipts) *MandatoryCallbackOutbox {
	return &MandatoryCallbackOutbox{next: next, receipts: receipts}
}

// NewSelectiveCallbackOutbox retains the same atomic reservation boundary for
// mixed batches without requiring invocation evidence for unrelated callbacks.
func NewSelectiveCallbackOutbox(next core.ReservableOutbox, receipts func() *CallbackReceipts, required func(core.OutboundEvent) bool) *MandatoryCallbackOutbox {
	return &MandatoryCallbackOutbox{next: next, receipts: receipts, required: required}
}

func (o *MandatoryCallbackOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	reservation, err := o.Reserve(ctx, fed, []core.OutboxDelivery{{Recipient: h, Event: event}})
	if err != nil {
		return err
	}
	defer reservation.Release()
	return reservation.Commit()
}

func (o *MandatoryCallbackOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o == nil || o.next == nil || o.receipts == nil || o.receipts() == nil {
		return nil, fmt.Errorf("mandatory callback receiver unavailable")
	}
	r := o.receipts()
	required, wire, count, data, err := o.prepareCallbackEnvelopes(deliveries)
	if err != nil {
		return nil, err
	}
	credits, err := r.reserveCallbackCredits(count, data)
	if err != nil {
		return nil, err
	}
	defer credits.release()
	prepared := &mandatoryCallbackReservation{receipts: r}
	wireDeliveries := make([]core.OutboxDelivery, 0, len(deliveries))
	for i, delivery := range deliveries {
		if !required[i] {
			wireDeliveries = append(wireDeliveries, delivery)
			continue
		}
		event := wire[i]
		ticket, rollback, err := r.prepareWithCredits(ctx, fed, delivery.Recipient, event, true, credits)
		if err != nil {
			prepared.Release()
			return nil, err
		}
		prepared.tickets = append(prepared.tickets, ticket)
		prepared.rollbacks = append(prepared.rollbacks, rollback)
		event.CallbackReceipt = slices.Clone(ticket)
		r.mu.Lock()
		r.entries[string(ticket)].source = delivery.Event
		r.mu.Unlock()
		wrapped := &requiredCallbackEvent{event: event, source: delivery.Event, receipts: r}
		prepared.events = append(prepared.events, wrapped)
		wireDeliveries = append(wireDeliveries, core.OutboxDelivery{Recipient: delivery.Recipient, Event: wrapped})
	}
	reservation, err := o.next.Reserve(ctx, fed, wireDeliveries)
	if err != nil || reservation == nil {
		prepared.Release()
		if err == nil {
			err = fmt.Errorf("mandatory callback outbox returned no reservation")
		}
		return nil, err
	}
	prepared.queue = reservation
	return prepared, nil
}

type requiredCallbackEvent struct {
	event    *rtiv1.FederateEvent
	source   core.OutboundEvent
	receipts *CallbackReceipts
}

func (e *requiredCallbackEvent) Seq() uint64 { return e.event.GetSeq() }
func (e *requiredCallbackEvent) Inner() *rtiv1.FederateEvent {
	return proto.Clone(e.event).(*rtiv1.FederateEvent)
}

type mandatoryCallbackReservation struct {
	mu        sync.Mutex
	done      bool
	err       error
	receipts  *CallbackReceipts
	queue     core.OutboxReservation
	tickets   [][]byte
	rollbacks []func(bool)
	events    []*requiredCallbackEvent
}

func (p *mandatoryCallbackReservation) Commit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return p.err
	}
	// Serialize ordinals with queue publication, without holding the ledger
	// mutex while the queue commits or a stream reports callback entry.
	p.receipts.publicationMu.Lock()
	defer p.receipts.publicationMu.Unlock()
	p.refreshOwnershipPublication()
	// Mark before publishing: a stream may drain the queue during Commit.
	p.receipts.mu.Lock()
	if uint64(len(p.tickets)) > ^uint64(0)-p.receipts.nextCheckpointOrder {
		p.err = fmt.Errorf("callback receipt admission order exhausted")
	}
	for _, ticket := range p.tickets {
		if p.err != nil {
			break
		}
		if entry := p.receipts.entries[string(ticket)]; entry != nil {
			p.receipts.nextCheckpointOrder++
			entry.checkpointOrder = p.receipts.nextCheckpointOrder
			entry.admitted = true
		}
	}
	p.receipts.mu.Unlock()
	if p.err == nil {
		p.err = p.queue.Commit()
	}
	if p.err == nil {
		p.receipts.mu.Lock()
		for _, ticket := range p.tickets {
			if entry := p.receipts.entries[string(ticket)]; entry != nil {
				p.receipts.publishCheckpointLocked(entry)
			}
		}
		p.receipts.mu.Unlock()
	}
	if p.err != nil {
		p.receipts.mu.Lock()
		for _, ticket := range p.tickets {
			if entry := p.receipts.entries[string(ticket)]; entry != nil {
				entry.admitted = false
			}
		}
		p.receipts.mu.Unlock()
		p.queue.Release()
		for _, rollback := range p.rollbacks {
			rollback(false)
		}
	}
	p.done = true
	return p.err
}
func (p *mandatoryCallbackReservation) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return
	}
	if p.queue != nil {
		p.queue.Release()
	}
	for _, rollback := range p.rollbacks {
		rollback(false)
	}
	p.done = true
}

func (r *CallbackReceipts) preparedDelivery(fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) (func(bool), bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prepared, skip, err := r.preparedDeliveryLocked(fed, h, event)
	if err != nil || skip {
		return nil, skip, err
	}
	return func(sent bool) { r.commit(prepared.key, prepared.entry, sent) }, false, nil
}

func (r *CallbackReceipts) preparedDeliveryLocked(fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) (preparedCallbackDelivery, bool, error) {
	key := string(event.GetCallbackReceipt())
	entry := r.entries[key]
	if entry == nil || !entry.required || !entry.admitted || entry.fed != fed || entry.h != h {
		return preparedCallbackDelivery{}, false, status.Error(codes.FailedPrecondition, "required callback receipt unavailable")
	}
	if (entry.entryApplied && !entry.recoveryOutcome) || entry.applied {
		return preparedCallbackDelivery{}, true, nil
	}
	if entry.source != nil {
		if _, err := delivery.ResolveEvent(entry.source); err != nil {
			if errors.Is(err, delivery.ErrObsoleteAdvisory) && r.discardObsoleteLocked(key, entry) {
				return preparedCallbackDelivery{}, true, nil
			}
			return preparedCallbackDelivery{}, false, status.Error(codes.FailedPrecondition, "callback source is no longer deliverable")
		}
	}
	if err := validatePreparedCallbackPayload(entry, event); err != nil {
		return preparedCallbackDelivery{}, false, err
	}
	entry.retry = false
	return preparedCallbackDelivery{key: key, entry: entry}, false, nil
}

func (r *CallbackReceipts) markDisconnected(fed core.FederationName, h core.FederateHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.entries {
		if entry.fed == fed && entry.h == h && entry.applied && entry.event.GetCallbackRecovery().GetCompleted() {
			entry.retry = true
			continue
		}
		if entry.fed == fed && entry.h == h && entry.required && entry.admitted && (!entry.entryApplied || entry.recoveryOutcome) && !entry.applied {
			entry.retry = true
		}
	}
}

func (r *CallbackReceipts) pendingRedelivery(fed core.FederationName, h core.FederateHandle) []*rtiv1.FederateEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var events []*rtiv1.FederateEvent
	orders := map[*rtiv1.FederateEvent]uint64{}
	for key, entry := range r.entries {
		if entry.fed == fed && entry.h == h && r.discardObsoleteLocked(key, entry) {
			continue
		}
		if entry.fed == fed && entry.h == h && entry.applied && entry.retry && entry.event.GetCallbackRecovery().GetCompleted() {
			event := proto.Clone(entry.event).(*rtiv1.FederateEvent)
			events = append(events, event)
			orders[event] = entry.checkpointOrder
			continue
		}
		if entry.pendingRedeliveryFor(fed, h) {
			event := proto.Clone(entry.event).(*rtiv1.FederateEvent)
			event.CallbackReceipt = []byte(key)
			events = append(events, event)
			orders[event] = entry.checkpointOrder
		}
	}
	slices.SortFunc(events, func(a, b *rtiv1.FederateEvent) int {
		left := orders[a]
		right := orders[b]
		if left < right {
			return -1
		}
		if left > right {
			return 1
		}
		return 0
	})
	return events
}

// Only explicitly cancellable, unentered advice may retire without an outcome.
// EventGuard is lock-free; terminal callbacks remain fail-closed.
func (r *CallbackReceipts) discardObsoleteLocked(key string, entry *callbackReceipt) bool {
	if entry.source == nil || entry.entryApplied || entry.applied || entry.entryWorking != nil || entry.outcomeWorking != nil {
		return false
	}
	if _, err := delivery.ResolveEvent(entry.source); !errors.Is(err, delivery.ErrObsoleteAdvisory) {
		return false
	}
	delete(r.entries, key)
	r.retirePendingLocked()
	r.pendingDelta(entry, -1)
	return true
}

func (r *CallbackReceipts) discardObsolete(ticket []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry := r.entries[string(ticket)]; entry != nil {
		r.discardObsoleteLocked(string(ticket), entry)
	}
}
func (o *MandatoryCallbackOutbox) prepareCallbackEnvelopes(deliveries []core.OutboxDelivery) ([]bool, []*rtiv1.FederateEvent, int, bool, error) {
	required := make([]bool, len(deliveries))
	wire := make([]*rtiv1.FederateEvent, len(deliveries))
	count := 0
	data := false
	for i, delivery := range deliveries {
		required[i] = o.required == nil || o.required(delivery.Event)
		if required[i] {
			count++
			event, err := toDispatchFederateEvent(delivery.Event)
			if err != nil {
				return nil, nil, 0, false, err
			}
			if event == nil {
				return nil, nil, 0, false, fmt.Errorf("mandatory callback is not convertible")
			}
			wire[i] = event
			data = data || event.GetReflect() != nil || event.GetReceive() != nil
		}
	}
	return required, wire, count, data, nil
}

func (p *mandatoryCallbackReservation) refreshOwnershipPublication() {
	// Ownership transactions assign their immutable callback ID after queue
	// reservation. Read that committed identity before publishing the envelope.
	for i, event := range p.events {
		if event.event.GetOwnershipAcquired() == nil && event.event.GetOwnershipUnavailable() == nil && event.event.GetOwnershipAcquisitionCancelled() == nil {
			continue
		}
		current, err := toDispatchFederateEvent(event.source)
		if err != nil || current == nil {
			p.err = fmt.Errorf("ownership callback publication identity unavailable: %w", err)
			break
		}
		original := proto.Clone(event.event).(*rtiv1.FederateEvent)
		original.CallbackReceipt = nil
		original.OwnershipReceipt = nil
		withoutAuthority := proto.Clone(current).(*rtiv1.FederateEvent)
		withoutAuthority.OwnershipReceipt = nil
		if !proto.Equal(original, withoutAuthority) {
			p.err = fmt.Errorf("ownership callback body changed during reservation")
			break
		}
		event.event.OwnershipReceipt = current.OwnershipReceipt
		p.receipts.mu.Lock()
		if entry := p.receipts.entries[string(p.tickets[i])]; entry != nil {
			entry.event.OwnershipReceipt = current.OwnershipReceipt
			entry.checkpointExcluded = false
		}
		p.receipts.mu.Unlock()
	}
}

func validatePreparedCallbackPayload(entry *callbackReceipt, event *rtiv1.FederateEvent) error {
	// Borrow payloads only for this comparison. A new envelope avoids copying
	// generated message state or mutating the transport/ledger's owned messages.
	view := &rtiv1.FederateEvent{
		Seq:                  event.Seq,
		OwnershipReceipt:     event.OwnershipReceipt,
		ObjectKnowledgeEpoch: event.ObjectKnowledgeEpoch,
		CallbackRecovery:     event.CallbackRecovery,
		Event:                event.Event,
	}
	view.ProtoReflect().SetUnknown(event.ProtoReflect().GetUnknown())
	// The ownership adapter replaces delivery-attempt authority independently.
	// Its body and domain callback ID must still match the reserved envelope.
	if saved := entry.event.GetOwnershipReceipt(); saved != nil {
		if actual := view.GetOwnershipReceipt(); actual != nil && actual.CallbackId == saved.CallbackId {
			view.OwnershipReceipt = saved
		} else if entry.recoveryOutcome && actual == nil {
			view.OwnershipReceipt = saved
		} else {
			return status.Error(codes.InvalidArgument, "required ownership callback identity mismatch")
		}
	}
	if !proto.Equal(view, entry.event) {
		return status.Error(codes.InvalidArgument, "required callback receipt payload mismatch")
	}
	return nil
}

func (entry *callbackReceipt) pendingRedeliveryFor(fed core.FederationName, h core.FederateHandle) bool {
	return entry.fed == fed && entry.h == h && entry.required && entry.admitted && entry.retry &&
		(!entry.entryApplied || entry.recoveryOutcome) && !entry.applied
}
