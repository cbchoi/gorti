// Package object — change_*_transportation_type (M23 W3 / TASK-259).
//
// IEEE 1516.1-2010 §6.20: change_attribute_transportation_type.
// IEEE 1516.1-2010 §6.22: change_interaction_transportation_type.
//
// Overrides are included in callback metadata and attribute partitioning.
// Both configured transportation services currently use the gRPC channel;
// this store does not select a separate datagram network transport.
package object

import (
	"context"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// transportStore holds per-Registry transport-type overrides. One
// instance is kept on the Registry side-table — the in-memory
// federationState does not own it (matches the M22 nerStore-extension
// pattern).
type transportStore struct {
	mu sync.RWMutex
	// attrKey maps (federation, object, attribute) → transport type.
	attr map[attrTransportKey]core.TransportType
	// interKey maps (federation, publisher, interaction class) →
	// transport type. Per-publisher because multiple publishers of the
	// same class can choose different transports.
	inter     map[interTransportKey]core.TransportType
	tickets   map[string]*transportationTicket
	completed []string
}

type attrTransportKey struct {
	fed  core.FederationName
	obj  core.ObjectHandle
	attr core.AttributeHandle
}

type interTransportKey struct {
	fed       core.FederationName
	publisher core.FederateHandle
	cls       core.InteractionClassHandle
}

func newTransportStore() *transportStore {
	return &transportStore{
		attr:    map[attrTransportKey]core.TransportType{},
		inter:   map[interTransportKey]core.TransportType{},
		tickets: map[string]*transportationTicket{},
	}
}

func (s *transportStore) removeFederation(fed core.FederationName) {
	s.mu.Lock()
	s.retireTicketsLocked(func(c core.TransportationChange) bool { return c.Scope.Member.Federation == fed })
	for key := range s.attr {
		if key.fed == fed {
			delete(s.attr, key)
		}
	}
	for key := range s.inter {
		if key.fed == fed {
			delete(s.inter, key)
		}
	}
	s.mu.Unlock()
}

// ChangeAttributeTransportType records a per-instance, per-attribute
// transport override. Owner-only: returns ErrObjectNotOwned for
// non-owners.
//
// Errors:
//   - ErrObjectHandleInvalid if obj is unknown
//   - ErrObjectNotOwned if owner != objectInstance.owner
//   - ErrTransportTypeUnspecified if tt == TransportTypeUnspecified
func (r *RegistryOf[T]) ChangeAttributeTransportType(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tt core.TransportType,
) error {
	if r.opts.Ownership != nil {
		return r.RequestAttributeTransportationTypeChange(ctx, fed, owner, obj, attrs, tt)
	}
	if tt == core.TransportTypeUnspecified {
		return core.ErrTransportTypeUnspecified
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	inst, ok := st.instances[obj]
	if !ok {
		st.mu.Unlock()
		return core.ErrObjectHandleInvalid
	}
	if inst.owner != owner {
		st.mu.Unlock()
		return core.ErrObjectNotOwned
	}
	st.mu.Unlock()

	r.transports.mu.Lock()
	for _, a := range attrs {
		r.transports.attr[attrTransportKey{fed: fed, obj: obj, attr: a}] = tt
	}
	r.transports.mu.Unlock()
	return nil
}

// ChangeInteractionTransportType records a per-publisher per-class
// override. No publisher check (every joined federate may set its
// own override even if it's not currently publishing — the override
// kicks in when it does).
func (r *RegistryOf[T]) ChangeInteractionTransportType(
	_ context.Context,
	fed core.FederationName,
	publisher core.FederateHandle,
	cls core.InteractionClassHandle,
	tt core.TransportType,
) error {
	if tt == core.TransportTypeUnspecified {
		return core.ErrTransportTypeUnspecified
	}
	r.transports.mu.Lock()
	r.transports.inter[interTransportKey{fed: fed, publisher: publisher, cls: cls}] = tt
	r.transports.mu.Unlock()
	return nil
}

// AttributeTransportType returns the recorded override for
// (fed, obj, attr), or TransportTypeUnspecified if no override is
// in place. Read-only; safe for snapshot consumers.
func (r *RegistryOf[T]) AttributeTransportType(
	fed core.FederationName, obj core.ObjectHandle, attr core.AttributeHandle,
) core.TransportType {
	r.transports.mu.RLock()
	defer r.transports.mu.RUnlock()
	return r.transports.attr[attrTransportKey{fed: fed, obj: obj, attr: attr}]
}

// InteractionTransportType returns the recorded override for
// (fed, publisher, cls), or TransportTypeUnspecified if no override.
func (r *RegistryOf[T]) InteractionTransportType(
	fed core.FederationName, publisher core.FederateHandle, cls core.InteractionClassHandle,
) core.TransportType {
	r.transports.mu.RLock()
	defer r.transports.mu.RUnlock()
	return r.transports.inter[interTransportKey{fed: fed, publisher: publisher, cls: cls}]
}
