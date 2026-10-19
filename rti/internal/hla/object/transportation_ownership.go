package object

import (
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// transportationOwnershipRelease is prepared with the object pin held and
// released after the ownership manager unlocks, before any acquisition callback.
// Retiring overrides before unpinning prevents a new owner's change from being
// erased by a delayed post-commit observer, including transfer away and back.
func (r *RegistryOf[T]) transportationOwnershipRelease(fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle, release func()) func() {
	release = r.ddmOwnershipRelease(fed, obj, attrs, release)
	ownership, ok := r.opts.Ownership.(ownershipQuerySnapshot)
	if !ok {
		return release
	}
	type ownerState struct {
		owner core.FederateHandle
		owned bool
	}
	before := make(map[core.AttributeHandle]ownerState, len(attrs))
	for _, attr := range attrs {
		owner, owned := ownership.QueryOwnership(fed, obj, attr)
		before[attr] = ownerState{owner, owned}
	}
	return sync.OnceFunc(func() {
		defer release()
		var changed []core.AttributeHandle
		for attr, previous := range before {
			owner, owned := ownership.QueryOwnership(fed, obj, attr)
			if previous != (ownerState{owner, owned}) {
				changed = append(changed, attr)
			}
		}
		if len(changed) == 0 {
			return
		}
		r.transports.mu.Lock()
		defer r.transports.mu.Unlock()
		for key, ticket := range r.transports.tickets {
			if ticket.change.Scope.Member.Federation != fed || ticket.change.Object != obj {
				continue
			}
			before := len(ticket.change.Attributes)
			known, actual := ticket.change.Attributes[:0], ticket.actualAttributes[:0]
			for i, attr := range ticket.actualAttributes {
				if !slices.Contains(changed, attr) {
					known = append(known, ticket.change.Attributes[i])
					actual = append(actual, attr)
				}
			}
			ticket.change.Attributes, ticket.actualAttributes = known, actual
			// A not-yet-invoked confirmation keeps only the attributes continuously
			// owned since prepare. Reacquisition cannot reinsert a removed attribute.
			if ticket.activated && len(ticket.change.Attributes) != before {
				delete(r.transports.tickets, key)
			}
		}
		r.transports.completed = slices.DeleteFunc(r.transports.completed, func(key string) bool { return r.transports.tickets[key] == nil })
		for _, attr := range changed {
			delete(r.transports.attr, attrTransportKey{fed: fed, obj: obj, attr: attr})
		}
	})
}

// Called with the object pin held, before resignation takes the ownership lock.
// Capture only the departing owner's overrides; rollback must leave them intact.
func (r *RegistryOf[T]) transportationOwnedAttributes(st *federationStateOf[T], fed core.FederationName, owner core.FederateHandle) []attrTransportKey {
	r.transports.mu.RLock()
	var candidates []attrTransportKey
	for key := range r.transports.attr {
		if key.fed == fed {
			candidates = append(candidates, key)
		}
	}
	r.transports.mu.RUnlock()
	var owned []attrTransportKey
	for _, key := range candidates {
		if r.opts.Ownership != nil {
			if r.opts.Ownership.IsOwnedBy(fed, owner, key.obj, key.attr) {
				owned = append(owned, key)
			}
		} else if inst := st.instances[key.obj]; inst != nil && inst.owner == owner {
			owned = append(owned, key)
		}
	}
	return owned
}
