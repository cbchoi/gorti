package object

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

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
	subs := r.subscribersForReflect(ctx, fed, inst, updateAttrs)
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
func (r *Registry) subscribersForReflect(ctx context.Context, fed core.FederationName, inst *objectInstance, updateAttrs []core.AttributeHandle) []core.FederateHandle {
	if r.opts.DDM == nil || !r.opts.DDM.HasObjectAssociations(fed, inst.handle) {
		return r.opts.Declarations.SubscribersFor(ctx, fed, inst.cls, updateAttrs)
	}
	// DDM-aware union across the updated attribute set. For each
	// attribute, fall back to the cut-1 subscribers when the
	// publisher did not associate any region for that attr (the
	// per-attr nil branch).
	union := map[core.FederateHandle]struct{}{}
	for _, attr := range updateAttrs {
		pubRegions := r.opts.DDM.PublisherRegionsFor(fed, inst.handle, attr)
		var subs []core.FederateHandle
		if len(pubRegions) == 0 {
			subs = r.opts.Declarations.SubscribersFor(ctx, fed, inst.cls, []core.AttributeHandle{attr})
		} else {
			subs = r.opts.DDM.SubscribersForUpdate(fed, inst.cls, attr, pubRegions)
		}
		for _, h := range subs {
			union[h] = struct{}{}
		}
	}
	out := make([]core.FederateHandle, 0, len(union))
	for h := range union {
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
