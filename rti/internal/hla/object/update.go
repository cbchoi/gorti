package object

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// internalMOMProducer mirrors mom.momProducer (max-uint64): the
// FederateHandle the RTI itself uses to produce HLAmanager object
// instances (IEEE 1516.1-2010 §11, M36 DD-2). It can never collide
// with real federate handles (the federation manager allocates small
// monotonic handles starting at 1). The §6.6 per-instance ownership
// gate exempts it — see updateAttributes (M38 GB).
const internalMOMProducer = ^core.FederateHandle(0)

// Compile-time assertion: the composed Registry offers the optional W3
// LocalLRC direct-apply fast path.
var _ core.WireApplier = (*Registry)(nil)

// Order is the preferred attribute or interaction order declared in the FOM.
// It applies independently of the federation's transport profile.
type Order uint8

const (
	// OrderTimeStamp is the FOM-default ordering. Outbound events keep
	// their timestamp (TSO).
	OrderTimeStamp Order = iota
	// OrderReceive selects RO delivery while retaining any supplied timestamp.
	OrderReceive
)

// FederationModeLookup answers "what mode is this federation in?".
// The federation manager satisfies this contract directly via its
// ModeFor method; tests can supply a stub.
//
// Kept for embedded callers; preferred message order does not depend on mode.
type FederationModeLookup interface {
	ModeFor(fed core.FederationName) (core.Mode, bool)
}

// AttributeOrderLookup answers "what is this attribute / interaction
// declared with in the FOM?". Returning (OrderTimeStamp, false)
// signals an unknown declaration; the registry retains the TimeStamp default.
//
// Optional in object.Options: when nil, the registry assumes
// OrderTimeStamp for every attribute (existing TSO-only behavior
// preserved).
type AttributeOrderLookup interface {
	AttributeOrder(fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle) (Order, bool)
	InteractionOrder(fed core.FederationName, cls core.InteractionClassHandle) (Order, bool)
}

// OutgoingTSOValidator validates a TSO send's timestamp against the
// sender's time-regulation state (M37 EB-3 / IEEE 1516.1-2010 §8.1.2:
// ts >= currentTime + lookahead for regulating senders; non-regulating
// senders are exempt). time.Manager satisfies this contract directly;
// tests supply a stub.
//
// Optional in object.Options: when nil, no validation runs (pre-M37
// behavior preserved for fixtures without a time manager).
type OutgoingTSOValidatorOf[T core.LogicalTimeScalar] interface {
	ValidateOutgoingTSO(fed core.FederationName, sender core.FederateHandle, ts T) error
}

// RO retains the original time even though it has no TSO ordering bound.
// Keep the existing positive-infinity representation accepted by the time core.
func validateMessageTimestampOf[T core.LogicalTimeScalar](ts *T) error {
	if ts != nil && (math.IsNaN(float64(*ts)) || *ts < 0) {
		return core.ErrTimeInvalidLogicalTime
	}
	return nil
}

// validateOutgoingTSO consults the optional TSOValidator for a TSO
// send (non-nil ts). RO sends bypass. Shared by the update /
// interaction / delete ingestion points.
func (r *RegistryOf[T]) validateOutgoingTSO(fed core.FederationName, sender core.FederateHandle, ts *T) error {
	if ts == nil || r.opts.TSOValidator == nil {
		return nil
	}
	return r.opts.TSOValidator.ValidateOutgoingTSO(fed, sender, *ts)
}

// updateAttributes implements core.ObjectRegistry.UpdateAttributes.
//
// Flow:
//  1. Look up the instance; reject unknown with ErrObjectNotFound.
//  2. Verify the producer publishes EVERY attribute in the update via
//     declaration.Manager. (Cut 1: any-attr ownership; full
//     ownership-management arrives in cut 2 with explicit Acquire/Divest.)
//  3. Partition attributes by effective preferred order and transportation,
//     applying instance overrides before FOM defaults in every profile.
//  4. Resolve and reserve the complete callback set, including DDM scope
//     advisories, immediate recipients, and TSO-buffer admission.
//  5. Persist AttributeUpdated to the EventLog (write-ahead).
//  6. Commit callbacks in advisory-before-reflect, sorted-recipient order.
//
// Cut-1 simplification: attribute bytes are passed through as-is. The
// CodecFactory dependency is reserved for the gRPC layer's transition
// path; the registry sees pre-encoded bytes from the client (which the
// PySDK / Go SDK encode via rti/pkg/encoding before sending).
func (r *RegistryOf[T]) updateAttributes(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[core.AttributeHandle][]byte,
	ts *T,
	retractionHandle uint64,
	metadata ...messageMetadata,
) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	st := r.stateFor(fed)
	updateAttrs := sortedAttrHandles(attrs)
	inst, known, mapped, err := r.prepareUpdateObject(ctx, fed, st, producer, obj, updateAttrs)
	if err != nil {
		return err
	}

	if !r.producerOwnsAllAttrs(ctx, fed, producer, known, attrs) {
		return core.ErrAttributeNotOwned
	}
	if known != inst.cls {
		canonical := make(map[core.AttributeHandle][]byte, len(attrs))
		for i, attr := range updateAttrs {
			canonical[mapped[i]] = attrs[attr]
		}
		attrs = canonical
		updateAttrs = sortedAttrHandles(attrs)
	}

	// M38 GB — §6.6 precondition: the producer must be the CURRENT §7
	// owner of EVERY instance-attribute in the update. The publication
	// probe above is the §5 class-level condition — necessary, not
	// sufficient — so without this gate a federate that divested an
	// attribute (or never acquired it) could keep updating it.
	//
	// Runs AFTER st.mu is released and takes only the ownership
	// manager's own RLock per attribute (a map hit, no scan) — no
	// registry↔manager lock nesting (M36 DD re-entry discipline).
	// The RTI-internal MOM producer is exempt: its §11 HLAmanager
	// reflects are RTI-maintained state whose FOM-resolved attribute
	// handles may lie outside the cut-1 ownership-seeding probe range
	// (see initialOwnedAttrs / fanoutAttrProbe).
	if err := r.validateUpdateOwnership(fed, producer, obj, updateAttrs); err != nil {
		return err
	}

	// One wire-shaped copy serves BOTH the outbound reflect envelope and
	// the eventlog record (W3): the map is read-only after this point
	// (proto.Marshal does not mutate), so sharing it is safe, and the
	// caller keeps the right to mutate its input map post-call.
	wireAttrs := copyAttrMap(attrs)
	fanout, err := r.prepareFanoutReflect(
		ctx, fed, st, producer, inst, wireAttrs, updateAttrs, ts, retractionHandle, metadata...,
	)
	if err != nil {
		return fmt.Errorf("object: update %d in %q: reserve fanout: %w", obj, fed, err)
	}
	defer fanout.abort()

	if r.opts.EventLog != nil {
		ev := newAttributeUpdatedEventOf[T](obj, producer, wireAttrs, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			return fmt.Errorf("object: update %d in %q: eventlog append: %w", obj, fed, err)
		}
	}

	if err := fanout.commit(ctx); err != nil {
		if fanout.directSend {
			// W4: the fast path moves admission from Reserve to the
			// commit-time Send; keep the reservation branch's error
			// surface (prefix and errors.Is chain) bit-identical.
			return fmt.Errorf("object: update %d in %q: reserve fanout: %w", obj, fed, err)
		}
		return fmt.Errorf("object: update %d in %q: fanout: %w", obj, fed, err)
	}
	if r.opts.OnUpdateSent != nil {
		r.opts.OnUpdateSent(fed, producer)
	}
	if r.opts.OnObjectTraffic != nil {
		r.opts.OnObjectTraffic(fed, producer, inst.cls, false)
	}
	fanout.recordSentTransport()
	if r.opts.OnObjectInstanceTraffic != nil {
		r.opts.OnObjectInstanceTraffic(fed, producer, obj, inst.cls, false)
	}
	return nil
}

// UpdateAttributesWire implements core.WireApplier (W3): the LocalLRC
// direct-apply twin of updateAttributes. `attrs` arrives wire-shaped
// (uint64-keyed, exactly as the proto decoder produced it) and its
// OWNERSHIP TRANSFERS to the registry — see the core.WireApplier
// contract. The map goes directly into the outbound
// ReflectAttributeValues envelope and the eventlog record without any
// copyAttrMap materialization; retraction is always 0 (LocalLRC ops
// carry no retraction handle).
//
// NOTE: map[uint64][]byte is NOT convertible to
// map[core.AttributeHandle][]byte, so the publication/ownership attr
// loops and the sorted-handle build below are uint64-keyed twins of
// the typed-path helpers. Keep them in lockstep with updateAttributes.
func (r *RegistryOf[T]) UpdateAttributesWire(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[uint64][]byte,
	ts *T,
) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	st := r.stateFor(fed)
	updateAttrs := sortedAttrHandlesWire(attrs)
	inst, known, mapped, err := r.prepareUpdateObject(ctx, fed, st, producer, obj, updateAttrs)
	if err != nil {
		return err
	}

	if !r.producerOwnsAllAttrsWire(ctx, fed, producer, known, attrs) {
		return core.ErrAttributeNotOwned
	}
	if known != inst.cls {
		canonical := make(map[uint64][]byte, len(attrs))
		for i, attr := range updateAttrs {
			canonical[uint64(mapped[i])] = attrs[uint64(attr)]
		}
		attrs = canonical
		updateAttrs = sortedAttrHandlesWire(attrs)
	}

	// M38 GB §6.6 per-instance ownership gate — uint64-keyed twin of the
	// updateAttributes loop (same MOM-producer exemption).
	if err := r.validateUpdateOwnership(fed, producer, obj, updateAttrs); err != nil {
		return err
	}

	fanout, err := r.prepareFanoutReflect(
		ctx, fed, st, producer, inst, attrs, updateAttrs, ts, 0,
	)
	if err != nil {
		return &core.UncommittedWireError{Err: fmt.Errorf("object: update %d in %q: reserve fanout: %w", obj, fed, err)}
	}
	defer fanout.abort()

	if r.opts.EventLog != nil {
		ev := newAttributeUpdatedEventOf[T](obj, producer, attrs, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			return fmt.Errorf("object: update %d in %q: eventlog append: %w", obj, fed, err)
		}
	}

	if err := fanout.commit(ctx); err != nil {
		if fanout.directSend {
			// W4: identical error surface across the fast-Send and
			// Reserve branches — see updateAttributes.
			return fmt.Errorf("object: update %d in %q: reserve fanout: %w", obj, fed, err)
		}
		return fmt.Errorf("object: update %d in %q: fanout: %w", obj, fed, err)
	}
	if r.opts.OnUpdateSent != nil {
		r.opts.OnUpdateSent(fed, producer)
	}
	if r.opts.OnObjectTraffic != nil {
		r.opts.OnObjectTraffic(fed, producer, inst.cls, false)
	}
	fanout.recordSentTransport()
	if r.opts.OnObjectInstanceTraffic != nil {
		r.opts.OnObjectInstanceTraffic(fed, producer, obj, inst.cls, false)
	}
	return nil
}

// deliveryTimestampForAttributes resolves the timestamp the registry
// uses as the ordering key. FOM Receive applies in every profile; callbacks
// retain the original timestamp separately. Missing declarations retain the
// TimeStamp default. attributeDeliveries partitions mixed attribute orders.
func (r *RegistryOf[T]) deliveryTimestampForAttributes(
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
	ts *T,
) *T {
	if ts == nil {
		// Producer already chose RO; nothing to strip.
		return nil
	}
	if r.opts.Orders == nil {
		return ts
	}
	for _, a := range attrs {
		ord, known := r.opts.Orders.AttributeOrder(fed, cls, a)
		if !known || ord != OrderReceive {
			return ts
		}
	}
	return nil
}

// producerOwnsAllAttrs returns true iff `producer` publishes every
// attribute in `attrs` for `cls` in `fed`. The all-or-nothing check
// matches HLA's per-attribute ownership semantics: a single unowned
// attribute in the bundle invalidates the whole UpdateAttributeValues
// call.
func (r *RegistryOf[T]) producerOwnsAllAttrs(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte) bool {
	if len(attrs) == 0 {
		return false
	}
	if lookup, ok := r.opts.Declarations.(interface {
		PublishesObjectAttribute(
			context.Context,
			core.FederationName,
			core.ObjectClassHandle,
			core.AttributeHandle,
			core.FederateHandle,
		) bool
	}); ok {
		for attr := range attrs {
			if !lookup.PublishesObjectAttribute(ctx, fed, cls, attr, producer) {
				return false
			}
		}
		return true
	}
	for attr := range attrs {
		owned := false
		for _, h := range r.opts.Declarations.PublishersFor(ctx, fed, cls, attr) {
			if h == producer {
				owned = true
				break
			}
		}
		if !owned {
			return false
		}
	}
	return true
}

// sortedAttrHandles returns attrs's keys in ascending order. Used to feed
// SubscribersFor deterministically and to stamp the eventlog payload's
// iteration order.
func sortedAttrHandles(attrs map[core.AttributeHandle][]byte) []core.AttributeHandle {
	out := make([]core.AttributeHandle, 0, len(attrs))
	for h := range attrs {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}

// sortedAttrHandlesWire is the uint64-keyed twin of sortedAttrHandles
// for the W3 wire apply path (map[uint64][]byte is not convertible to
// map[core.AttributeHandle][]byte).
func sortedAttrHandlesWire(attrs map[uint64][]byte) []core.AttributeHandle {
	out := make([]core.AttributeHandle, 0, len(attrs))
	for h := range attrs {
		out = append(out, core.AttributeHandle(h))
	}
	slices.Sort(out)
	return out
}

// producerOwnsAllAttrsWire is the uint64-keyed twin of
// producerOwnsAllAttrs for the W3 wire apply path. Keep the two in
// lockstep.
func (r *RegistryOf[T]) producerOwnsAllAttrsWire(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, attrs map[uint64][]byte) bool {
	if len(attrs) == 0 {
		return false
	}
	if lookup, ok := r.opts.Declarations.(interface {
		PublishesObjectAttribute(
			context.Context,
			core.FederationName,
			core.ObjectClassHandle,
			core.AttributeHandle,
			core.FederateHandle,
		) bool
	}); ok {
		for attr := range attrs {
			if !lookup.PublishesObjectAttribute(ctx, fed, cls, core.AttributeHandle(attr), producer) {
				return false
			}
		}
		return true
	}
	for attr := range attrs {
		owned := false
		for _, h := range r.opts.Declarations.PublishersFor(ctx, fed, cls, core.AttributeHandle(attr)) {
			if h == producer {
				owned = true
				break
			}
		}
		if !owned {
			return false
		}
	}
	return true
}

type bufferedAttributeDeliveryOf[T core.LogicalTimeScalar] struct {
	sub   core.FederateHandle
	ts    T
	event core.OutboundEvent
}

type preparedAttributeScopeOf[T core.LogicalTimeScalar] struct {
	object   core.ObjectHandle
	attrs    []core.AttributeHandle
	desired  map[core.AttributeHandle]map[core.FederateHandle]struct{}
	newlyIn  map[core.FederateHandle][]core.AttributeHandle
	newlyOut map[core.FederateHandle][]core.AttributeHandle
}

type preparedAttributeFanoutOf[T core.LogicalTimeScalar] struct {
	sentTransports      [2]bool
	instance            *objectInstance
	discoveries         []discoverKey
	discoveryClasses    map[discoverKey]core.ObjectClassHandle
	knowledgePublished  bool
	deliveryLocked      bool
	class               core.ObjectClassHandle
	registry            *RegistryOf[T]
	state               *federationStateOf[T]
	fed                 core.FederationName
	producer            core.FederateHandle
	retraction          uint64
	retractionAdmission core.OutboxReservation
	immediate           []core.OutboxDelivery
	immediateInline     [1]core.OutboxDelivery
	buffered            []bufferedAttributeDeliveryOf[T]
	reflectSubs         []core.FederateHandle
	reflectSubsInline   [1]core.FederateHandle
	reservation         core.OutboxReservation
	tsoReservation      core.TSOBufferReservationOf[T]
	scope               *preparedAttributeScopeOf[T]
	finished            bool
	// directSend marks the W4 reservation-bypass fast path: EventLog nil,
	// exactly one immediate delivery, no TSO-buffered work. Commit sends
	// via Outbox.Send instead of committing a reservation, and the caller
	// wraps a commit failure with the SAME "reserve fanout" prefix the
	// Reserve path uses so the error surface is identical across branches.
	directSend bool
}

func (p *preparedAttributeFanoutOf[T]) initImmediate(capacity int) {
	if capacity <= 0 || cap(p.immediate) >= capacity {
		return
	}
	if capacity == 1 {
		p.immediate = p.immediateInline[:0]
		return
	}
	p.immediate = make([]core.OutboxDelivery, 0, capacity)
}

func (p *preparedAttributeFanoutOf[T]) appendImmediate(delivery core.OutboxDelivery) {
	if cap(p.immediate) == 0 {
		p.immediate = p.immediateInline[:0]
	}
	p.immediate = append(p.immediate, delivery)
}

func (p *preparedAttributeFanoutOf[T]) initReflectSubs(capacity int) {
	if capacity <= 0 {
		return
	}
	if capacity == 1 {
		p.reflectSubs = p.reflectSubsInline[:0]
		return
	}
	p.reflectSubs = make([]core.FederateHandle, 0, capacity)
}

func prepareAttributeScopeLockedOf[T core.LogicalTimeScalar](
	st *federationStateOf[T],
	producer core.FederateHandle,
	object core.ObjectHandle,
	updateAttrs []core.AttributeHandle,
	perAttr map[core.AttributeHandle][]core.FederateHandle,
) *preparedAttributeScopeOf[T] {
	prepared := &preparedAttributeScopeOf[T]{
		object:   object,
		attrs:    append([]core.AttributeHandle(nil), updateAttrs...),
		desired:  make(map[core.AttributeHandle]map[core.FederateHandle]struct{}, len(updateAttrs)),
		newlyIn:  map[core.FederateHandle][]core.AttributeHandle{},
		newlyOut: map[core.FederateHandle][]core.AttributeHandle{},
	}
	var objScope map[core.FederateHandle]map[core.AttributeHandle]struct{}
	if st.scope != nil {
		objScope = st.scope[object]
	}
	for _, attr := range updateAttrs {
		current := map[core.FederateHandle]struct{}{}
		prepared.desired[attr] = current
		for _, h := range perAttr[attr] {
			if h == producer {
				continue
			}
			current[h] = struct{}{}
			if _, was := objScope[h][attr]; !was {
				prepared.newlyIn[h] = append(prepared.newlyIn[h], attr)
			}
		}
		for h, set := range objScope {
			if _, was := set[attr]; !was {
				continue
			}
			if _, still := current[h]; still {
				continue
			}
			prepared.newlyOut[h] = append(prepared.newlyOut[h], attr)
		}
	}
	return prepared
}

func (p *preparedAttributeScopeOf[T]) applyLocked(st *federationStateOf[T]) {
	if st.scope == nil {
		st.scope = map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle]struct{}{}
	}
	objScope := st.scope[p.object]
	if objScope == nil {
		objScope = map[core.FederateHandle]map[core.AttributeHandle]struct{}{}
		st.scope[p.object] = objScope
	}
	for _, attr := range p.attrs {
		current := p.desired[attr]
		for h := range current {
			set := objScope[h]
			if set == nil {
				set = map[core.AttributeHandle]struct{}{}
				objScope[h] = set
			}
			set[attr] = struct{}{}
		}
		for h, set := range objScope {
			if _, was := set[attr]; !was {
				continue
			}
			if _, still := current[h]; still {
				continue
			}
			delete(set, attr)
			if len(set) == 0 {
				delete(objScope, h)
			}
		}
	}
}

// reflectEventBlock packs the three per-subscriber reflect-event
// allocations (outboundEvent adapter + FederateEvent + oneof wrapper)
// into ONE (W3). Fields are wired pointer-to-sibling-field at
// construction and never reassigned; downstream consumers still see
// the exact same *outboundEvent concrete type as before.
func newReflectEventOf[T core.LogicalTimeScalar](seq uint64, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[uint64][]byte, ts *T) *CallbackEventOf[T] {
	return &CallbackEventOf[T]{
		Sequence:    seq,
		Kind:        CallbackReflect,
		Object:      obj,
		ObjectClass: cls,
		Attributes:  attrs,
		LogicalTime: ts,
	}
}

// ReflectionObject lets the time queue preserve a known object until its
// timestamp-equal attribute reflections have been delivered before removal.
func (e *CallbackEventOf[T]) ReflectionObject() core.ObjectHandle {
	if e != nil && e.Kind == CallbackReflect {
		return e.Object
	}
	return 0
}

// prepareFanoutReflect admits the complete callback set before the update's
// write-ahead append. Scope advisories retain their legacy position before
// reflects, while the scope cache remains unchanged until commit succeeds.
//
// wireAttrs is the READY-TO-SHIP wire-shaped attribute map: it is
// stored directly into the shared ReflectAttributeValues envelope
// without another copy (W3), so it must already be safe to alias —
// either a copyAttrMap materialization (typed path) or an
// ownership-transferred decode map (core.WireApplier path). It is
// read-only from here on.
func (r *RegistryOf[T]) prepareFanoutReflect(
	ctx context.Context,
	fed core.FederationName,
	st *federationStateOf[T],
	producer core.FederateHandle,
	inst *objectInstance,
	wireAttrs map[uint64][]byte,
	updateAttrs []core.AttributeHandle,
	ts *T,
	retractionHandle uint64,
	metadata ...messageMetadata,
) (*preparedAttributeFanoutOf[T], error) {
	if err := validateMessageTimestampOf[T](ts); err != nil {
		return nil, err
	}
	prepared := &preparedAttributeFanoutOf[T]{
		instance:   inst,
		class:      inst.cls,
		registry:   r,
		state:      st,
		fed:        fed,
		producer:   producer,
		retraction: retractionHandle,
	}
	inst.deliveryMu.Lock()
	prepared.deliveryLocked = true
	pendingDeletion, err := prepared.validateReflectionInstance(ts)
	if err != nil {
		prepared.abort()
		return nil, err
	}
	classes := r.recipientClasses(ctx, fed, inst, updateAttrs...)
	subs, perAttr, snapshots, snapshotErr := r.snapshotReflectRecipients(ctx, fed, inst, updateAttrs, classes)
	if snapshotErr != nil {
		prepared.abort()
		return nil, snapshotErr
	}
	prepared.prepareReflectionKnowledge(subs, perAttr, updateAttrs, classes)

	candidates, delayed, candidateErr := r.delayedSubscriptionCandidates(ctx, fed, subs)
	if candidateErr != nil {
		prepared.abort()
		return nil, candidateErr
	}
	if delayed {
		// Keep discovery and scope based on current interest, but retain every
		// canonical attribute for delivery-time projection of joined candidates.
		subs = candidates
		perAttr = nil
	}
	observe := prepared.sentTransportObserver()
	parts, timestamped := r.attributeDeliveriesObserved(ctx, fed, producer, inst, wireAttrs, updateAttrs, subs, perAttr, ts, observe, snapshots)
	validationTime, err := prepared.prepareReflectionTime(ctx, ts, timestamped)
	if err != nil {
		prepared.abort()
		return nil, err
	}
	nReflect := len(parts)
	scopeAdvisories, nAdvisory := prepared.reflectionAdvisories()
	nSeq := len(prepared.discoveries) + nAdvisory + nReflect
	if pendingDeletion != nil {
		nSeq += len(prepared.discoveries)
	}
	_, canReserveTSO := r.opts.TSOGate.(core.ReservableTSODeliveryGateOf[T])
	useTSOGate := canReserveTSO || ts != nil && r.opts.TSOGate != nil
	if !useTSOGate {
		prepared.initImmediate(nSeq)
	} else {
		prepared.initImmediate(len(prepared.discoveries) + nAdvisory)
	}
	prepared.initReflectSubs(nReflect)
	var seq uint64
	if nSeq > 0 {
		st.mu.Lock()
		seq = st.nextOutboundSeqRangeLocked(nSeq)
		st.mu.Unlock()
	}

	seq = prepared.appendReflectionAdvisories(ctx, seq, classes, scopeAdvisories)
	reflects := prepared.reflectionMessages(ctx, seq, parts, classes, delayed, ts, canReserveTSO, useTSOGate, pendingDeletion, metadata...)
	if useTSOGate {
		if err := prepared.prepareReflectionTSO(ctx, validationTime, reflects, canReserveTSO); err != nil {
			prepared.abort()
			return nil, err
		}
	}

	prepared.reduceImmediateBestEffort(ctx)

	if err := prepared.reserveReflectionOutbox(ctx); err != nil {
		prepared.abort()
		return nil, err
	}
	return prepared, nil
}

func (p *preparedAttributeFanoutOf[T]) commit(ctx context.Context) error {
	if p.finished {
		return nil
	}
	if len(p.discoveries) > 0 {
		p.state.mu.Lock()
		for _, key := range p.discoveries {
			p.state.discovered[key] = p.discoveryClasses[key]
		}
		p.knowledgePublished = true
		p.state.mu.Unlock()
	}
	deletion := p.registry.PrepareRemovalDelivery(p.fed, p.immediate)
	if deletion != nil {
		defer deletion.Release()
	}
	// The per-object delivery lock preserves scope and knowledge ordering while
	// read-only queries can observe state from inside a delivered callback.
	// Match the interaction fanout lock order: release outbox recipient locks
	// before the TSO gate may synchronously re-evaluate a pending grant.
	if err := p.commitImmediateReflections(ctx); err != nil {
		return err
	}
	if deletion != nil {
		if err := deletion.Commit(); err != nil {
			return err
		}
	}
	if err := p.commitBufferedReflections(ctx); err != nil {
		return err
	}
	if p.scope != nil {
		p.state.mu.Lock()
		if p.state.instances[p.instance.handle] == p.instance {
			p.scope.applyLocked(p.state)
		}
		p.state.mu.Unlock()
	}
	if p.retractionAdmission != nil {
		if err := p.retractionAdmission.Commit(); err != nil {
			return fmt.Errorf("object: update delivered but retraction admission commit failed: %w", err)
		}
		p.retractionAdmission = nil
	}
	p.unlockDelivery()
	p.finished = true
	p.registry.FinishDeletionDelivery(p.fed)
	p.recordReflectionTraffic()
	return nil
}

func (r *RegistryOf[T]) attributeScopeAdvisoryEnabled(fed core.FederationName, recipient core.FederateHandle) bool {
	if switches, ok := r.opts.Declarations.(core.AdvisorySwitchReader); ok {
		return switches.AdvisorySwitchEnabled(fed, recipient, core.AdvisoryAttributeScope)
	}
	return true
}

func (p *preparedAttributeFanoutOf[T]) abort() {
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
	if p.retractionAdmission != nil {
		p.retractionAdmission.Release()
		p.retractionAdmission = nil
	}
	if p.knowledgePublished {
		p.state.mu.Lock()
		for _, key := range p.discoveries {
			p.state.forgetKnowledgeLocked(key)
		}
		p.state.mu.Unlock()
	}
	p.unlockDelivery()
	p.finished = true
}

func (p *preparedAttributeFanoutOf[T]) unlockDelivery() {
	if p.deliveryLocked {
		p.instance.deliveryMu.Unlock()
		p.deliveryLocked = false
	}
}

func (r *RegistryOf[T]) subscribersForReflectClasses(ctx context.Context, fed core.FederationName, inst *objectInstance, updateAttrs []core.AttributeHandle, classes map[core.FederateHandle]core.ObjectClassHandle) ([]core.FederateHandle, map[core.AttributeHandle][]core.FederateHandle) {
	perAttr := make(map[core.AttributeHandle][]core.FederateHandle, len(updateAttrs))
	var out []core.FederateHandle
	for sub, cls := range classes {
		matched := false
		for _, attr := range updateAttrs {
			if r.recipientInterested(ctx, fed, inst, sub, cls, attr, false) {
				perAttr[attr] = append(perAttr[attr], sub)
				matched = true
			}
		}
		if matched {
			out = append(out, sub)
		}
	}
	for _, subs := range perAttr {
		slices.Sort(subs)
	}
	slices.Sort(out)
	return out, perAttr
}

func (r *RegistryOf[T]) objectSubscribers(
	ctx context.Context,
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) []core.FederateHandle {
	if snapshots, ok := r.opts.Declarations.(interface {
		ObjectSubscribersSnapshot(
			context.Context,
			core.FederationName,
			core.ObjectClassHandle,
			[]core.AttributeHandle,
		) []core.FederateHandle
	}); ok {
		return snapshots.ObjectSubscribersSnapshot(ctx, fed, cls, attrs)
	}
	return r.opts.Declarations.SubscribersFor(ctx, fed, cls, attrs)
}

// sortedAdvisoryRecipients returns the map's keys in ascending order.
func sortedAdvisoryRecipients(m map[core.FederateHandle][]core.AttributeHandle) []core.FederateHandle {
	out := make([]core.FederateHandle, 0, len(m))
	for h := range m {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}

// copyAttrMap defensively copies the attribute map so per-subscriber
// outbound events do not alias the producer's input map. The bytes
// themselves are NOT cloned — proto.Marshal does not mutate them and
// each subscriber stream serializes independently.
func copyAttrMap(attrs map[core.AttributeHandle][]byte) map[uint64][]byte {
	out := make(map[uint64][]byte, len(attrs))
	for k, v := range attrs {
		out[uint64(k)] = v
	}
	return out
}

// attributeUpdatedRecordBlock packs the four eventlog-record
// allocations (eventRecord adapter + Event + oneof wrapper +
// AttributeUpdated body) into ONE (W3). The eventlog Writer still sees
// the exact same *eventRecord concrete type as before.
// newAttributeUpdatedEvent builds the eventlog adapter for an
// AttributeUpdated event in a single allocation. wireAttrs follows the
// same aliasing contract as prepareFanoutReflect: already safe to
// share, read-only from here. Fields are assigned individually (never
// whole-message copies).
func newAttributeUpdatedEventOf[T core.LogicalTimeScalar](obj core.ObjectHandle, producer core.FederateHandle, wireAttrs map[uint64][]byte, ts *T, wallNs int64) *core.ObjectJournalRecordOf[T] {
	return &core.ObjectJournalRecordOf[T]{
		Kind:        JournalAttributeUpdated,
		WallNS:      journalWallBits(wallNs),
		Object:      obj,
		Federate:    producer,
		Attributes:  wireAttrs,
		LogicalTime: copiedLogicalTimeOf[T](ts),
	}
}

func (p *preparedAttributeFanoutOf[T]) validateReflectionInstance(ts *T) (*DeletionStateOf[T], error) {
	st, inst := p.state, p.instance
	var pendingDeletion *DeletionStateOf[T]
	st.mu.Lock()
	if st.instances[inst.handle] != inst {
		st.mu.Unlock()
		return nil, core.ErrObjectNotFound
	}
	if d, pending := st.deletions[inst.handle]; pending {
		var err error
		if d.Committed || ts == nil {
			err = core.ErrObjectDeletionPending
		} else if *ts > d.Timestamp {
			// IEEE 1516.1-2010 6.10.3(i) permits equality before retirement.
			err = core.ErrTimeInvalidLogicalTime
		}
		if err != nil {
			st.mu.Unlock()
			return nil, err
		}
		pendingDeletion = &d
	}
	st.mu.Unlock()
	return pendingDeletion, nil
}

func (p *preparedAttributeFanoutOf[T]) prepareReflectionKnowledge(subs []core.FederateHandle, perAttr map[core.AttributeHandle][]core.FederateHandle, updateAttrs []core.AttributeHandle, classes map[core.FederateHandle]core.ObjectClassHandle) {
	st, inst, producer := p.state, p.instance, p.producer
	p.discoveryClasses = make(map[discoverKey]core.ObjectClassHandle)

	// The instance delivery lock keeps this scope snapshot stable through
	// admission and commit. Never hold the state lock while reserving an outbox.
	if perAttr != nil {
		st.mu.Lock()
		p.scope = prepareAttributeScopeLockedOf[T](st, producer, inst.handle, updateAttrs, perAttr)
		st.mu.Unlock()
	}
	st.mu.Lock()
	if producer != internalMOMProducer {
		for _, sub := range subs {
			key := discoverKey{sub: sub, obj: inst.handle}
			if _, known := st.discovered[key]; sub != producer && !known {
				p.discoveries = append(p.discoveries, key)
				p.discoveryClasses[key] = classes[sub]
			}
		}
	}
	st.mu.Unlock()
}

func (p *preparedAttributeFanoutOf[T]) sentTransportObserver() func(core.TransportType) {
	var observe func(core.TransportType)
	if p.registry.opts.OnObjectSentTransport != nil {
		observe = func(transport core.TransportType) {
			if transport == core.TransportTypeReliable {
				p.sentTransports[0] = true
			}
			if transport == core.TransportTypeBestEffort {
				p.sentTransports[1] = true
			}
		}
	}
	return observe
}

func (p *preparedAttributeFanoutOf[T]) prepareReflectionTime(ctx context.Context, ts *T, timestamped bool) (*T, error) {
	r, fed, producer, retractionHandle := p.registry, p.fed, p.producer, p.retraction
	// Validate the classified send, including unsubscribed TSO attributes.
	// An original timestamp on an all-RO update is not a TSO ordering key.
	var validationTime *T
	if timestamped {
		validationTime = ts
	}
	if err := r.validateOutgoingTSO(fed, producer, validationTime); err != nil {
		return nil, err
	}
	if retractionHandle != 0 && ts != nil {
		// Classify once for admission and delivery, including unsubscribed attrs.
		var err error
		p.retractionAdmission, err = r.reserveRetractionID(ctx, fed, producer, retractionHandle, validationTime)
		if err != nil {
			return nil, err
		}
	}
	return validationTime, nil
}

func (p *preparedAttributeFanoutOf[T]) reflectionAdvisories() (map[core.FederateHandle]bool, int) {
	r, fed := p.registry, p.fed
	nAdvisory := 0
	var scopeAdvisories map[core.FederateHandle]bool
	if p.scope != nil {
		scopeAdvisories = make(map[core.FederateHandle]bool)
		for h := range p.scope.newlyOut {
			scopeAdvisories[h] = r.attributeScopeAdvisoryEnabled(fed, h)
		}
		for h := range p.scope.newlyIn {
			if _, seen := scopeAdvisories[h]; !seen {
				scopeAdvisories[h] = r.attributeScopeAdvisoryEnabled(fed, h)
			}
		}
		for h := range p.scope.newlyOut {
			if scopeAdvisories[h] {
				nAdvisory++
			}
		}
		for h := range p.scope.newlyIn {
			if scopeAdvisories[h] {
				nAdvisory++
			}
		}
	}
	return scopeAdvisories, nAdvisory
}

func (p *preparedAttributeFanoutOf[T]) appendReflectionAdvisories(ctx context.Context, seq uint64, classes map[core.FederateHandle]core.ObjectClassHandle, scopeAdvisories map[core.FederateHandle]bool) uint64 {
	r, fed, inst := p.registry, p.fed, p.instance
	for _, key := range p.discoveries {
		p.appendImmediate(core.OutboxDelivery{
			Recipient: key.sub,
			Event:     &CallbackEventOf[T]{Sequence: seq, Kind: CallbackDiscover, Object: inst.handle, ObjectClass: classes[key.sub], ObjectName: inst.name, Metadata: metadataForOf[T](inst.owner, nil, 0)},
		})
		seq++
	}
	// After discovery, out-of-scope then in-scope, with sorted recipients in each
	// class. Every advisory remains ahead of every reflect.
	if p.scope != nil {
		for _, h := range sortedAdvisoryRecipients(p.scope.newlyOut) {
			if !scopeAdvisories[h] {
				continue
			}
			p.appendImmediate(core.OutboxDelivery{
				Recipient: h,
				Event:     &CallbackEventOf[T]{Sequence: seq, Kind: CallbackAttributesOutOfScope, Object: inst.handle, AttributeHandles: r.projectAttributes(ctx, fed, inst.cls, classes[h], p.scope.newlyOut[h])},
			})
			seq++
		}
		for _, h := range sortedAdvisoryRecipients(p.scope.newlyIn) {
			if !scopeAdvisories[h] {
				continue
			}
			p.appendImmediate(core.OutboxDelivery{
				Recipient: h,
				Event:     &CallbackEventOf[T]{Sequence: seq, Kind: CallbackAttributesInScope, Object: inst.handle, AttributeHandles: r.projectAttributes(ctx, fed, inst.cls, classes[h], p.scope.newlyIn[h])},
			})
			seq++
		}
	}

	return seq
}

func (p *preparedAttributeFanoutOf[T]) reflectionEvent(ctx context.Context, seq uint64, part attributeDeliveryOf[T], known core.ObjectClassHandle, delayed bool, ts *T, metadata ...messageMetadata) *CallbackEventOf[T] {
	r, fed, inst, producer, retractionHandle := p.registry, p.fed, p.instance, p.producer, p.retraction
	projected := part.attributes
	if cls := known; !delayed && cls != inst.cls {
		projected = make(map[uint64][]byte, len(part.attributes))
		for attr, value := range part.attributes {
			if mapped, ok := r.attributeAtClass(ctx, fed, inst.cls, cls, core.AttributeHandle(attr)); ok {
				projected[uint64(mapped)] = value
			}
		}
	}
	evt := newReflectEventOf[T](seq, inst.handle, known, projected, copiedLogicalTimeOf[T](ts))
	evt.Metadata = metadataForOf[T](producer, part.time, part.transport)
	evt.Metadata.DelayedSubscription = delayed
	if delayed {
		evt.Metadata.RegisteredClass = inst.cls
	}
	if part.time != nil {
		evt.Metadata.RetractionHandle = retractionHandle
	}
	evt.Metadata.Regions = part.regions
	evt.Metadata.ConveyedRegions = slices.Clone(part.conveyed)
	if part.attributeConveyed != nil {
		evt.Metadata.AttributeConveyedRegions = make(map[core.AttributeHandle][]core.ConveyedRegionCopy, len(part.attributeConveyed))
		for actual, copies := range part.attributeConveyed {
			if delayed {
				evt.Metadata.AttributeConveyedRegions[actual] = slices.Clone(copies)
				continue
			}
			if mapped, found := r.attributeAtClass(ctx, fed, inst.cls, known, actual); found {
				evt.Metadata.AttributeConveyedRegions[mapped] = slices.Clone(copies)
			}
		}
	}
	if len(metadata) > 0 {
		evt.UserSuppliedTag = metadata[0].tag
	}
	return evt
}

func (p *preparedAttributeFanoutOf[T]) reflectionMessages(ctx context.Context, seq uint64, parts []attributeDeliveryOf[T], classes map[core.FederateHandle]core.ObjectClassHandle, delayed bool, ts *T, canReserveTSO, useTSOGate bool, pendingDeletion *DeletionStateOf[T], metadata ...messageMetadata) []core.MessageDeliveryOf[T] {
	r, fed, inst, producer, retractionHandle := p.registry, p.fed, p.instance, p.producer, p.retraction
	var reflects []core.MessageDeliveryOf[T]
	if useTSOGate {
		reflects = make([]core.MessageDeliveryOf[T], 0, len(parts))
	}
	for _, part := range parts {
		sub := part.subscriber
		evt := p.reflectionEvent(ctx, seq, part, classes[part.subscriber], delayed, ts, metadata...)
		seq++
		p.reflectSubs = append(p.reflectSubs, sub)
		if canReserveTSO || part.time != nil && useTSOGate {
			reflects = append(reflects, messageDeliveryOf[T](sub, producer, part.time, evt, retractionHandle))
		} else {
			if projected := r.projectImmediateDelayed(ctx, fed, sub, evt); projected != nil {
				p.appendImmediate(core.OutboxDelivery{Recipient: sub, Event: projected})
			}
		}
	}

	if pendingDeletion != nil {
		for _, key := range p.discoveries {
			d := pendingDeletion
			event := &CallbackEventOf[T]{Sequence: seq, Kind: CallbackRemove, Object: inst.handle, ObjectClass: classes[key.sub], LogicalTime: copiedLogicalTimeOf[T](&d.Timestamp), UserSuppliedTag: slices.Clone(d.Tag), Metadata: metadataForOf[T](d.Producer, &d.Timestamp, 0)}
			event.Metadata.RetractionHandle = d.RetractionHandle
			seq++
			reflects = append(reflects, core.MessageDeliveryOf[T]{Recipient: key.sub, Timestamp: d.Timestamp, Event: event, Sender: d.Producer, RetractionHandle: d.RetractionHandle})
		}
	}
	return reflects
}

func (p *preparedAttributeFanoutOf[T]) prepareReflectionTSO(ctx context.Context, validationTime *T, reflects []core.MessageDeliveryOf[T], canReserveTSO bool) error {
	r, fed, producer, st, inst := p.registry, p.fed, p.producer, p.state, p.instance
	if canReserveTSO {
		var err error
		p.tsoReservation, err = r.reserveOutgoingTSO(fed, producer, validationTime, reflects)
		if err != nil {
			return err
		}
		if p.tsoReservation == nil {
			return fmt.Errorf("reservable TSO gate returned a nil reservation")
		}
		for _, delivery := range p.tsoReservation.Immediate() {
			projected := r.projectImmediateDelayed(ctx, fed, delivery.Recipient, delivery.Event)
			if projected == nil {
				continue
			}
			p.appendImmediate(core.OutboxDelivery{
				Recipient: delivery.Recipient,
				Event:     projected,
			})
		}
		st.mu.Lock()
		stillLive := st.instances[inst.handle] == inst
		st.mu.Unlock()
		if !stillLive {
			return core.ErrObjectNotFound
		}
	} else {
		for _, delivery := range reflects {
			if r.opts.TSOGate.ShouldDeliverNow(fed, delivery.Recipient, delivery.Timestamp) {
				projected := r.projectImmediateDelayed(ctx, fed, delivery.Recipient, delivery.Event)
				if projected == nil {
					continue
				}
				p.appendImmediate(core.OutboxDelivery{
					Recipient: delivery.Recipient,
					Event:     projected,
				})
				continue
			}
			p.buffered = append(p.buffered, bufferedAttributeDeliveryOf[T]{
				sub: delivery.Recipient, ts: delivery.Timestamp, event: delivery.Event,
			})
		}
	}
	return nil
}

func (p *preparedAttributeFanoutOf[T]) reserveReflectionOutbox(ctx context.Context) error {
	r, fed := p.registry, p.fed
	// A legacy gate cannot reserve multiple buffered recipients, nor can it
	// atomically combine a buffered delivery with an immediate callback.
	if len(p.buffered) > 0 && (len(p.buffered) > 1 || len(p.immediate) > 0) {
		return fmt.Errorf(
			"atomic update fanout requires a reservable TSO gate for %d immediate and %d buffered deliveries",
			len(p.immediate), len(p.buffered),
		)
	}

	if len(p.immediate) > 0 {
		if reservable, ok := r.opts.Outbox.(core.ReservableOutbox); ok {
			// W4 fast path: the outbox reservation exists solely to span
			// the write-ahead append atomically. With no EventLog, a
			// single immediate delivery, and no TSO-buffered work there
			// is nothing to make atomic — commit calls Outbox.Send
			// directly at the exact program point Reserve+Commit would
			// have delivered, skipping the reservation allocation and
			// its recipient-lock round trip.
			if r.opts.EventLog == nil && len(p.immediate) == 1 &&
				len(p.buffered) == 0 && p.tsoReservation == nil {
				p.directSend = true
				return nil
			}
			reservation, err := reservable.Reserve(ctx, fed, p.immediate)
			if err != nil {
				return err
			}
			if reservation == nil {
				return fmt.Errorf("reservable outbox returned a nil reservation")
			}
			p.reservation = reservation
		} else if len(p.immediate) > 1 {
			return fmt.Errorf(
				"atomic update fanout requires a reservable outbox for %d immediate deliveries",
				len(p.immediate),
			)
		}
	}
	return nil
}

func (r *RegistryOf[T]) prepareUpdateObject(ctx context.Context, fed core.FederationName, st *federationStateOf[T], producer core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (*objectInstance, core.ObjectClassHandle, []core.AttributeHandle, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, ok := st.instances[obj]
	if !ok {
		return nil, 0, nil, core.ErrObjectNotFound
	}
	known := st.knownClass(producer, inst)
	var mapped []core.AttributeHandle
	var err error
	if known != inst.cls {
		mapped, err = r.knownAttributes(ctx, fed, st, producer, inst, attrs)
	}
	return inst, known, mapped, err
}

func (p *preparedAttributeFanoutOf[T]) commitImmediateReflections(ctx context.Context) error {
	if p.reservation != nil {
		if err := p.reservation.Commit(); err != nil {
			return err
		}
		p.reservation = nil
		for _, delivery := range p.immediate {
			p.recordReflectionDelivery(ctx, delivery)
		}
	} else {
		for _, delivery := range p.immediate {
			if err := p.registry.opts.Outbox.Send(ctx, p.fed, delivery.Recipient, delivery.Event); err != nil {
				return err
			}
			p.recordReflectionDelivery(ctx, delivery)
		}
	}
	return nil
}

func (p *preparedAttributeFanoutOf[T]) commitBufferedReflections(ctx context.Context) error {
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
	return nil
}

func (r *RegistryOf[T]) validateUpdateOwnership(fed core.FederationName, producer core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
	if r.opts.Ownership != nil && producer != internalMOMProducer {
		for _, attr := range attrs {
			if !r.opts.Ownership.IsOwnedBy(fed, producer, obj, attr) {
				return core.ErrAttributeNotOwned
			}
		}
	}
	return nil
}

func (p *preparedAttributeFanoutOf[T]) recordReflectionTraffic() {
	if p.registry.opts.OnReflectDelivered != nil {
		for _, sub := range p.reflectSubs {
			p.registry.opts.OnReflectDelivered(p.fed, sub)
		}
	}
	if p.registry.opts.OnObjectTraffic != nil {
		for _, sub := range p.reflectSubs {
			p.registry.opts.OnObjectTraffic(p.fed, sub, p.class, true)
		}
	}
	if hook := p.registry.opts.OnObjectInstanceTraffic; hook != nil {
		for _, sub := range p.reflectSubs {
			hook(p.fed, sub, p.instance.handle, p.class, true)
		}
	}
}
