// Registry-side public methods for the object instance name
// reservation flow (M26 Phase F, IEEE 1516.1-2010 §6.1-6.5).
//
// The wire RPCs return Empty synchronously; the actual result is
// delivered as a FederateEvent on the requesting federate's stream.

package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ReserveObjectInstanceName attempts a single-name reservation and
// emits the matching success/failure event on the requesting
// federate's stream. Invalid arguments fail synchronously without a
// callback; reservation failure due to collision is reported via the
// event (ObjectInstanceNameReservationFailed).
func (r *RegistryOf[T]) ReserveObjectInstanceName(
	ctx context.Context,
	fed core.FederationName,
	holder core.FederateHandle,
	name string,
) error {
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	_, err := r.reservations.reserveWithAdmission(fed, holder, name, func(ok bool) error {
		kind := CallbackReservationFailed
		if ok {
			kind = CallbackReservationSucceeded
		}
		evt := &CallbackEventOf[T]{Sequence: st.nextOutboundSeq + 1, Kind: kind, ObjectName: name}
		if err := r.opts.Outbox.Send(ctx, fed, holder, evt); err != nil {
			return err
		}
		st.nextOutboundSeq = evt.Sequence
		return nil
	})
	return err
}

// ReleaseObjectInstanceName drops a single reservation. The
// synchronous return carries the spec error directly — release is
// not async because no callback is needed; the federate already
// knows whether it owned the reservation.
func (r *RegistryOf[T]) ReleaseObjectInstanceName(
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
func (r *RegistryOf[T]) ReserveMultipleObjectInstanceNames(
	ctx context.Context,
	fed core.FederationName,
	holder core.FederateHandle,
	names []string,
) error {
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	_, err := r.reservations.reserveBatchWithAdmission(fed, holder, names, func(colliding []string) error {
		kind := CallbackMultipleReservationSucceeded
		if len(colliding) != 0 {
			kind = CallbackMultipleReservationFailed
		}
		evt := &CallbackEventOf[T]{
			Sequence: st.nextOutboundSeq + 1, Kind: kind,
			ObjectNames: append([]string(nil), names...), CollidingNames: append([]string(nil), colliding...),
		}
		if err := r.opts.Outbox.Send(ctx, fed, holder, evt); err != nil {
			return err
		}
		st.nextOutboundSeq = evt.Sequence
		return nil
	})
	return err
}

// OnFederateResign drops every reservation owned by the resigning
// federate. Wired from the rtid composition root's resign hook.
// Idempotent.
func (r *RegistryOf[T]) OnFederateResign(fed core.FederationName, h core.FederateHandle) {
	r.reservations.OnFederateResign(fed, h)
}

// OnFederationDestroyed drops every reservation and registered name
// for the federation. Wired from the rtid composition root's
// destroy-federation hook. Idempotent.
func (r *RegistryOf[T]) OnFederationDestroyed(fed core.FederationName) {
	r.reservations.OnFederationDestroyed(fed)
	r.transports.removeFederation(fed)
	r.mu.Lock()
	if previous := r.federations[fed]; previous != nil {
		previous.knowledgeRetired.Store(true)
	}
	delete(r.federations, fed)
	r.mu.Unlock()
}

// LookupObjectInstanceByName implements core.ObjectInstanceQuery
// (§6.30). M27 Phase C — runtime handle resolution.
func (r *RegistryOf[T]) LookupObjectInstanceByName(fed core.FederationName, name string) (core.ObjectHandle, bool) {
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
func (r *RegistryOf[T]) LookupObjectInstanceName(fed core.FederationName, handle core.ObjectHandle) (string, bool) {
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
