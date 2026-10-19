package object

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
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
//  3. Resolve preferred order from publisher overrides, then FOM defaults,
//     independently of profile. Retain any supplied timestamp on RO callbacks.
//  4. Fan ReceiveInteraction to subscribers in sorted FederateHandle
//     order, EXCEPT the producer.
func (r *RegistryOf[T]) sendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
	ts *T,
	retractionHandle uint64,
) error {
	return r.sendInteractionTo(ctx, fed, producer, cls, params, ts, retractionHandle, nil, false)
}

func (r *RegistryOf[T]) sendInteractionTo(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *T, retractionHandle uint64, subscribers []core.FederateHandle, scoped bool, metadata ...messageMetadata) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	if err := validateMessageTimestampOf[T](ts); err != nil {
		return err
	}

	// M20.3 — MOM dispatch. Resolve the class name once; if it's in
	// the HLAmanager subtree AND a dispatcher is wired AND the
	// dispatcher recognizes the class, dispatch INSTEAD of doing
	// normal publish-gate + fanout. Per IEEE 1516.1 §10: management
	// interactions don't need a producer publish declaration, and
	// they aren't broadcast to subscribers.
	st := r.stateFor(fed)
	management, managementKnown := r.managementClassForDispatch(ctx, fed, st, cls)
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

	deliveryTs := r.interactionDeliveryTime(fed, producer, cls, ts)
	if err := r.validateOutgoingTSO(fed, producer, deliveryTs); err != nil {
		return err
	}

	wireParams := copyParamMap(params)
	admission, err := r.reserveRetractionID(ctx, fed, producer, retractionHandle, deliveryTs)
	if err != nil {
		return err
	}
	if admission != nil {
		defer admission.Release()
	}
	if !scoped {
		subscribers = r.interactionSubscriptionCandidates(ctx, fed, cls)
	}
	fanout, err := r.prepareFanoutReceiveTo(ctx, fed, st, producer, cls, wireParams, deliveryTs, ts, retractionHandle, subscribers, metadata...)
	if err != nil {
		return fmt.Errorf("object: send interaction class %d in %q: reserve fanout: %w", cls, fed, err)
	}
	defer fanout.abort()

	if err := r.journalInteraction(ctx, fed, producer, cls, wireParams, ts); err != nil {
		return err
	}

	if err := fanout.commitSend(ctx, cls); err != nil {
		return err
	}
	if err := commitInteractionRetraction(admission); err != nil {
		return err
	}
	r.recordInteractionSent(fed, producer, cls, fanout.sentTransport)
	return nil
}

// SendInteractionWire implements core.WireApplier (W3): the LocalLRC
// direct-apply twin of sendInteraction. `params` arrives wire-shaped
// (uint64-keyed, exactly as the proto decoder produced it) and its
// OWNERSHIP TRANSFERS to the registry — see the core.WireApplier
// contract. It goes directly into the shared ReceiveInteraction
// envelope and the eventlog record, skipping the copyParamMap
// materialization (map AND byte-slice clones) the typed path pays;
// retraction is always 0 (LocalLRC ops carry no retraction handle).
//
// NOTE: map[uint64][]byte is NOT convertible to
// map[core.ParameterHandle][]byte, so the params validation loop below
// is a uint64-keyed twin and the (cold) management-dispatch branch
// re-boxes into the typed map the dispatcher contract requires. Keep
// this in lockstep with sendInteraction.
func (r *RegistryOf[T]) SendInteractionWire(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[uint64][]byte,
	ts *T,
) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	if err := validateMessageTimestampOf[T](ts); err != nil {
		return err
	}

	st := r.stateFor(fed)
	management, managementKnown := r.managementClassForDispatch(ctx, fed, st, cls)
	if err := r.validateInteractionHandlesWire(ctx, fed, st, cls, params); err != nil {
		return err
	}
	if managementKnown && management.isManager {
		// Cold path: management interactions re-box into the typed map
		// the dispatcher contract requires.
		typed := make(map[core.ParameterHandle][]byte, len(params))
		for h, v := range params {
			typed[core.ParameterHandle(h)] = v
		}
		return r.opts.ManagementDispatch.Dispatch(
			ctx, fed, management.name, producer, typed, management.fom, management.names,
		)
	}
	if !r.producerPublishesInteraction(ctx, fed, producer, cls) {
		return core.ErrInteractionClassNotPublished
	}

	deliveryTs := r.interactionDeliveryTime(fed, producer, cls, ts)
	if err := r.validateOutgoingTSO(fed, producer, deliveryTs); err != nil {
		return err
	}

	fanout, err := r.prepareFanoutReceive(ctx, fed, st, producer, cls, params, deliveryTs, ts, 0)
	if err != nil {
		return &core.UncommittedWireError{Err: fmt.Errorf("object: send interaction class %d in %q: reserve fanout: %w", cls, fed, err)}
	}
	defer fanout.abort()

	if err := r.journalInteraction(ctx, fed, producer, cls, params, ts); err != nil {
		return err
	}

	if err := fanout.commitSend(ctx, cls); err != nil {
		return err
	}
	r.recordInteractionSent(fed, producer, cls, fanout.sentTransport)
	return nil
}

func (r *RegistryOf[T]) validateInteractionHandles(
	ctx context.Context,
	fed core.FederationName,
	st *federationStateOf[T],
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

// validateInteractionHandlesWire is the uint64-keyed twin of
// validateInteractionHandles for the W3 wire apply path
// (map[uint64][]byte is not convertible to
// map[core.ParameterHandle][]byte). Keep the two in lockstep.
func (r *RegistryOf[T]) validateInteractionHandlesWire(
	ctx context.Context,
	fed core.FederationName,
	st *federationStateOf[T],
	cls core.InteractionClassHandle,
	params map[uint64][]byte,
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
		if _, found := names.ParameterName(cls, core.ParameterHandle(parameter)); !found {
			return core.ErrInteractionParameterNotFound
		}
	}
	return nil
}

// managementClass classifies an interaction once per federation and class.
// Federation FOMs are immutable after creation, so successful classifications
// and the handles required for manager dispatch remain valid for the state's
// lifetime. Failed FOM/name lookups are not cached, preserving retry behavior.
func (r *RegistryOf[T]) managementClass(
	ctx context.Context,
	fed core.FederationName,
	st *federationStateOf[T],
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
// deliveryTimestampForAttributes: FOM Receive applies in every profile.
func (r *RegistryOf[T]) deliveryTimestampForInteraction(
	fed core.FederationName,
	cls core.InteractionClassHandle,
	ts *T,
) *T {
	if ts == nil {
		return nil
	}
	if r.opts.Orders == nil {
		return ts
	}
	ord, known := r.opts.Orders.InteractionOrder(fed, cls)
	if !known || ord != OrderReceive {
		return ts
	}
	return nil
}

func (r *RegistryOf[T]) producerPublishesInteraction(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle) bool {
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

// receiveEventBlock packs the three per-subscriber receive-event
// allocations (outboundEvent adapter + FederateEvent + oneof wrapper)
// into ONE (W3) — the interaction twin of update.go's
// reflectEventBlock. Downstream consumers still see the same
// *outboundEvent concrete type as before.
func newReceiveEventOf[T core.LogicalTimeScalar](seq uint64, cls core.InteractionClassHandle, params map[uint64][]byte, ts *T) *CallbackEventOf[T] {
	return &CallbackEventOf[T]{
		Sequence:         seq,
		Kind:             CallbackReceive,
		InteractionClass: cls,
		Parameters:       params,
		LogicalTime:      ts,
	}
}

type bufferedInteractionDeliveryOf[T core.LogicalTimeScalar] struct {
	sub   core.FederateHandle
	ts    T
	event core.OutboundEvent
}

type preparedInteractionFanoutOf[T core.LogicalTimeScalar] struct {
	sentTransport   core.TransportType
	registry        *RegistryOf[T]
	fed             core.FederationName
	producer        core.FederateHandle
	retraction      uint64
	immediate       []core.OutboxDelivery
	immediateInline [1]core.OutboxDelivery
	buffered        []bufferedInteractionDeliveryOf[T]
	tsoDeliveries   []core.MessageDeliveryOf[T]
	reservation     core.OutboxReservation
	tsoReservation  core.TSOBufferReservationOf[T]
	finished        bool
	// directSend marks the W4 reservation-bypass fast path: EventLog nil,
	// exactly one immediate delivery, no TSO-buffered work. Commit sends
	// via Outbox.Send instead of committing a reservation, and the caller
	// wraps a commit failure with the SAME "reserve fanout" prefix the
	// Reserve path uses so the error surface is identical across branches.
	directSend bool
}

func (p *preparedInteractionFanoutOf[T]) initImmediate(capacity int) {
	if capacity <= 0 || cap(p.immediate) >= capacity {
		return
	}
	if capacity == 1 {
		p.immediate = p.immediateInline[:0]
		return
	}
	p.immediate = make([]core.OutboxDelivery, 0, capacity)
}

func (p *preparedInteractionFanoutOf[T]) appendImmediate(recipient core.FederateHandle, event core.OutboundEvent) {
	if cap(p.immediate) == 0 {
		p.immediate = p.immediateInline[:0]
	}
	p.immediate = append(p.immediate, core.OutboxDelivery{Recipient: recipient, Event: event})
}

func (r *RegistryOf[T]) prepareFanoutReceive(ctx context.Context, fed core.FederationName, st *federationStateOf[T], producer core.FederateHandle, cls core.InteractionClassHandle, params map[uint64][]byte, ts, originalTime *T, retractionHandle uint64) (*preparedInteractionFanoutOf[T], error) {
	subs := r.interactionSubscriptionCandidates(ctx, fed, cls)
	return r.prepareFanoutReceiveTo(ctx, fed, st, producer, cls, params, ts, originalTime, retractionHandle, subs)
}

func (r *RegistryOf[T]) prepareFanoutReceiveTo(ctx context.Context, fed core.FederationName, st *federationStateOf[T], producer core.FederateHandle, cls core.InteractionClassHandle, params map[uint64][]byte, ts, originalTime *T, retractionHandle uint64, subs []core.FederateHandle, metadata ...messageMetadata) (*preparedInteractionFanoutOf[T], error) {
	var delayed bool
	var candidateErr error
	st.managementClassMu.RLock()
	cachedFOM := st.managementClasses[cls].fom
	st.managementClassMu.RUnlock()
	subs, delayed, candidateErr = r.delayedSubscriptionCandidates(ctx, fed, subs, cachedFOM)
	if candidateErr != nil {
		return nil, candidateErr
	}
	// Hoist the inner proto and the param-map defensive copy out of
	// the per-subscriber loop. The producer's input map is copied once
	// (so the producer can mutate post-call); the resulting map and
	// the *ReceiveInteraction wrapping it are read-only from here on
	// and shared by every per-subscriber FederateEvent. proto.Marshal
	// does not mutate, so concurrent serialization across subscriber
	// streams is safe.
	eventTime := copiedLogicalTimeOf[T](originalTime)
	// Acquire a seq range once per fanout instead of once per
	// subscriber to cut N-1 lock acquires from the hot path.
	nSeq := 0
	for _, sub := range subs {
		if sub != producer {
			nSeq++
		}
	}
	st.mu.Lock()
	startSeq := st.nextOutboundSeqRangeLocked(nSeq)
	st.mu.Unlock()
	seq := startSeq
	prepared := &preparedInteractionFanoutOf[T]{registry: r, fed: fed, producer: producer, retraction: retractionHandle}
	_, canReserveTSO := r.opts.TSOGate.(core.ReservableTSODeliveryGateOf[T])
	useTSOReservation := canReserveTSO
	if useTSOReservation {
		prepared.tsoDeliveries = make([]core.MessageDeliveryOf[T], 0, nSeq)
	} else if ts == nil || r.opts.TSOGate == nil {
		prepared.initImmediate(nSeq)
	}
	transport := r.interactionTransportation(ctx, fed, producer, cls)
	prepared.sentTransport = transport
	prepared.prepareInteractionMessages(ctx, seq, cls, params, eventTime, ts, subs, delayed, useTSOReservation, metadata...)
	if useTSOReservation {
		if err := prepared.reserveInteractionTSO(ctx, ts); err != nil {
			return nil, err
		}
	}
	if err := prepared.reserveInteractionOutbox(ctx, useTSOReservation); err != nil {
		return nil, err
	}
	return prepared, nil
}

func (r *RegistryOf[T]) interactionSubscriptionCandidates(ctx context.Context, fed core.FederationName, cls core.InteractionClassHandle) []core.FederateHandle {
	classes := []core.InteractionClassHandle{cls}
	if model, err := r.subscriptionModel(ctx, fed, r.cachedInteractionFOM(fed, cls)); err == nil {
		if hierarchy, ok := model.(core.FOMClassAncestry); ok {
			classes = append(classes, hierarchy.InteractionClassAncestors(cls)...)
		}
	}
	var result []core.FederateHandle
	for _, class := range classes {
		result = append(result, r.interactionSubscribers(ctx, fed, class)...)
	}
	slices.Sort(result)
	return slices.Compact(result)
}

// Admission fixes the receiving class unless delayed evaluation was selected.
func (r *RegistryOf[T]) projectAdmittedInteraction(ctx context.Context, fed core.FederationName, sub core.FederateHandle, event *CallbackEventOf[T]) *CallbackEventOf[T] {
	if slices.Contains(r.interactionSubscribers(ctx, fed, event.InteractionClass), sub) {
		return event
	}
	model, err := r.subscriptionModel(ctx, fed, r.cachedInteractionFOM(fed, event.InteractionClass))
	hierarchy, ok := model.(core.FOMClassAncestry)
	if err != nil || !ok {
		return event
	}
	for _, class := range hierarchy.InteractionClassAncestors(event.InteractionClass) {
		if class == event.InteractionClass || !slices.Contains(r.interactionSubscribers(ctx, fed, class), sub) {
			continue
		}
		projectedEvent := *event
		metadata := *event.Metadata
		metadata.DelayedSubscription = true
		projectedEvent.Metadata = &metadata
		if projected, ok := r.projectDelayedInteraction(ctx, fed, &projectedEvent, class).(*CallbackEventOf[T]); ok {
			return projected
		}
	}
	return event
}

func (r *RegistryOf[T]) cachedInteractionFOM(fed core.FederationName, cls core.InteractionClassHandle) core.FOMHandle {
	st := r.stateFor(fed)
	st.managementClassMu.RLock()
	defer st.managementClassMu.RUnlock()
	return st.managementClasses[cls].fom
}

func (r *RegistryOf[T]) interactionSubscribers(
	ctx context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
) []core.FederateHandle {
	ordinary := r.ordinaryInteractionSubscribers(ctx, fed, cls)
	if routing, ok := r.opts.RegistrationRegions.(interface {
		InteractionSubscribersForDefaultRegion(context.Context, core.FederationName, core.InteractionClassHandle) []core.FederateHandle
	}); ok {
		regional := routing.InteractionSubscribersForDefaultRegion(ctx, fed, cls)
		if len(regional) != 0 {
			// Declaration snapshots are immutable and must never be appended to.
			union := append(slices.Clone(ordinary), regional...)
			slices.Sort(union)
			return slices.Compact(union)
		}
	}
	return ordinary
}

func (r *RegistryOf[T]) ordinaryInteractionSubscribers(
	ctx context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
) []core.FederateHandle {
	if snapshots, ok := r.opts.Declarations.(interface {
		InteractionSubscribersSnapshot(
			context.Context,
			core.FederationName,
			core.InteractionClassHandle,
		) []core.FederateHandle
	}); ok {
		return snapshots.InteractionSubscribersSnapshot(ctx, fed, cls)
	}
	return r.opts.Declarations.InteractionSubscribersFor(ctx, fed, cls)
}

func (p *preparedInteractionFanoutOf[T]) commit(ctx context.Context) error {
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
		for _, delivery := range p.immediate {
			if err := p.registry.opts.Outbox.Send(ctx, p.fed, delivery.Recipient, delivery.Event); err != nil {
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
	p.recordInteractionDeliveries()
	p.finished = true
	return nil
}

func (p *preparedInteractionFanoutOf[T]) abort() {
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

// interactionSentRecordBlock packs the four eventlog-record
// allocations into ONE (W3) — the interaction twin of update.go's
// attributeUpdatedRecordBlock.
// newInteractionSentEvent builds the eventlog adapter for an
// InteractionSent event in a single allocation. Fields are assigned
// individually (never whole-message copies).
func newInteractionSentEventOf[T core.LogicalTimeScalar](cls core.InteractionClassHandle, producer core.FederateHandle, params map[uint64][]byte, ts *T, wallNs int64) *core.ObjectJournalRecordOf[T] {
	return &core.ObjectJournalRecordOf[T]{
		Kind:             JournalInteractionSent,
		WallNS:           journalWallBits(wallNs),
		Federate:         producer,
		InteractionClass: cls,
		Parameters:       params,
		LogicalTime:      copiedLogicalTimeOf[T](ts),
	}
}

func (r *RegistryOf[T]) recordInteractionSent(fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle, transport core.TransportType) {
	if r.opts.OnInteractionSent != nil {
		r.opts.OnInteractionSent(fed, producer)
	}
	if r.opts.OnInteractionTraffic != nil {
		r.opts.OnInteractionTraffic(fed, producer, cls, false)
	}
	if hook := r.opts.OnInteractionSentTransport; hook != nil {
		hook(fed, producer, cls, transport)
	}
}

func (r *RegistryOf[T]) journalInteraction(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle, params map[uint64][]byte, ts *T) error {
	if r.opts.EventLog != nil {
		ev := newInteractionSentEventOf[T](cls, producer, params, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			return fmt.Errorf("object: send interaction class %d in %q: eventlog append: %w", cls, fed, err)
		}
	}
	return nil
}

func (p *preparedInteractionFanoutOf[T]) commitSend(ctx context.Context, cls core.InteractionClassHandle) error {
	if err := p.commit(ctx); err != nil {
		if p.directSend {
			// Direct send performs admission at commit; preserve its error boundary.
			return fmt.Errorf("object: send interaction class %d in %q: reserve fanout: %w", cls, p.fed, err)
		}
		return fmt.Errorf("object: send interaction class %d in %q: fanout: %w", cls, p.fed, err)
	}
	return nil
}

func (p *preparedInteractionFanoutOf[T]) prepareInteractionMessages(ctx context.Context, seq uint64, cls core.InteractionClassHandle, params map[uint64][]byte, eventTime, ts *T, subs []core.FederateHandle, delayed, useTSOReservation bool, metadata ...messageMetadata) {
	r, fed, producer, retractionHandle, transport := p.registry, p.fed, p.producer, p.retraction, p.sentTransport
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		evt := newReceiveEventOf[T](seq, cls, params, eventTime)
		evt.Metadata = metadataForOf[T](producer, ts, transport)
		evt.Metadata.DelayedSubscription = delayed
		if ts != nil {
			evt.Metadata.RetractionHandle = retractionHandle
		}
		if len(metadata) > 0 {
			evt.UserSuppliedTag = metadata[0].tag
			evt.Metadata.Regions = metadata[0].regions
			evt.Metadata.ConveyedRegions = slices.Clone(metadata[0].conveyed)
		}
		seq++
		if !delayed {
			evt = r.projectAdmittedInteraction(ctx, fed, sub, evt)
		}
		if useTSOReservation {
			p.tsoDeliveries = append(p.tsoDeliveries, messageDeliveryOf[T](sub, producer, ts, evt, retractionHandle))
			continue
		}
		if ts != nil && r.opts.TSOGate != nil && !r.opts.TSOGate.ShouldDeliverNow(fed, sub, *ts) {
			p.buffered = append(p.buffered, bufferedInteractionDeliveryOf[T]{sub: sub, ts: *ts, event: evt})
		} else {
			if projected := r.projectImmediateDelayed(ctx, fed, sub, evt); projected != nil {
				p.appendImmediate(sub, projected)
			}
		}
	}
}

func (p *preparedInteractionFanoutOf[T]) reserveInteractionTSO(ctx context.Context, ts *T) error {
	r, fed, producer := p.registry, p.fed, p.producer
	var err error
	p.tsoReservation, err = r.reserveOutgoingTSO(fed, producer, ts, p.tsoDeliveries)
	if err != nil {
		return err
	}
	if p.tsoReservation == nil {
		return fmt.Errorf("reservable TSO gate returned a nil reservation")
	}
	immediate := p.tsoReservation.Immediate()
	p.initImmediate(len(immediate))
	for _, delivery := range immediate {
		if projected := r.projectImmediateDelayed(ctx, fed, delivery.Recipient, delivery.Event); projected != nil {
			p.appendImmediate(delivery.Recipient, projected)
		}
	}
	return nil
}

func (p *preparedInteractionFanoutOf[T]) reserveInteractionOutbox(ctx context.Context, useTSOReservation bool) error {
	r, fed := p.registry, p.fed
	if reservable, ok := r.opts.Outbox.(core.ReservableOutbox); ok && len(p.immediate) > 0 {
		// W4 fast path: the outbox reservation exists solely to span the
		// write-ahead append atomically. With no EventLog, a single
		// immediate delivery, and no TSO-buffered work there is nothing
		// to make atomic — commit calls Outbox.Send directly at the
		// exact program point Reserve+Commit would have delivered,
		// skipping the reservation allocation and its recipient-lock
		// round trip.
		if r.opts.EventLog == nil && len(p.immediate) == 1 &&
			len(p.buffered) == 0 && p.tsoReservation == nil {
			p.directSend = true
			return nil
		}
		reservation, err := reservable.Reserve(ctx, fed, p.immediate)
		if err != nil {
			if p.tsoReservation != nil {
				p.tsoReservation.Release()
			}
			return err
		}
		if reservation == nil {
			p.abort()
			return fmt.Errorf("reservable outbox returned a nil reservation")
		}
		p.reservation = reservation
	} else if useTSOReservation && len(p.immediate) > 1 {
		p.abort()
		return fmt.Errorf("atomic interaction fanout requires a reservable outbox")
	}
	return nil
}

func (p *preparedInteractionFanoutOf[T]) recordInteractionDeliveries() {
	if p.registry.opts.OnInteractionDelivered != nil {
		if len(p.tsoDeliveries) > 0 {
			for _, delivery := range p.tsoDeliveries {
				p.registry.opts.OnInteractionDelivered(p.fed, delivery.Recipient)
			}
		} else {
			for _, delivery := range p.immediate {
				p.registry.opts.OnInteractionDelivered(p.fed, delivery.Recipient)
			}
			for _, delivery := range p.buffered {
				p.registry.opts.OnInteractionDelivered(p.fed, delivery.sub)
			}
		}
	}
	if hook := p.registry.opts.OnInteractionTraffic; hook != nil {
		if len(p.tsoDeliveries) > 0 {
			for _, delivery := range p.tsoDeliveries {
				hook(p.fed, delivery.Recipient, delivery.Event.(*CallbackEventOf[T]).InteractionClass, true)
			}
		} else {
			for _, delivery := range p.immediate {
				hook(p.fed, delivery.Recipient, delivery.Event.(*CallbackEventOf[T]).InteractionClass, true)
			}
			for _, delivery := range p.buffered {
				hook(p.fed, delivery.sub, delivery.event.(*CallbackEventOf[T]).InteractionClass, true)
			}
		}
	}
}

func (r *RegistryOf[T]) managementClassForDispatch(ctx context.Context, fed core.FederationName, st *federationStateOf[T], cls core.InteractionClassHandle) (managementClassClassification, bool) {
	if r.opts.ManagementDispatch != nil {
		return r.managementClass(ctx, fed, st, cls)
	}
	return managementClassClassification{}, false
}

func commitInteractionRetraction(admission core.OutboxReservation) error {
	if admission != nil {
		if err := admission.Commit(); err != nil {
			return fmt.Errorf("object: interaction delivered but retraction admission commit failed: %w", err)
		}
	}
	return nil
}
