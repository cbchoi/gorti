package object

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
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

// Order is the per-attribute or per-interaction declared delivery order
// in the FOM. TASK-077: when the federation operates in best-effort
// mode and an attribute / interaction is declared OrderReceive, the
// outbound event is delivered RO (timestamp stripped).
type Order uint8

const (
	// OrderTimeStamp is the FOM-default ordering. Outbound events keep
	// their timestamp (TSO).
	OrderTimeStamp Order = iota
	// OrderReceive marks the attribute / interaction as best-effort.
	// In a best-effort federation the outbound event drops its
	// timestamp and is delivered immediately (RO).
	OrderReceive
)

// FederationModeLookup answers "what mode is this federation in?".
// The federation manager satisfies this contract directly via its
// ModeFor method; tests can supply a stub.
//
// Optional in object.Options: when nil, the registry treats every
// federation as ModeVerbose (existing TSO-only behavior preserved).
type FederationModeLookup interface {
	ModeFor(fed core.FederationName) (core.Mode, bool)
}

// AttributeOrderLookup answers "what is this attribute / interaction
// declared with in the FOM?". Returning (OrderTimeStamp, false)
// signals "unknown / not declared best-effort"; the registry treats
// unknown as TimeStamp-order to preserve TSO unless an explicit
// best-effort declaration is present.
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
type OutgoingTSOValidator interface {
	ValidateOutgoingTSO(fed core.FederationName, sender core.FederateHandle, ts core.LogicalTime) error
}

// validateOutgoingTSO consults the optional TSOValidator for a TSO
// send (non-nil ts). RO sends bypass. Shared by the update /
// interaction / delete ingestion points.
func (r *Registry) validateOutgoingTSO(fed core.FederationName, sender core.FederateHandle, ts *core.LogicalTime) error {
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
//  3. Resolve per-attribute delivery order: when the federation runs in
//     best-effort mode AND every updated attribute is declared
//     OrderReceive in the FOM, the outbound event drops its timestamp
//     (RO). Otherwise the timestamp is preserved (TSO). See TASK-077.
//  4. Resolve and reserve the complete callback set, including DDM scope
//     advisories, immediate recipients, and TSO-buffer admission.
//  5. Persist AttributeUpdated to the EventLog (write-ahead).
//  6. Commit callbacks in advisory-before-reflect, sorted-recipient order.
//
// Cut-1 simplification: attribute bytes are passed through as-is. The
// CodecFactory dependency is reserved for the gRPC layer's transition
// path; the registry sees pre-encoded bytes from the client (which the
// PySDK / Go SDK encode via rti/pkg/encoding before sending).
func (r *Registry) updateAttributes(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[core.AttributeHandle][]byte,
	ts *core.LogicalTime,
	retractionHandle uint64,
) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	inst, ok := st.instances[obj]
	st.mu.Unlock()
	if !ok {
		return core.ErrObjectNotFound
	}

	if !r.producerOwnsAllAttrs(ctx, fed, producer, inst.cls, attrs) {
		return core.ErrAttributeNotOwned
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
	if r.opts.Ownership != nil && producer != internalMOMProducer {
		for attr := range attrs {
			if !r.opts.Ownership.IsOwnedBy(fed, producer, obj, attr) {
				return core.ErrAttributeNotOwned
			}
		}
	}

	// M37 EB-3 — §8.1.2 outgoing-TSO timestamp validation, BEFORE the
	// eventlog write-ahead so rejected sends never enter the replay log.
	if err := r.validateOutgoingTSO(fed, producer, ts); err != nil {
		return err
	}

	updateAttrs := sortedAttrHandles(attrs)
	deliveryTs := r.deliveryTimestampForAttributes(fed, inst.cls, updateAttrs, ts)
	// One wire-shaped copy serves BOTH the outbound reflect envelope and
	// the eventlog record (W3): the map is read-only after this point
	// (proto.Marshal does not mutate), so sharing it is safe, and the
	// caller keeps the right to mutate its input map post-call.
	wireAttrs := copyAttrMap(attrs)
	fanout, err := r.prepareFanoutReflect(
		ctx, fed, st, producer, inst, wireAttrs, updateAttrs, deliveryTs, retractionHandle,
	)
	if err != nil {
		return fmt.Errorf("object: update %d in %q: reserve fanout: %w", obj, fed, err)
	}
	defer fanout.abort()

	if r.opts.EventLog != nil {
		ev := newAttributeUpdatedEvent(obj, producer, wireAttrs, ts, r.opts.Clock.Now().UnixNano())
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
func (r *Registry) UpdateAttributesWire(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[uint64][]byte,
	ts *core.LogicalTime,
) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	inst, ok := st.instances[obj]
	st.mu.Unlock()
	if !ok {
		return core.ErrObjectNotFound
	}

	if !r.producerOwnsAllAttrsWire(ctx, fed, producer, inst.cls, attrs) {
		return core.ErrAttributeNotOwned
	}

	// M38 GB §6.6 per-instance ownership gate — uint64-keyed twin of the
	// updateAttributes loop (same MOM-producer exemption).
	if r.opts.Ownership != nil && producer != internalMOMProducer {
		for attr := range attrs {
			if !r.opts.Ownership.IsOwnedBy(fed, producer, obj, core.AttributeHandle(attr)) {
				return core.ErrAttributeNotOwned
			}
		}
	}

	// M37 EB-3 — §8.1.2 outgoing-TSO validation before the write-ahead.
	if err := r.validateOutgoingTSO(fed, producer, ts); err != nil {
		return err
	}

	updateAttrs := sortedAttrHandlesWire(attrs)
	deliveryTs := r.deliveryTimestampForAttributes(fed, inst.cls, updateAttrs, ts)
	fanout, err := r.prepareFanoutReflect(
		ctx, fed, st, producer, inst, attrs, updateAttrs, deliveryTs, 0,
	)
	if err != nil {
		return fmt.Errorf("object: update %d in %q: reserve fanout: %w", obj, fed, err)
	}
	defer fanout.abort()

	if r.opts.EventLog != nil {
		ev := newAttributeUpdatedEvent(obj, producer, attrs, ts, r.opts.Clock.Now().UnixNano())
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
	return nil
}

// deliveryTimestampForAttributes resolves the timestamp the registry
// should stamp on outbound ReflectAttributeValues envelopes. When the
// federation is best-effort AND every updated attribute is declared
// OrderReceive in the FOM, the timestamp is dropped (returns nil → RO
// delivery). Otherwise the producer-supplied timestamp passes through
// unchanged (TSO delivery, including the existing nil-when-RO-from-the-
// producer behavior).
//
// Either lookup being absent (Federations or Orders nil) collapses to
// the TSO-default path so existing tests that do not wire either
// continue to behave as they did before TASK-077.
func (r *Registry) deliveryTimestampForAttributes(
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
	ts *core.LogicalTime,
) *core.LogicalTime {
	if ts == nil {
		// Producer already chose RO; nothing to strip.
		return nil
	}
	if r.opts.Federations == nil || r.opts.Orders == nil {
		return ts
	}
	mode, ok := r.opts.Federations.ModeFor(fed)
	if !ok || mode != core.ModeBestEffort {
		return ts
	}
	// Best-effort mode: check that EVERY updated attribute is declared
	// OrderReceive. If even one attribute is OrderTimeStamp (or unknown,
	// which we conservatively treat as TimeStamp), keep the timestamp.
	// Per-attribute split into separate envelopes is a future
	// enhancement; the cut-1 contract is one-envelope-per-update.
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
func (r *Registry) producerOwnsAllAttrs(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte) bool {
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
func (r *Registry) producerOwnsAllAttrsWire(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.ObjectClassHandle, attrs map[uint64][]byte) bool {
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

type bufferedAttributeDelivery struct {
	sub   core.FederateHandle
	ts    core.LogicalTime
	event core.OutboundEvent
}

type preparedAttributeScope struct {
	object   core.ObjectHandle
	attrs    []core.AttributeHandle
	desired  map[core.AttributeHandle]map[core.FederateHandle]struct{}
	newlyIn  map[core.FederateHandle][]core.AttributeHandle
	newlyOut map[core.FederateHandle][]core.AttributeHandle
}

type preparedAttributeFanout struct {
	registry          *Registry
	state             *federationState
	fed               core.FederationName
	producer          core.FederateHandle
	retraction        uint64
	immediate         []core.OutboxDelivery
	immediateInline   [1]core.OutboxDelivery
	buffered          []bufferedAttributeDelivery
	reflectSubs       []core.FederateHandle
	reflectSubsInline [1]core.FederateHandle
	reservation       core.OutboxReservation
	tsoReservation    core.TSOBufferReservation
	scope             *preparedAttributeScope
	stateLocked       bool
	finished          bool
	// directSend marks the W4 reservation-bypass fast path: EventLog nil,
	// exactly one immediate delivery, no TSO-buffered work. Commit sends
	// via Outbox.Send instead of committing a reservation, and the caller
	// wraps a commit failure with the SAME "reserve fanout" prefix the
	// Reserve path uses so the error surface is identical across branches.
	directSend bool
}

func (p *preparedAttributeFanout) initImmediate(capacity int) {
	if capacity <= 0 || cap(p.immediate) >= capacity {
		return
	}
	if capacity == 1 {
		p.immediate = p.immediateInline[:0]
		return
	}
	p.immediate = make([]core.OutboxDelivery, 0, capacity)
}

func (p *preparedAttributeFanout) appendImmediate(delivery core.OutboxDelivery) {
	if cap(p.immediate) == 0 {
		p.immediate = p.immediateInline[:0]
	}
	p.immediate = append(p.immediate, delivery)
}

func (p *preparedAttributeFanout) initReflectSubs(capacity int) {
	if capacity <= 0 {
		return
	}
	if capacity == 1 {
		p.reflectSubs = p.reflectSubsInline[:0]
		return
	}
	p.reflectSubs = make([]core.FederateHandle, 0, capacity)
}

func prepareAttributeScopeLocked(
	st *federationState,
	producer core.FederateHandle,
	object core.ObjectHandle,
	updateAttrs []core.AttributeHandle,
	perAttr map[core.AttributeHandle][]core.FederateHandle,
) *preparedAttributeScope {
	prepared := &preparedAttributeScope{
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

func (p *preparedAttributeScope) applyLocked(st *federationState) {
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
type reflectEventBlock struct {
	evt  outboundEvent
	pb   rtiv1.FederateEvent
	body rtiv1.FederateEvent_Reflect
}

// newReflectEvent builds one per-subscriber reflect FederateEvent in a
// single allocation. Fields are assigned individually (never
// whole-message copies) so the generated messages' internal state is
// not copied.
func newReflectEvent(seq uint64, reflect *rtiv1.ReflectAttributeValues) *outboundEvent {
	blk := &reflectEventBlock{}
	blk.body.Reflect = reflect
	blk.pb.Seq = seq
	blk.pb.Event = &blk.body
	blk.evt.pb = &blk.pb
	return &blk.evt
}

func logicalTimeValue(ts *core.LogicalTime) core.LogicalTime {
	if ts == nil {
		return 0
	}
	return *ts
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
func (r *Registry) prepareFanoutReflect(
	ctx context.Context,
	fed core.FederationName,
	st *federationState,
	producer core.FederateHandle,
	inst *objectInstance,
	wireAttrs map[uint64][]byte,
	updateAttrs []core.AttributeHandle,
	ts *core.LogicalTime,
	retractionHandle uint64,
) (*preparedAttributeFanout, error) {
	prepared := &preparedAttributeFanout{
		registry:   r,
		state:      st,
		fed:        fed,
		producer:   producer,
		retraction: retractionHandle,
	}
	subs, perAttr := r.subscribersForReflect(ctx, fed, inst, updateAttrs)

	// Keep the DDM scope snapshot stable through admission, WAL, and commit.
	// This prevents concurrent updates from observing the same old cache and
	// emitting duplicate or contradictory advisories.
	if perAttr != nil {
		st.mu.Lock()
		prepared.stateLocked = true
		prepared.scope = prepareAttributeScopeLocked(st, producer, inst.handle, updateAttrs, perAttr)
	}

	nReflect := 0
	for _, sub := range subs {
		if sub != producer {
			nReflect++
		}
	}
	nAdvisory := 0
	if prepared.scope != nil {
		nAdvisory = len(prepared.scope.newlyOut) + len(prepared.scope.newlyIn)
	}
	nSeq := nAdvisory + nReflect
	useTSOGate := ts != nil && r.opts.TSOGate != nil
	if !useTSOGate {
		prepared.initImmediate(nSeq)
	} else {
		prepared.initImmediate(nAdvisory)
	}
	prepared.initReflectSubs(nReflect)
	var seq uint64
	if nSeq > 0 {
		if prepared.stateLocked {
			seq = st.nextOutboundSeqRangeLocked(nSeq)
		} else {
			st.mu.Lock()
			seq = st.nextOutboundSeqRangeLocked(nSeq)
			st.mu.Unlock()
		}
	}

	// Out-of-scope first, then in-scope, with sorted recipients in each
	// class. Every advisory remains ahead of every reflect.
	if prepared.scope != nil {
		for _, h := range sortedAdvisoryRecipients(prepared.scope.newlyOut) {
			prepared.appendImmediate(core.OutboxDelivery{
				Recipient: h,
				Event: &outboundEvent{pb: &rtiv1.FederateEvent{
					Seq: seq,
					Event: &rtiv1.FederateEvent_AttributesOutOfScope{
						AttributesOutOfScope: &rtiv1.AttributesOutOfScope{
							ObjectHandle:     uint64(inst.handle),
							AttributeHandles: attrHandlesToWire(prepared.scope.newlyOut[h]),
						},
					},
				}},
			})
			seq++
		}
		for _, h := range sortedAdvisoryRecipients(prepared.scope.newlyIn) {
			prepared.appendImmediate(core.OutboxDelivery{
				Recipient: h,
				Event: &outboundEvent{pb: &rtiv1.FederateEvent{
					Seq: seq,
					Event: &rtiv1.FederateEvent_AttributesInScope{
						AttributesInScope: &rtiv1.AttributesInScope{
							ObjectHandle:     uint64(inst.handle),
							AttributeHandles: attrHandlesToWire(prepared.scope.newlyIn[h]),
						},
					},
				}},
			})
			seq++
		}
	}

	pb := &rtiv1.ReflectAttributeValues{
		ObjectHandle:      uint64(inst.handle),
		ObjectClassHandle: uint64(inst.cls),
		Attributes:        wireAttrs,
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	var reflects []core.TSOBufferedDelivery
	if useTSOGate {
		reflects = make([]core.TSOBufferedDelivery, 0, nReflect)
	}
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		evt := newReflectEvent(seq, pb)
		seq++
		prepared.reflectSubs = append(prepared.reflectSubs, sub)
		if useTSOGate {
			reflects = append(reflects, core.TSOBufferedDelivery{
				Recipient: sub, Timestamp: logicalTimeValue(ts), Event: evt,
				Sender: producer, RetractionHandle: retractionHandle,
			})
		} else {
			prepared.appendImmediate(core.OutboxDelivery{Recipient: sub, Event: evt})
		}
	}

	if useTSOGate && len(reflects) > 0 {
		if reservable, ok := r.opts.TSOGate.(core.ReservableTSODeliveryGate); ok {
			prepared.tsoReservation = reservable.ReserveTSO(fed, reflects)
			if prepared.tsoReservation == nil {
				prepared.abort()
				return nil, fmt.Errorf("reservable TSO gate returned a nil reservation")
			}
			for _, delivery := range prepared.tsoReservation.Immediate() {
				prepared.appendImmediate(core.OutboxDelivery{
					Recipient: delivery.Recipient,
					Event:     delivery.Event,
				})
			}
		} else {
			for _, delivery := range reflects {
				if r.opts.TSOGate.ShouldDeliverNow(fed, delivery.Recipient, delivery.Timestamp) {
					prepared.appendImmediate(core.OutboxDelivery{
						Recipient: delivery.Recipient,
						Event:     delivery.Event,
					})
					continue
				}
				prepared.buffered = append(prepared.buffered, bufferedAttributeDelivery{
					sub: delivery.Recipient, ts: delivery.Timestamp, event: delivery.Event,
				})
			}
		}
	}

	// A legacy gate cannot reserve multiple buffered recipients, nor can it
	// atomically combine a buffered delivery with an immediate callback.
	if len(prepared.buffered) > 0 && (len(prepared.buffered) > 1 || len(prepared.immediate) > 0) {
		prepared.abort()
		return nil, fmt.Errorf(
			"atomic update fanout requires a reservable TSO gate for %d immediate and %d buffered deliveries",
			len(prepared.immediate), len(prepared.buffered),
		)
	}

	if len(prepared.immediate) > 0 {
		if reservable, ok := r.opts.Outbox.(core.ReservableOutbox); ok {
			// W4 fast path: the outbox reservation exists solely to span
			// the write-ahead append atomically. With no EventLog, a
			// single immediate delivery, and no TSO-buffered work there
			// is nothing to make atomic — commit calls Outbox.Send
			// directly at the exact program point Reserve+Commit would
			// have delivered, skipping the reservation allocation and
			// its recipient-lock round trip.
			if r.opts.EventLog == nil && len(prepared.immediate) == 1 &&
				len(prepared.buffered) == 0 && prepared.tsoReservation == nil {
				prepared.directSend = true
				return prepared, nil
			}
			reservation, err := reservable.Reserve(ctx, fed, prepared.immediate)
			if err != nil {
				prepared.abort()
				return nil, err
			}
			if reservation == nil {
				prepared.abort()
				return nil, fmt.Errorf("reservable outbox returned a nil reservation")
			}
			prepared.reservation = reservation
		} else if len(prepared.immediate) > 1 {
			prepared.abort()
			return nil, fmt.Errorf(
				"atomic update fanout requires a reservable outbox for %d immediate deliveries",
				len(prepared.immediate),
			)
		}
	}
	return prepared, nil
}

func (p *preparedAttributeFanout) commit(ctx context.Context) error {
	if p.finished {
		return nil
	}
	// Match the interaction fanout lock order: release outbox recipient locks
	// before the TSO gate may synchronously re-evaluate a pending grant.
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
	if p.scope != nil {
		p.scope.applyLocked(p.state)
	}
	p.unlockState()
	p.finished = true
	if p.registry.opts.OnReflectDelivered != nil {
		for _, sub := range p.reflectSubs {
			p.registry.opts.OnReflectDelivered(p.fed, sub)
		}
	}
	return nil
}

func (p *preparedAttributeFanout) abort() {
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
	p.unlockState()
	p.finished = true
}

func (p *preparedAttributeFanout) unlockState() {
	if p.stateLocked {
		p.state.mu.Unlock()
		p.stateLocked = false
	}
}

// subscribersForReflect resolves the set of subscriber federates for
// a given (object, attribute set) update. Splits the cut-1 path from
// the M10 DDM-aware path so the hot path stays one direct call when
// no DDM filter is wired or no associations exist.
//
// The second return value is the per-attribute recipient breakdown,
// populated ONLY on the DDM-aware path (nil otherwise). The §6.17/§6.18
// scope-advisory emitter consumes it (M37).
func (r *Registry) subscribersForReflect(ctx context.Context, fed core.FederationName, inst *objectInstance, updateAttrs []core.AttributeHandle) ([]core.FederateHandle, map[core.AttributeHandle][]core.FederateHandle) {
	if r.opts.DDM == nil || !r.opts.DDM.HasObjectAssociations(fed, inst.handle) {
		return r.objectSubscribers(ctx, fed, inst.cls, updateAttrs), nil
	}
	// DDM-aware union across the updated attribute set. For each
	// attribute, fall back to the cut-1 subscribers when the
	// publisher did not associate any region for that attr (the
	// per-attr nil branch).
	union := map[core.FederateHandle]struct{}{}
	perAttr := make(map[core.AttributeHandle][]core.FederateHandle, len(updateAttrs))
	for _, attr := range updateAttrs {
		pubRegions := r.opts.DDM.PublisherRegionsFor(fed, inst.handle, attr)
		var subs []core.FederateHandle
		if len(pubRegions) == 0 {
			subs = r.objectSubscribers(ctx, fed, inst.cls, []core.AttributeHandle{attr})
		} else {
			subs = r.opts.DDM.SubscribersForUpdate(fed, inst.cls, attr, pubRegions)
		}
		perAttr[attr] = subs
		for _, h := range subs {
			union[h] = struct{}{}
		}
	}
	out := make([]core.FederateHandle, 0, len(union))
	for h := range union {
		out = append(out, h)
	}
	slices.Sort(out)
	return out, perAttr
}

func (r *Registry) objectSubscribers(
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

// attrHandlesToWire converts (already-sorted-by-iteration) attribute
// handles to their wire form, sorting defensively.
func attrHandlesToWire(attrs []core.AttributeHandle) []uint64 {
	slices.Sort(attrs)
	out := make([]uint64, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, uint64(a))
	}
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
type attributeUpdatedRecordBlock struct {
	rec  eventRecord
	ev   rtiv1.Event
	body rtiv1.Event_AttrUpdated
	pb   rtiv1.AttributeUpdated
}

// newAttributeUpdatedEvent builds the eventlog adapter for an
// AttributeUpdated event in a single allocation. wireAttrs follows the
// same aliasing contract as prepareFanoutReflect: already safe to
// share, read-only from here. Fields are assigned individually (never
// whole-message copies).
func newAttributeUpdatedEvent(obj core.ObjectHandle, producer core.FederateHandle, wireAttrs map[uint64][]byte, ts *core.LogicalTime, wallNs int64) *eventRecord {
	blk := &attributeUpdatedRecordBlock{}
	blk.pb.ObjectHandle = uint64(obj)
	blk.pb.ProducerFederateHandle = uint64(producer)
	blk.pb.Attributes = wireAttrs
	if ts != nil {
		v := float64(*ts)
		blk.pb.LogicalTime = &v
	}
	blk.body.AttrUpdated = &blk.pb
	blk.ev.WallNs = uint64(wallNs)
	blk.ev.Body = &blk.body
	blk.rec.pb = &blk.ev
	return &blk.rec
}
