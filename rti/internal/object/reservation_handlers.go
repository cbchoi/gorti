// Registry-side public methods for the object instance name
// reservation flow (M26 Phase F, IEEE 1516.1-2010 §6.1-6.5).
//
// The wire RPCs return Empty synchronously; the actual result is
// delivered as a FederateEvent on the requesting federate's stream.

package object

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ReserveObjectInstanceName attempts a single-name reservation and
// emits the matching success/failure event on the requesting
// federate's stream. The synchronous return surfaces only outbox
// errors; reservation failure due to collision is reported via the
// event (ObjectInstanceNameReservationFailed).
func (r *Registry) ReserveObjectInstanceName(
	ctx context.Context,
	fed core.FederationName,
	holder core.FederateHandle,
	name string,
) error {
	ok, err := r.reservations.Reserve(fed, holder, name)
	// The validation-grade error (empty name) is reported as a
	// failure event for API symmetry; collisions report as failure
	// events too. The synchronous return stays nil unless the outbox
	// send fails.
	st := r.stateFor(fed)
	st.mu.Lock()
	seq := st.nextOutboundSeqLocked()
	st.mu.Unlock()

	var evt *outboundEvent
	if ok && err == nil {
		evt = &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq: seq,
			Event: &rtiv1.FederateEvent_ReservationSucceeded{
				ReservationSucceeded: &rtiv1.ObjectInstanceNameReservationSucceeded{
					ObjectName: name,
				},
			},
		}}
	} else {
		evt = &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq: seq,
			Event: &rtiv1.FederateEvent_ReservationFailed{
				ReservationFailed: &rtiv1.ObjectInstanceNameReservationFailed{
					ObjectName: name,
				},
			},
		}}
	}
	return r.opts.Outbox.Send(ctx, fed, holder, evt)
}

// ReleaseObjectInstanceName drops a single reservation. The
// synchronous return carries the spec error directly — release is
// not async because no callback is needed; the federate already
// knows whether it owned the reservation.
func (r *Registry) ReleaseObjectInstanceName(
	_ context.Context,
	fed core.FederationName,
	holder core.FederateHandle,
	name string,
) error {
	return r.reservations.Release(fed, holder, name)
}

// ReserveMultipleObjectInstanceNames attempts an atomic batch
// reservation. Emits MultipleObjectInstanceNameReservationSucceeded
// on success (with all names), or
// MultipleObjectInstanceNameReservationFailed on any collision
// (with the colliding names listed).
func (r *Registry) ReserveMultipleObjectInstanceNames(
	ctx context.Context,
	fed core.FederationName,
	holder core.FederateHandle,
	names []string,
) error {
	colliding, _ := r.reservations.ReserveBatch(fed, holder, names)
	st := r.stateFor(fed)
	st.mu.Lock()
	seq := st.nextOutboundSeqLocked()
	st.mu.Unlock()

	var evt *outboundEvent
	if len(colliding) == 0 {
		evt = &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq: seq,
			Event: &rtiv1.FederateEvent_ReservationMultiSucceeded{
				ReservationMultiSucceeded: &rtiv1.MultipleObjectInstanceNameReservationSucceeded{
					ObjectNames: append([]string(nil), names...),
				},
			},
		}}
	} else {
		evt = &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq: seq,
			Event: &rtiv1.FederateEvent_ReservationMultiFailed{
				ReservationMultiFailed: &rtiv1.MultipleObjectInstanceNameReservationFailed{
					RequestedNames: append([]string(nil), names...),
					CollidingNames: colliding,
				},
			},
		}}
	}
	return r.opts.Outbox.Send(ctx, fed, holder, evt)
}

// OnFederateResign drops every reservation owned by the resigning
// federate. Wired from the rtid composition root's resign hook.
// Idempotent.
func (r *Registry) OnFederateResign(fed core.FederationName, h core.FederateHandle) {
	r.reservations.OnFederateResign(fed, h)
}

// OnFederationDestroyed drops every reservation and registered name
// for the federation. Wired from the rtid composition root's
// destroy-federation hook. Idempotent.
func (r *Registry) OnFederationDestroyed(fed core.FederationName) {
	r.reservations.OnFederationDestroyed(fed)
}

// LookupObjectInstanceByName implements core.ObjectInstanceQuery
// (§6.30). M27 Phase C — runtime handle resolution.
func (r *Registry) LookupObjectInstanceByName(fed core.FederationName, name string) (core.ObjectHandle, bool) {
	r.mu.RLock()
	st, ok := r.federations[fed]
	r.mu.RUnlock()
	if !ok {
		return core.InvalidObjectHandle, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	h, exists := st.nameToHandle[name]
	if !exists {
		return core.InvalidObjectHandle, false
	}
	return h, true
}

// LookupObjectInstanceName implements core.ObjectInstanceQuery (§6.31).
func (r *Registry) LookupObjectInstanceName(fed core.FederationName, handle core.ObjectHandle) (string, bool) {
	r.mu.RLock()
	st, ok := r.federations[fed]
	r.mu.RUnlock()
	if !ok {
		return "", false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, exists := st.instances[handle]
	if !exists {
		return "", false
	}
	return inst.name, true
}

// Compile-time guard: the reservation flow uses core.ErrObjectInstanceNameReservedByOther
// in Register's collision path; keep the import live.
var _ = errors.Is
