package object

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// updateAttributes implements core.ObjectRegistry.UpdateAttributes.
//
// Flow:
//  1. Look up the instance; reject unknown with ErrObjectNotFound.
//  2. Verify the producer publishes EVERY attribute in the update via
//     declaration.Manager. (Cut 1: any-attr ownership; full
//     ownership-management arrives in cut 2 with explicit Acquire/Divest.)
//  3. Persist AttributeUpdated to the EventLog (write-ahead).
//  4. Fan ReflectAttributeValues to subscribers of any updated attribute,
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

	updateAttrs := sortedAttrHandles(attrs)

	if r.opts.EventLog != nil {
		ev := newAttributeUpdatedEvent(obj, producer, attrs, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			return fmt.Errorf("object: update %d in %q: eventlog append: %w", obj, fed, err)
		}
	}

	r.fanoutReflect(ctx, fed, st, producer, inst, attrs, updateAttrs, ts)
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
func (r *Registry) fanoutReflect(ctx context.Context, fed core.FederationName, st *federationState, producer core.FederateHandle, inst *objectInstance, attrs map[core.AttributeHandle][]byte, updateAttrs []core.AttributeHandle, ts *core.LogicalTime) {
	subs := r.opts.Declarations.SubscribersFor(ctx, fed, inst.cls, updateAttrs)
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		evt := r.buildReflectEvent(st, inst, attrs, ts)
		_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
	}
}

func (r *Registry) buildReflectEvent(st *federationState, inst *objectInstance, attrs map[core.AttributeHandle][]byte, ts *core.LogicalTime) *outboundEvent {
	st.mu.Lock()
	seq := st.nextOutboundSeqLocked()
	st.mu.Unlock()
	pb := &rtiv1.ReflectAttributeValues{
		ObjectHandle:      uint64(inst.handle),
		ObjectClassHandle: uint64(inst.cls),
		Attributes:        copyAttrMap(attrs),
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	return &outboundEvent{pb: &rtiv1.FederateEvent{
		Seq:   seq,
		Event: &rtiv1.FederateEvent_Reflect{Reflect: pb},
	}}
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
