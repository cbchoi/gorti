package object

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

// Capacity and entropy are reserved before domain import. Store access and
// this side table share transportStore.mu; ordinary prepares respect the lease.
var transportationCheckpointLeases sync.Map

func transportationCheckpointLeasedTickets(store *transportStore) map[string]*transportationTicket {
	value, _ := transportationCheckpointLeases.Load(store)
	if value == nil {
		return nil
	}
	return value.(map[string]*transportationTicket)
}

func transportationCheckpointCapacityUse(store *transportStore) int {
	return len(store.tickets) + len(transportationCheckpointLeasedTickets(store))
}

type transportationCheckpointFOM struct{ model core.FOMHandle }

func (r transportationCheckpointFOM) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return r.model, nil
}
func (r transportationCheckpointFOM) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return nil, fmt.Errorf("checkpoint FOM view is read-only")
}

type transportationCheckpointOwners struct{ state *ownership.ManagerState }

func (o transportationCheckpointOwners) IsOwnedBy(_ core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle) bool {
	if o.state != nil {
		for _, row := range o.state.Owners {
			if row.Object == obj && row.Attribute == attr {
				return row.Owner == h
			}
		}
	}
	return false
}

// Reuses the live attribute validator with detached imported object state and
// immutable FOM/ownership views; no live mutation, tickets or fake handles.
func ValidateTransportationCheckpointState[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, recipient core.FederateHandle, binding TransportationCheckpointBinding, state *RegistryStateOf[T], owned *ownership.ManagerState, model core.FOMHandle, original ...[]core.AttributeHandle) error {
	if err := ValidateTransportationCheckpointBinding(binding); err != nil {
		return err
	}
	prepared, err := prepareRegistryStateOf[T](fed, state)
	if err != nil {
		return err
	}
	if model == nil || !model.IsValid() || recipient == 0 {
		return core.ErrTransportationTicketInvalid
	}
	if binding.Interaction != 0 {
		return transportationInteractionDefined(model, binding.Interaction)
	}
	view := &RegistryOf[T]{opts: OptionsOf[T]{FOMs: transportationCheckpointFOM{model: model}, Ownership: transportationCheckpointOwners{state: owned}}}
	inst, err := view.transportationObject(prepared.state, fed, recipient, binding.Object, binding.Attributes)
	if err != nil {
		return err
	}
	for _, attrs := range original {
		if _, err := view.knownAttributes(ctx, fed, prepared.state, recipient, inst, attrs); err != nil {
			return err
		}
	}
	var actual []core.AttributeHandle
	if binding.Activated {
		actual, err = view.knownAttributes(ctx, fed, prepared.state, recipient, inst, binding.Attributes)
	} else {
		actual, err = view.validateAttributeTransportation(ctx, prepared.state, core.TransportationScope{Member: core.MemberIdentity{Federation: fed, Handle: recipient}}, binding.Object, binding.Attributes, binding.Transportation)
	}
	if err != nil {
		return err
	}
	if !slices.Equal(actual, binding.ActualAttributes) {
		return fmt.Errorf("transportation checkpoint: saved registered attribute mapping mismatch")
	}
	return nil
}

func (r *RegistryOf[T]) PrepareTransportationCheckpoint(ctx context.Context, requests []TransportationCheckpointRestore, state *RegistryStateOf[T], owned *ownership.ManagerState, model core.FOMHandle) (*TransportationCheckpointReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := &TransportationCheckpointReservation{rollback: func() {}}
	if len(requests) == 0 {
		return p, nil
	}
	fed := requests[0].Scope.Member.Federation
	if err := validateTransportationCheckpointRequests(ctx, fed, requests, state, owned, model); err != nil {
		return nil, err
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	leased := transportationCheckpointLeasedTickets(r.transports)
	other := 0
	for _, entry := range r.transports.tickets {
		if entry.change.Scope.Member.Federation != fed {
			other++
		}
	}
	if len(requests) > 4096-other-len(leased) {
		return nil, core.ErrTransportationPendingLimit
	}
	if leased == nil {
		leased = map[string]*transportationTicket{}
		transportationCheckpointLeases.Store(r.transports, leased)
	}
	entries := map[string]*transportationTicket{}
	leaseActive := true
	cleanupLease := func() {
		if !leaseActive {
			return
		}
		for key := range entries {
			delete(leased, key)
		}
		if len(leased) == 0 {
			transportationCheckpointLeases.Delete(r.transports)
		}
		leaseActive = false
	}
	for _, request := range requests {
		b := cloneTransportationCheckpoint(request.Binding)
		ticket, err := r.reserveCheckpointTicket(leased)
		if err != nil {
			cleanupLease()
			return nil, err
		}
		entry := &transportationTicket{change: core.TransportationChange{Ticket: ticket, Scope: request.Scope, Object: b.Object, Attributes: b.Attributes, Interaction: b.Interaction, Transportation: b.Transportation}, actualAttributes: b.ActualAttributes, activated: b.Activated, checkpointAcknowledgement: b.Activated}
		entries[string(ticket)], leased[string(ticket)] = entry, entry
		p.changes = append(p.changes, cloneTransportationChange(entry.change))
	}
	r.setCheckpointReservationActions(p, entries, cleanupLease)
	return p, nil
}

func validateTransportationCheckpointRequests[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, requests []TransportationCheckpointRestore, state *RegistryStateOf[T], owned *ownership.ManagerState, model core.FOMHandle) error {
	for i, request := range requests {
		id := request.Scope.Member
		if fed == "" || id.Federation != fed || id.Handle == 0 || id.Handle == ^core.FederateHandle(0) || id.Incarnation == 0 {
			return core.ErrTransportationTicketInvalid
		}
		if err := ValidateTransportationCheckpointState(ctx, fed, id.Handle, request.Binding, state, owned, model); err != nil {
			return err
		}
		for _, prior := range requests[:i] {
			if err := transportationCheckpointConflict(request, prior); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RegistryOf[T]) reserveCheckpointTicket(leased map[string]*transportationTicket) ([]byte, error) {
	ticket := make([]byte, 32)
	for {
		if _, err := rand.Read(ticket); err != nil {
			return nil, err
		}
		if r.transports.tickets[string(ticket)] == nil && leased[string(ticket)] == nil {
			break
		}
	}
	return ticket, nil
}

func (r *RegistryOf[T]) setCheckpointReservationActions(p *TransportationCheckpointReservation, entries map[string]*transportationTicket, cleanupLease func()) {
	p.install = func() {
		r.transports.mu.Lock()
		defer r.transports.mu.Unlock()
		for key, entry := range entries {
			r.transports.tickets[key] = entry
		}
		cleanupLease()
	}
	p.rollback = func() {
		r.transports.mu.Lock()
		defer r.transports.mu.Unlock()
		cleanupLease()
		for key, entry := range entries {
			if r.transports.tickets[key] == entry {
				delete(r.transports.tickets, key)
			}
		}
		r.transports.completed = slices.DeleteFunc(r.transports.completed, func(key string) bool { return r.transports.tickets[key] == nil })
	}
}
