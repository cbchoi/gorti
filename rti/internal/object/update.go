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
//  3. Persist AttributeUpdated to the EventLog (write-ahead).
//  4. Resolve per-attribute delivery order: when the federation runs in
//     best-effort mode AND every updated attribute is declared
//     OrderReceive in the FOM, the outbound event drops its timestamp
//     (RO). Otherwise the timestamp is preserved (TSO). See TASK-077.
//  5. Fan ReflectAttributeValues to subscribers of any updated attribute,
//     in sorted FederateHandle order, EXCEPT the producer.
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

	if r.opts.EventLog != nil {
		ev := newAttributeUpdatedEvent(obj, producer, attrs, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			return fmt.Errorf("object: update %d in %q: eventlog append: %w", obj, fed, err)
		}
	}

	deliveryTs := r.deliveryTimestampForAttributes(fed, inst.cls, updateAttrs, ts)
	r.fanoutReflect(ctx, fed, st, producer, inst, attrs, updateAttrs, deliveryTs, retractionHandle)
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

// fanoutReflect sends ReflectAttributeValues to every federate subscribed
// to any attribute in updateAttrs, excluding the producer. Per-subscriber
// outbound seq is stamped here.
//
// DDM hook (M10 / FR-DDM-3..6): when the registry was constructed with
// a DDM filter AND the producer has associated regions with `inst`,
// the cut-1 declaration.SubscribersFor result is replaced by the
// union (over updateAttrs) of DDM.SubscribersForUpdate. When no
// associations exist for inst the registry takes the cut-1 path
// unchanged — the FR-DDM-6 zero-cost contract.
func (r *Registry) fanoutReflect(ctx context.Context, fed core.FederationName, st *federationState, producer core.FederateHandle, inst *objectInstance, attrs map[core.AttributeHandle][]byte, updateAttrs []core.AttributeHandle, ts *core.LogicalTime, retractionHandle uint64) {
	subs, perAttr := r.subscribersForReflect(ctx, fed, inst, updateAttrs)
	// §6.17/§6.18 (M37 Agent EA) — scope advisories fire BEFORE the
	// reflect fanout so a newly-in-scope subscriber sees InScope →
	// Reflect in stream order. perAttr is non-nil only on the DDM-
	// aware path (FR-DDM-6 zero-cost contract preserved).
	if perAttr != nil {
		r.emitScopeAdvisories(ctx, fed, st, producer, inst, updateAttrs, perAttr)
	}
	// Hoist: see fanoutReceive for the rationale (single defensive
	// copy + shared inner proto + batched seq allocation).
	pb := &rtiv1.ReflectAttributeValues{
		ObjectHandle:      uint64(inst.handle),
		ObjectClassHandle: uint64(inst.cls),
		Attributes:        copyAttrMap(attrs),
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	nSeq := 0
	for _, sub := range subs {
		if sub != producer {
			nSeq++
		}
	}
	if nSeq == 0 {
		return
	}
	st.mu.Lock()
	startSeq := st.nextOutboundSeqRangeLocked(nSeq)
	st.mu.Unlock()
	seq := startSeq
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		evt := &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq:   seq,
			Event: &rtiv1.FederateEvent_Reflect{Reflect: pb},
		}}
		seq++
		// M22 TASK-237 — gate TSO delivery. If the federation has a
		// TSOGate configured AND this delivery carries a timestamp
		// (TSO), consult the gate. Async-on or currentTime-caught-up
		// → direct Send; otherwise BufferTSO holds the event until
		// advance grant releases it. RO delivery (ts == nil) bypasses.
		if ts != nil && r.opts.TSOGate != nil {
			if r.opts.TSOGate.ShouldDeliverNow(fed, sub, *ts) {
				_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
			} else if retractionHandle != 0 {
				_ = r.opts.TSOGate.BufferTSOWithRetraction(
					ctx, fed, sub, *ts, evt, producer, retractionHandle,
				)
			} else {
				_ = r.opts.TSOGate.BufferTSO(ctx, fed, sub, *ts, evt)
			}
		} else {
			_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
		}
		if r.opts.OnReflectDelivered != nil {
			r.opts.OnReflectDelivered(fed, sub)
		}
	}
}

// subscribersForReflect resolves the set of subscriber federates for
// a given (object, attribute set) update. Splits the cut-1 path from
// the M10 DDM-aware path so the hot path stays one direct call when
// no DDM filter is wired or no associations exist.
//
// The second return value is the per-attribute recipient breakdown,
// populated ONLY on the DDM-aware path (nil otherwise). The §6.17/§6.18
// scope-advisory emitter consumes it (M37 Agent EA).
func (r *Registry) subscribersForReflect(ctx context.Context, fed core.FederationName, inst *objectInstance, updateAttrs []core.AttributeHandle) ([]core.FederateHandle, map[core.AttributeHandle][]core.FederateHandle) {
	if r.opts.DDM == nil || !r.opts.DDM.HasObjectAssociations(fed, inst.handle) {
		return r.opts.Declarations.SubscribersFor(ctx, fed, inst.cls, updateAttrs), nil
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
			subs = r.opts.Declarations.SubscribersFor(ctx, fed, inst.cls, []core.AttributeHandle{attr})
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

// emitScopeAdvisories diffs the per-attribute recipient sets of THIS
// update against the per-(object, subscriber) in-scope cache and emits
// AttributesInScope / AttributesOutOfScope (§6.17/§6.18) to subscribers
// whose region-overlap membership changed. Only attributes carried by
// the current update are (re)evaluated — the update path is where the
// overlap is computed (M37 Agent EA).
func (r *Registry) emitScopeAdvisories(
	ctx context.Context,
	fed core.FederationName,
	st *federationState,
	producer core.FederateHandle,
	inst *objectInstance,
	updateAttrs []core.AttributeHandle,
	perAttr map[core.AttributeHandle][]core.FederateHandle,
) {
	newlyIn := map[core.FederateHandle][]core.AttributeHandle{}
	newlyOut := map[core.FederateHandle][]core.AttributeHandle{}

	st.mu.Lock()
	if st.scope == nil {
		st.scope = map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle]struct{}{}
	}
	objScope := st.scope[inst.handle]
	if objScope == nil {
		objScope = map[core.FederateHandle]map[core.AttributeHandle]struct{}{}
		st.scope[inst.handle] = objScope
	}
	for _, attr := range updateAttrs { // sorted → deterministic
		cur := map[core.FederateHandle]struct{}{}
		for _, h := range perAttr[attr] {
			if h == producer {
				continue
			}
			cur[h] = struct{}{}
			set := objScope[h]
			if _, was := set[attr]; !was {
				if set == nil {
					set = map[core.AttributeHandle]struct{}{}
					objScope[h] = set
				}
				set[attr] = struct{}{}
				newlyIn[h] = append(newlyIn[h], attr)
			}
		}
		for h, set := range objScope {
			if _, was := set[attr]; !was {
				continue
			}
			if _, still := cur[h]; still {
				continue
			}
			delete(set, attr)
			if len(set) == 0 {
				delete(objScope, h)
			}
			newlyOut[h] = append(newlyOut[h], attr)
		}
	}
	nSeq := len(newlyIn) + len(newlyOut)
	var seq uint64
	if nSeq > 0 {
		seq = st.nextOutboundSeqRangeLocked(nSeq)
	}
	st.mu.Unlock()

	if nSeq == 0 {
		return
	}
	send := func(h core.FederateHandle, evt *rtiv1.FederateEvent) {
		evt.Seq = seq
		seq++
		_ = r.opts.Outbox.Send(ctx, fed, h, &outboundEvent{pb: evt})
	}
	// Out-of-scope first (the attrs left scope BEFORE this update's
	// reflect), then in-scope, each in sorted-recipient order.
	for _, h := range sortedAdvisoryRecipients(newlyOut) {
		send(h, &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_AttributesOutOfScope{
				AttributesOutOfScope: &rtiv1.AttributesOutOfScope{
					ObjectHandle:     uint64(inst.handle),
					AttributeHandles: attrHandlesToWire(newlyOut[h]),
				},
			},
		})
	}
	for _, h := range sortedAdvisoryRecipients(newlyIn) {
		send(h, &rtiv1.FederateEvent{
			Event: &rtiv1.FederateEvent_AttributesInScope{
				AttributesInScope: &rtiv1.AttributesInScope{
					ObjectHandle:     uint64(inst.handle),
					AttributeHandles: attrHandlesToWire(newlyIn[h]),
				},
			},
		})
	}
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

// newAttributeUpdatedEvent builds the eventlog adapter for an
// AttributeUpdated event.
func newAttributeUpdatedEvent(obj core.ObjectHandle, producer core.FederateHandle, attrs map[core.AttributeHandle][]byte, ts *core.LogicalTime, wallNs int64) *eventRecord {
	pb := &rtiv1.AttributeUpdated{
		ObjectHandle:           uint64(obj),
		ProducerFederateHandle: uint64(producer),
		Attributes:             copyAttrMap(attrs),
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	return &eventRecord{pb: &rtiv1.Event{
		WallNs: uint64(wallNs),
		Body:   &rtiv1.Event_AttrUpdated{AttrUpdated: pb},
	}}
}
