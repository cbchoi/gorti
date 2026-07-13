package object

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

type managementClassClassification struct {
	isManager bool
	name      string
	fom       core.FOMHandle
	names     core.FOMHandleNameLookup
}

// sendInteraction implements core.ObjectRegistry.SendInteraction.
//
// Symmetric to UpdateAttributes for interactions:
//  1. Verify the producer publishes the interaction class via
//     declaration.Manager. Reject with ErrInteractionClassNotPublished
//     otherwise.
//  2. Persist InteractionSent to the EventLog (write-ahead).
//  3. Resolve delivery order: when the federation is best-effort AND the
//     interaction class is declared OrderReceive in the FOM, the
//     outbound event drops its timestamp (RO). Otherwise TSO. See
//     TASK-077.
//  4. Fan ReceiveInteraction to subscribers in sorted FederateHandle
//     order, EXCEPT the producer.
func (r *Registry) sendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
	ts *core.LogicalTime,
	retractionHandle uint64,
) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}

	// M20.3 — MOM dispatch. Resolve the class name once; if it's in
	// the HLAmanager subtree AND a dispatcher is wired AND the
	// dispatcher recognizes the class, dispatch INSTEAD of doing
	// normal publish-gate + fanout. Per IEEE 1516.1 §10: management
	// interactions don't need a producer publish declaration, and
	// they aren't broadcast to subscribers.
	st := r.stateFor(fed)
	var management managementClassClassification
	var managementKnown bool
	if r.opts.ManagementDispatch != nil {
		management, managementKnown = r.managementClass(ctx, fed, st, cls)
	}
	if err := r.validateInteractionHandles(ctx, fed, st, cls, params); err != nil {
		return err
	}
	if managementKnown && management.isManager {
		return r.opts.ManagementDispatch.Dispatch(
			ctx, fed, management.name, producer, params, management.fom, management.names,
		)
	}
	if !r.producerPublishesInteraction(ctx, fed, producer, cls) {
		return core.ErrInteractionClassNotPublished
	}

	// M37 EB-3 — §8.1.2 outgoing-TSO timestamp validation, BEFORE the
	// eventlog write-ahead so rejected sends never enter the replay log.
	if err := r.validateOutgoingTSO(fed, producer, ts); err != nil {
		return err
	}

	wireParams := copyParamMap(params)
	deliveryTs := r.deliveryTimestampForInteraction(fed, cls, ts)
	fanout, err := r.prepareFanoutReceive(ctx, fed, st, producer, cls, wireParams, deliveryTs, retractionHandle)
	if err != nil {
		return fmt.Errorf("object: send interaction class %d in %q: reserve fanout: %w", cls, fed, err)
	}
	defer fanout.abort()

	if r.opts.EventLog != nil {
		ev := newInteractionSentEvent(cls, producer, wireParams, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			return fmt.Errorf("object: send interaction class %d in %q: eventlog append: %w", cls, fed, err)
		}
	}

	if err := fanout.commit(ctx); err != nil {
		// The write-ahead record may already be committed. Surface an
		// indeterminate delivery result instead of acknowledging success.
		return fmt.Errorf("object: send interaction class %d in %q: fanout: %w", cls, fed, err)
	}
	if r.opts.OnInteractionSent != nil {
		r.opts.OnInteractionSent(fed, producer)
	}
	return nil
}

func (r *Registry) validateInteractionHandles(
	ctx context.Context,
	fed core.FederationName,
	st *federationState,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
) error {
	if r.opts.FOMs == nil {
		return nil
	}
	var names core.FOMHandleNameLookup
	st.managementClassMu.RLock()
	classification, cached := st.managementClasses[cls]
	st.managementClassMu.RUnlock()
	if cached {
		names = classification.names
	} else {
		handle, err := r.opts.FOMs.Get(ctx, fed)
		if err != nil {
			// Legacy in-process registries may provide only a load-time FOM
			// validator and no federation-keyed reverse lookup. Production rtid
			// remembers every created federation and therefore takes the strict
			// path below.
			if errors.Is(err, core.ErrFederationNotFound) {
				return nil
			}
			return err
		}
		var ok bool
		names, ok = handle.(core.FOMHandleNameLookup)
		if !ok {
			return nil
		}
	}
	if !cached {
		if _, found := names.InteractionClassName(cls); !found {
			return core.ErrInteractionClassNotFound
		}
	}
	for parameter := range params {
		if _, found := names.ParameterName(cls, parameter); !found {
			return core.ErrInteractionParameterNotFound
		}
	}
	return nil
}

// managementClass classifies an interaction once per federation and class.
// Federation FOMs are immutable after creation, so successful classifications
// and the handles required for manager dispatch remain valid for the state's
// lifetime. Failed FOM/name lookups are not cached, preserving retry behavior.
func (r *Registry) managementClass(
	ctx context.Context,
	fed core.FederationName,
	st *federationState,
	cls core.InteractionClassHandle,
) (managementClassClassification, bool) {
	st.managementClassMu.RLock()
	classification, ok := st.managementClasses[cls]
	st.managementClassMu.RUnlock()
	if ok {
		return classification, true
	}

	st.managementClassMu.Lock()
	defer st.managementClassMu.Unlock()
	if classification, ok = st.managementClasses[cls]; ok {
		return classification, true
	}

	fomH, err := r.opts.FOMs.Get(ctx, fed)
	if err != nil || fomH == nil {
		return managementClassClassification{}, false
	}
	names, ok := fomH.(core.FOMHandleNameLookup)
	if !ok {
		return managementClassClassification{}, false
	}
	name, ok := names.InteractionClassName(cls)
	if !ok {
		return managementClassClassification{}, false
	}

	classification = managementClassClassification{
		isManager: r.opts.ManagementDispatch.IsManagerClass(name),
		name:      name,
		fom:       fomH,
		names:     names,
	}
	st.managementClasses[cls] = classification
	return classification, true
}

// deliveryTimestampForInteraction is the interaction-side analogue of
// deliveryTimestampForAttributes. See that function's doc for the
// best-effort-mode-plus-OrderReceive contract.
func (r *Registry) deliveryTimestampForInteraction(
	fed core.FederationName,
	cls core.InteractionClassHandle,
	ts *core.LogicalTime,
) *core.LogicalTime {
	if ts == nil {
		return nil
	}
	if r.opts.Federations == nil || r.opts.Orders == nil {
		return ts
	}
	mode, ok := r.opts.Federations.ModeFor(fed)
	if !ok || mode != core.ModeBestEffort {
		return ts
	}
	ord, known := r.opts.Orders.InteractionOrder(fed, cls)
	if !known || ord != OrderReceive {
		return ts
	}
	return nil
}

func (r *Registry) producerPublishesInteraction(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle) bool {
	if lookup, ok := r.opts.Declarations.(interface {
		PublishesInteraction(
			context.Context,
			core.FederationName,
			core.InteractionClassHandle,
			core.FederateHandle,
		) bool
	}); ok {
		return lookup.PublishesInteraction(ctx, fed, cls, producer)
	}
	for _, h := range r.opts.Declarations.InteractionPublishersFor(ctx, fed, cls) {
		if h == producer {
			return true
		}
	}
	return false
}

type bufferedInteractionDelivery struct {
	sub   core.FederateHandle
	ts    core.LogicalTime
	event core.OutboundEvent
}

type preparedInteractionFanout struct {
	registry       *Registry
	fed            core.FederationName
	producer       core.FederateHandle
	retraction     uint64
	immediateSubs  []core.FederateHandle
	immediate      []core.OutboundEvent
	buffered       []bufferedInteractionDelivery
	tsoDeliveries  []core.TSOBufferedDelivery
	reservation    core.OutboxReservation
	tsoReservation core.TSOBufferReservation
	finished       bool
}

func (r *Registry) prepareFanoutReceive(ctx context.Context, fed core.FederationName, st *federationState, producer core.FederateHandle, cls core.InteractionClassHandle, params map[uint64][]byte, ts *core.LogicalTime, retractionHandle uint64) (*preparedInteractionFanout, error) {
	subs := r.opts.Declarations.InteractionSubscribersFor(ctx, fed, cls)
	// Hoist the inner proto and the param-map defensive copy out of
	// the per-subscriber loop. The producer's input map is copied once
	// (so the producer can mutate post-call); the resulting map and
	// the *ReceiveInteraction wrapping it are read-only from here on
	// and shared by every per-subscriber FederateEvent. proto.Marshal
	// does not mutate, so concurrent serialization across subscriber
	// streams is safe.
	pb := &rtiv1.ReceiveInteraction{
		InteractionClassHandle: uint64(cls),
		Parameters:             params,
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	// Acquire a seq range once per fanout instead of once per
	// subscriber to cut N-1 lock acquires from the hot path.
	nSeq := 0
	for _, sub := range subs {
		if sub != producer {
			nSeq++
		}
	}
	if nSeq == 0 {
		return &preparedInteractionFanout{registry: r, fed: fed, producer: producer, retraction: retractionHandle}, nil
	}
	st.mu.Lock()
	startSeq := st.nextOutboundSeqRangeLocked(nSeq)
	st.mu.Unlock()
	seq := startSeq
	prepared := &preparedInteractionFanout{registry: r, fed: fed, producer: producer, retraction: retractionHandle}
	reservableTSO, canReserveTSO := r.opts.TSOGate.(core.ReservableTSODeliveryGate)
	useTSOReservation := ts != nil && canReserveTSO
	if useTSOReservation {
		prepared.tsoDeliveries = make([]core.TSOBufferedDelivery, 0, nSeq)
	}
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		evt := &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq:   seq,
			Event: &rtiv1.FederateEvent_Receive{Receive: pb},
		}}
		seq++
		if useTSOReservation {
			prepared.tsoDeliveries = append(prepared.tsoDeliveries, core.TSOBufferedDelivery{
				Recipient: sub, Timestamp: *ts, Event: evt,
				Sender: producer, RetractionHandle: retractionHandle,
			})
			continue
		}
		if ts != nil && r.opts.TSOGate != nil && !r.opts.TSOGate.ShouldDeliverNow(fed, sub, *ts) {
			prepared.buffered = append(prepared.buffered, bufferedInteractionDelivery{sub: sub, ts: *ts, event: evt})
		} else {
			prepared.immediateSubs = append(prepared.immediateSubs, sub)
			prepared.immediate = append(prepared.immediate, evt)
		}
	}
	if useTSOReservation {
		prepared.tsoReservation = reservableTSO.ReserveTSO(fed, prepared.tsoDeliveries)
		for _, delivery := range prepared.tsoReservation.Immediate() {
			prepared.immediateSubs = append(prepared.immediateSubs, delivery.Recipient)
			prepared.immediate = append(prepared.immediate, delivery.Event)
		}
	}
	if reservable, ok := r.opts.Outbox.(core.ReservableOutbox); ok && len(prepared.immediateSubs) > 0 {
		deliveries := make([]core.OutboxDelivery, len(prepared.immediateSubs))
		for i, sub := range prepared.immediateSubs {
			deliveries[i] = core.OutboxDelivery{Recipient: sub, Event: prepared.immediate[i]}
		}
		reservation, err := reservable.Reserve(ctx, fed, deliveries)
		if err != nil {
			if prepared.tsoReservation != nil {
				prepared.tsoReservation.Release()
			}
			return nil, err
		}
		prepared.reservation = reservation
	}
	return prepared, nil
}

func (p *preparedInteractionFanout) commit(ctx context.Context) error {
	if p.finished {
		return nil
	}
	// Release the atomic immediate-recipient reservation before calling the
	// time gate. BufferTSO may synchronously evaluate a pending grant, whose
	// lock order is evaluator -> outbox; retaining recipient locks here would
	// create the reverse order and deadlock mixed RO/TSO fanout.
	if p.reservation != nil {
		if err := p.reservation.Commit(); err != nil {
			return err
		}
		p.reservation = nil
	} else {
		for i, sub := range p.immediateSubs {
			if err := p.registry.opts.Outbox.Send(ctx, p.fed, sub, p.immediate[i]); err != nil {
				return err
			}
		}
	}
	if p.tsoReservation != nil {
		p.tsoReservation.Commit(ctx)
		p.tsoReservation = nil
	} else {
		for _, delivery := range p.buffered {
			var err error
			if p.retraction != 0 {
				err = p.registry.opts.TSOGate.BufferTSOWithRetraction(
					ctx, p.fed, delivery.sub, delivery.ts, delivery.event, p.producer, p.retraction,
				)
			} else {
				err = p.registry.opts.TSOGate.BufferTSO(ctx, p.fed, delivery.sub, delivery.ts, delivery.event)
			}
			if err != nil {
				return err
			}
		}
	}
	if p.registry.opts.OnInteractionDelivered != nil {
		if len(p.tsoDeliveries) > 0 {
			for _, delivery := range p.tsoDeliveries {
				p.registry.opts.OnInteractionDelivered(p.fed, delivery.Recipient)
			}
		} else {
			for _, sub := range p.immediateSubs {
				p.registry.opts.OnInteractionDelivered(p.fed, sub)
			}
			for _, delivery := range p.buffered {
				p.registry.opts.OnInteractionDelivered(p.fed, delivery.sub)
			}
		}
	}
	p.finished = true
	return nil
}

func (p *preparedInteractionFanout) abort() {
	if p.finished {
		return
	}
	if p.reservation != nil {
		p.reservation.Release()
		p.reservation = nil
	}
	if p.tsoReservation != nil {
		p.tsoReservation.Release()
		p.tsoReservation = nil
	}
	p.finished = true
}

func copyParamMap(params map[core.ParameterHandle][]byte) map[uint64][]byte {
	out := make(map[uint64][]byte, len(params))
	for k, v := range params {
		out[uint64(k)] = append([]byte(nil), v...)
	}
	return out
}

func newInteractionSentEvent(cls core.InteractionClassHandle, producer core.FederateHandle, params map[uint64][]byte, ts *core.LogicalTime, wallNs int64) *eventRecord {
	pb := &rtiv1.InteractionSent{
		InteractionClassHandle: uint64(cls),
		ProducerFederateHandle: uint64(producer),
		Parameters:             params,
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	return &eventRecord{pb: &rtiv1.Event{
		WallNs: uint64(wallNs),
		Body:   &rtiv1.Event_InterSent{InterSent: pb},
	}}
}
