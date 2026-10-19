package object

import (
	"context"
	"crypto/rand"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type transportationTicket struct {
	change                    core.TransportationChange
	actualAttributes          []core.AttributeHandle
	activated                 bool
	checkpointAcknowledgement bool
}

func cloneTransportationChange(c core.TransportationChange) core.TransportationChange {
	c.Ticket = slices.Clone(c.Ticket)
	c.Attributes = slices.Clone(c.Attributes)
	return c
}

func (r *RegistryOf[T]) validateAttributeTransportation(ctx context.Context, st *federationStateOf[T], scope core.TransportationScope, obj core.ObjectHandle, attrs []core.AttributeHandle, tt core.TransportType) ([]core.AttributeHandle, error) {
	if !validTransportation(tt) {
		return nil, core.ErrTransportTypeUnspecified
	}
	fed, caller := scope.Member.Federation, scope.Member.Handle
	return r.validateOwnedKnownAttributes(ctx, st, fed, caller, obj, attrs)
}

func (r *RegistryOf[T]) validateOwnedKnownAttributes(ctx context.Context, st *federationStateOf[T], fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	inst, err := r.transportationObject(st, fed, caller, obj, attrs)
	if err != nil {
		return nil, err
	}
	actual, err := r.knownAttributes(ctx, fed, st, caller, inst, attrs)
	if err != nil {
		return nil, err
	}
	for _, attr := range actual {
		owned := false
		if r.opts.Ownership != nil {
			owned = r.opts.Ownership.IsOwnedBy(fed, caller, obj, attr)
		} else {
			owned = inst.owner == caller && slices.Contains(r.opts.Declarations.PublishersFor(ctx, fed, inst.cls, attr), caller)
		}
		if !owned {
			return nil, core.ErrAttributeNotOwned
		}
	}
	return actual, nil
}

func (r *RegistryOf[T]) PrepareAttributeTransportationChange(ctx context.Context, scope core.TransportationScope, obj core.ObjectHandle, attrs []core.AttributeHandle, tt core.TransportType) (core.TransportationChange, error) {
	st := r.stateFor(scope.Member.Federation)
	st.mu.Lock()
	defer st.mu.Unlock()
	attrs = slices.Clone(attrs)
	slices.Sort(attrs)
	attrs = slices.Compact(attrs)
	actual, err := r.validateAttributeTransportation(ctx, st, scope, obj, attrs, tt)
	if err != nil {
		return core.TransportationChange{}, err
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	r.transports.retireStaleScopeLocked(scope)
	for _, pending := range r.transports.tickets {
		if pending.activated || pending.change.Scope.Member.Federation != scope.Member.Federation || pending.change.Object != obj {
			continue
		}
		for _, attr := range actual {
			if slices.Contains(pending.actualAttributes, attr) {
				return core.TransportationChange{}, core.ErrAttributeAlreadyBeingChanged
			}
		}
	}
	change, err := r.transports.prepareLocked(core.TransportationChange{Scope: scope, Object: obj, Attributes: attrs, Transportation: tt})
	if err == nil {
		r.transports.tickets[string(change.Ticket)].actualAttributes = actual
	}
	return change, err
}

func (r *RegistryOf[T]) PrepareInteractionTransportationChange(ctx context.Context, scope core.TransportationScope, cls core.InteractionClassHandle, tt core.TransportType) (core.TransportationChange, error) {
	if !validTransportation(tt) {
		return core.TransportationChange{}, core.ErrTransportTypeUnspecified
	}
	fed := scope.Member.Federation
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return core.TransportationChange{}, err
	}
	if err := transportationInteractionDefined(fom, cls); err != nil {
		return core.TransportationChange{}, err
	}
	if !r.producerPublishesInteraction(ctx, fed, scope.Member.Handle, cls) {
		return core.TransportationChange{}, core.ErrInteractionClassNotPublished
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	r.transports.retireStaleScopeLocked(scope)
	for _, pending := range r.transports.tickets {
		if !pending.activated && pending.change.Scope == scope && pending.change.Interaction == cls {
			return core.TransportationChange{}, core.ErrInteractionClassAlreadyBeingChanged
		}
	}
	return r.transports.prepareLocked(core.TransportationChange{Scope: scope, Interaction: cls, Transportation: tt})
}

func (s *transportStore) prepareLocked(change core.TransportationChange) (core.TransportationChange, error) {
	if change.Scope.Member.Handle == 0 || change.Scope.Member.Incarnation == 0 {
		return core.TransportationChange{}, core.ErrTransportationTicketInvalid
	}
	if transportationCheckpointCapacityUse(s) >= 4096 {
		return core.TransportationChange{}, core.ErrTransportationPendingLimit
	}
	change.Ticket = make([]byte, 32)
	for {
		if _, err := rand.Read(change.Ticket); err != nil {
			return core.TransportationChange{}, err
		}
		if s.tickets[string(change.Ticket)] == nil && transportationCheckpointLeasedTickets(s)[string(change.Ticket)] == nil {
			break
		}
	}
	s.tickets[string(change.Ticket)] = &transportationTicket{change: change}
	return cloneTransportationChange(change), nil
}

func (r *RegistryOf[T]) ActivateTransportationChange(ctx context.Context, scope core.TransportationScope, ticket []byte) (core.TransportationChange, error) {
	if len(ticket) != 32 {
		return core.TransportationChange{}, core.ErrTransportationTicketInvalid
	}
	st := r.stateFor(scope.Member.Federation)
	st.mu.Lock()
	defer st.mu.Unlock()
	r.transports.mu.RLock()
	entry := r.transports.tickets[string(ticket)]
	if entry == nil || entry.change.Scope != scope {
		r.transports.mu.RUnlock()
		return core.TransportationChange{}, core.ErrTransportationTicketInvalid
	}
	change, activated := cloneTransportationChange(entry.change), entry.activated
	checkpointAcknowledgement := entry.checkpointAcknowledgement
	r.transports.mu.RUnlock()
	if activated {
		if checkpointAcknowledgement {
			if err := r.consumeCheckpointTransportationAcknowledgement(ticket, entry); err != nil {
				return core.TransportationChange{}, err
			}
		}
		return change, nil
	}
	if change.Object != 0 {
		if _, err := r.validateAttributeTransportation(ctx, st, scope, change.Object, change.Attributes, change.Transportation); err != nil {
			return core.TransportationChange{}, err
		}
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	// Object pin excludes ownership changes. Lifecycle callers retain the outer
	// admission gate; no saved state or callback receipt can resurrect this ticket.
	if r.transports.tickets[string(ticket)] != entry {
		return core.TransportationChange{}, core.ErrTransportationTicketInvalid
	}
	if change.Object != 0 {
		for _, attr := range entry.actualAttributes {
			r.transports.attr[attrTransportKey{scope.Member.Federation, change.Object, attr}] = change.Transportation
		}
	} else {
		r.transports.inter[interTransportKey{scope.Member.Federation, scope.Member.Handle, change.Interaction}] = change.Transportation
	}
	entry.activated = true
	r.transports.completed = append(r.transports.completed, string(ticket))
	if len(r.transports.completed) > 256 {
		delete(r.transports.tickets, r.transports.completed[0])
		r.transports.completed = r.transports.completed[1:]
	}
	return change, nil
}

func (s *transportStore) retireTicketsLocked(matches func(core.TransportationChange) bool) {
	for key, ticket := range s.tickets {
		if matches(ticket.change) {
			delete(s.tickets, key)
		}
	}
	s.completed = slices.DeleteFunc(s.completed, func(key string) bool { return s.tickets[key] == nil })
}

func (s *transportStore) retireStaleScopeLocked(scope core.TransportationScope) {
	s.retireTicketsLocked(func(c core.TransportationChange) bool {
		return c.Scope.Member.Federation == scope.Member.Federation && c.Scope.Member.Handle == scope.Member.Handle && c.Scope != scope
	})
}

var _ core.TransportationActivation = (*Registry)(nil)
