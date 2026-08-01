// Package object — local_delete + request_attribute_value_update
// (M23 W2 / TASK-253).
//
// Per IEEE 1516.1-2010:
//   §6.18 local_delete_object_instance — federate-local cleanup
//   §6.24 request_attribute_value_update — pull-style resync (instance)
//   §6.25 request_class_attribute_value_update — class-scoped variant
//
// Both request flavors emit ProvideAttributeValueUpdate to the
// owner(s) via Outbox. Owner is expected to respond with
// UpdateAttributeValues (existing surface).

package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// LocalDelete records the federate-local cleanup; no peer notification.
// Cut-1 simplification: does NOT remove the instance from the
// federation's instances map. Other federates continue to see it; the
// requester is expected to drop their own local view (the SDK can
// hide it client-side or the application can ignore future reflects).
//
// Errors: ErrObjectHandleInvalid if obj does not exist.
func (r *Registry) LocalDelete(
	_ context.Context,
	fed core.FederationName,
	_ core.FederateHandle,
	obj core.ObjectHandle,
) error {
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.instances[obj]; !ok {
		return core.ErrObjectHandleInvalid
	}
	// No state mutation — other subscribers retain visibility.
	return nil
}

// RequestAttributeValueUpdate emits a ProvideAttributeValueUpdate
// event to the owner of obj with the requested attribute set + tag.
// IEEE 1516.1-2010 §6.24.
//
// Errors: ErrObjectHandleInvalid.
func (r *Registry) RequestAttributeValueUpdate(
	ctx context.Context,
	fed core.FederationName,
	_ core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	st := r.stateFor(fed)
	st.mu.Lock()
	inst, ok := st.instances[obj]
	if !ok {
		st.mu.Unlock()
		return core.ErrObjectHandleInvalid
	}
	owner := inst.owner
	startSeq := st.nextOutboundSeqRangeLocked(1)
	st.mu.Unlock()

	pb := &rtiv1.ProvideAttributeValueUpdate{
		ObjectHandle:     uint64(obj),
		AttributeHandles: handleSliceToUint64(attrs),
		UserSuppliedTag:  append([]byte(nil), tag...),
	}
	evt := &outboundEvent{pb: &rtiv1.FederateEvent{
		Seq:   startSeq,
		Event: &rtiv1.FederateEvent_ProvideUpdate{ProvideUpdate: pb},
	}}
	return r.opts.Outbox.Send(ctx, fed, owner, evt)
}

// RequestClassAttributeValueUpdate enumerates every instance of cls
// in the federation and emits one ProvideAttributeValueUpdate per
// unique owner. IEEE 1516.1-2010 §6.25.
func (r *Registry) RequestClassAttributeValueUpdate(
	ctx context.Context,
	fed core.FederationName,
	_ core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	st := r.stateFor(fed)
	st.mu.Lock()

	type ownerKey struct {
		owner core.FederateHandle
		obj   core.ObjectHandle
	}
	matches := make([]ownerKey, 0, len(st.instances))
	for h, inst := range st.instances {
		if inst.cls == cls {
			matches = append(matches, ownerKey{owner: inst.owner, obj: h})
		}
	}
	startSeq := st.nextOutboundSeqRangeLocked(len(matches))
	st.mu.Unlock()

	seq := startSeq
	wireAttrs := handleSliceToUint64(attrs)
	for _, k := range matches {
		pb := &rtiv1.ProvideAttributeValueUpdate{
			ObjectHandle:     uint64(k.obj),
			AttributeHandles: wireAttrs,
			UserSuppliedTag:  append([]byte(nil), tag...),
		}
		evt := &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq:   seq,
			Event: &rtiv1.FederateEvent_ProvideUpdate{ProvideUpdate: pb},
		}}
		seq++
		_ = r.opts.Outbox.Send(ctx, fed, k.owner, evt)
	}
	return nil
}

// handleSliceToUint64 converts a []core.AttributeHandle to []uint64
// for the wire envelope. Returns a fresh slice (defensive copy).
func handleSliceToUint64(in []core.AttributeHandle) []uint64 {
	out := make([]uint64, len(in))
	for i, h := range in {
		out[i] = uint64(h)
	}
	return out
}
