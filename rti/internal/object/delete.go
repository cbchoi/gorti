// Package object — DeleteObjectInstance (M23 W1 / TASK-247).
//
// IEEE 1516.1-2010 §6.16: owner federate signals that an object instance
// is no longer current. The RTI emits RemoveObjectInstance to every
// federate that subscribes to the class (excluding the owner itself),
// records ObjectDeleted in the event log (write-ahead), and removes the
// instance from per-federation state.

package object

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Delete removes obj from the federation. The deleter must own all
// attributes of the instance (cut-1 simplification: ownership is
// recorded only at register time on objectInstance.owner; per-attribute
// ownership lives in ownership.Manager but the simple owner check here
// matches Register's owner-only invariant). Subscribers receive a
// RemoveObjectInstance envelope.
//
// Errors:
//   - core.ErrObjectHandleInvalid if obj is not registered
//   - core.ErrObjectNotOwned if deleter != objectInstance.owner
//   - core.ErrObjectAlreadyDeleted if obj has been deleted
//
// Determinism: EventLog.Append → Outbox.Send fan-out → state mutation.
// Replay re-reads from the log; see SRS NFR-DET-1.
func (r *Registry) Delete(
	ctx context.Context,
	fed core.FederationName,
	deleter core.FederateHandle,
	obj core.ObjectHandle,
	ts *core.LogicalTime,
	tag []byte,
) error {
	if deleter == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}

	st := r.stateFor(fed)
	st.mu.Lock()
	inst, ok := st.instances[obj]
	if !ok {
		st.mu.Unlock()
		// Distinguish never-existed (Invalid) from previously-deleted
		// (AlreadyDeleted) only if we tracked tombstones; cut-1 keeps
		// it simple — both surface as Invalid.
		return core.ErrObjectHandleInvalid
	}
	if inst.owner != deleter {
		st.mu.Unlock()
		return core.ErrObjectNotOwned
	}

	// M37 EB-3 — §8.1.2 outgoing-TSO timestamp validation (a TSO
	// delete is a timestamped message like any other), BEFORE the
	// eventlog write-ahead so rejected deletes never enter the replay
	// log and the instance survives.
	if err := r.validateOutgoingTSO(fed, deleter, ts); err != nil {
		st.mu.Unlock()
		return err
	}

	// (a) Eventlog write-ahead.
	if r.opts.EventLog != nil {
		ev := newObjectDeletedEvent(obj, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			st.mu.Unlock()
			return fmt.Errorf("object: delete %d in %q: eventlog append: %w", obj, fed, err)
		}
	}

	// (b) Resolve subscribers BEFORE removing the instance. §6.16:
	// removeObjectInstance goes to every federate that knows the
	// instance — the same recipient set as the register-side §6.9
	// discover fan-out. M37 EB-1: the previous hardcoded {1} probe
	// missed subscribers of higher attribute handles (e.g. a Position
	// subscriber on attrs {2,3} discovered the instance but never got
	// the REMOVE). subscribersForDiscover probes the full
	// fanoutAttrProbe range and is DDM-aware (region-scoped
	// subscribers included), mirroring the register/update fan-out.
	subs := r.subscribersForDiscover(ctx, fed, inst)

	// (c) Take the snapshot we need for the wire frame, then drop the
	// lock before fanout (matches fanoutReflect's pattern).
	cls := inst.cls
	delete(st.instances, obj)
	delete(st.nameToHandle, inst.name)
	// M37 EB-4 — drop the (subscriber, object) discover records for
	// the deleted instance so the idempotency table doesn't leak.
	for k := range st.discovered {
		if k.obj == obj {
			delete(st.discovered, k)
		}
	}
	// M37 Agent EA — drop the §6.17/§6.18 in-scope cache with the
	// instance.
	delete(st.scope, obj)
	st.mu.Unlock()

	// (d) Build the envelope. ts is *core.LogicalTime; map to
	// proto's optional double field. Tag passes through verbatim.
	pb := &rtiv1.RemoveObjectInstance{
		ObjectHandle:    uint64(obj),
		UserSuppliedTag: append([]byte(nil), tag...),
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}

	// (e) Allocate a seq range for the fanout — per-recipient delivery
	// order matches existing fanoutReflect/fanoutReceive shape.
	st.mu.Lock()
	startSeq := st.nextOutboundSeqRangeLocked(len(subs))
	st.mu.Unlock()
	seq := startSeq

	for _, sub := range subs {
		if sub == deleter {
			continue
		}
		evt := &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq:   seq,
			Event: &rtiv1.FederateEvent_Remove{Remove: pb},
		}}
		seq++
		// M22 — gate TSO removes via the same gate as updates/interactions.
		if ts != nil && r.opts.TSOGate != nil {
			if r.opts.TSOGate.ShouldDeliverNow(fed, sub, *ts) {
				_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
			} else {
				_ = r.opts.TSOGate.BufferTSO(ctx, fed, sub, *ts, evt)
			}
		} else {
			_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
		}
	}
	_ = cls // reserved for class-scoped subscriber lookups in a future cut
	return nil
}

// newObjectDeletedEvent builds the eventlog adapter for an
// ObjectDeleted event. Mirrors newObjectRegisteredEvent.
func newObjectDeletedEvent(handle core.ObjectHandle, ts *core.LogicalTime, wallNs int64) *eventRecord {
	od := &rtiv1.ObjectDeleted{ObjectHandle: uint64(handle)}
	if ts != nil {
		v := float64(*ts)
		od.LogicalTime = &v
	}
	return &eventRecord{
		pb: &rtiv1.Event{
			WallNs: uint64(wallNs),
			Body:   &rtiv1.Event_ObjDeleted{ObjDeleted: od},
		},
	}
}
