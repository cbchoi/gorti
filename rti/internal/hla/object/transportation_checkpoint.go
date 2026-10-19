package object

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// TransportationCheckpointBinding is an intent, not activation authority.
// Attributes retain the caller's known coordinates; ActualAttributes retain
// their registered-class mapping after permanent ownership-loss projection.
type TransportationCheckpointBinding struct {
	Object           core.ObjectHandle           `json:"object,omitempty"`
	Attributes       []core.AttributeHandle      `json:"attributes"`
	ActualAttributes []core.AttributeHandle      `json:"actual_attributes"`
	Interaction      core.InteractionClassHandle `json:"interaction,omitempty"`
	Transportation   core.TransportType          `json:"transportation"`
	Activated        bool                        `json:"activated"`
}

type TransportationCheckpointSource struct {
	Scope   core.TransportationScope
	Binding TransportationCheckpointBinding
}

// The map keys and Scope are live lookup metadata. Never serialize this map;
// persist only a matched Binding after stripping the wire capability.
type TransportationCheckpointSnapshot map[string]TransportationCheckpointSource

func cloneTransportationCheckpoint(b TransportationCheckpointBinding) TransportationCheckpointBinding {
	b.Attributes = slices.Clone(b.Attributes)
	b.ActualAttributes = slices.Clone(b.ActualAttributes)
	return b
}

func ValidateTransportationCheckpointBinding(b TransportationCheckpointBinding) error {
	if !validTransportationBindingShape(b) {
		return core.ErrTransportationTicketInvalid
	}
	actual := make(map[core.AttributeHandle]bool, len(b.ActualAttributes))
	for i, attr := range b.Attributes {
		registered := b.ActualAttributes[i]
		if attr == 0 || attr == ^core.AttributeHandle(0) || registered == 0 || registered == ^core.AttributeHandle(0) || (i > 0 && b.Attributes[i-1] >= attr) || actual[registered] {
			return core.ErrTransportationTicketInvalid
		}
		actual[registered] = true
	}
	return nil
}

// Export under the caller's exclusive checkpoint, BEFORE any callback ledger
// lock. It only projects live tickets and does not retire or activate them.
func (r *RegistryOf[T]) ExportTransportationCheckpoint(fed core.FederationName) TransportationCheckpointSnapshot {
	r.transports.mu.RLock()
	defer r.transports.mu.RUnlock()
	result := TransportationCheckpointSnapshot{}
	for ticket, entry := range r.transports.tickets {
		if entry.change.Scope.Member.Federation != fed {
			continue
		}
		result[ticket] = TransportationCheckpointSource{Scope: entry.change.Scope, Binding: TransportationCheckpointBinding{
			Object: entry.change.Object, Attributes: slices.Clone(entry.change.Attributes), ActualAttributes: slices.Clone(entry.actualAttributes),
			Interaction: entry.change.Interaction, Transportation: entry.change.Transportation, Activated: entry.activated,
		}}
	}
	return result
}

type TransportationCheckpointRestore struct {
	Scope   core.TransportationScope
	Binding TransportationCheckpointBinding
}

// Callers retain exclusive checkpoint admission through queue publication.
// Rollback is only legal before publication; it never rolls back transport
// effects, because this reservation does not apply any effects.
type TransportationCheckpointReservation struct {
	mu        sync.Mutex
	changes   []core.TransportationChange
	committed bool
	released  bool
	rollback  func()
	install   func()
}

func (p *TransportationCheckpointReservation) Changes() []core.TransportationChange {
	result := make([]core.TransportationChange, len(p.changes))
	for i, change := range p.changes {
		result[i] = cloneTransportationChange(change)
	}
	return result
}

func (p *TransportationCheckpointReservation) Commit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.released && !p.committed {
		if p.install != nil {
			p.install()
		}
		p.committed = true
	}
}

func (p *TransportationCheckpointReservation) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.committed && !p.released {
		p.released = true
		p.rollback()
	}
}

func (p *TransportationCheckpointReservation) Rollback() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.released {
		p.released = true
		p.rollback()
	}
}

// Reserve after domain import with authenticated current member identities and
// PLANNED new callback epochs supplied by the restore coordinator. Do not query
// the outbox here: restore may already retain its recipient locks.
func (r *RegistryOf[T]) ReserveTransportationCheckpoint(ctx context.Context, requests []TransportationCheckpointRestore) (*TransportationCheckpointReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reservation := &TransportationCheckpointReservation{rollback: func() {}}
	if len(requests) == 0 {
		return reservation, nil
	}
	fed := requests[0].Scope.Member.Federation
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	bindings := make([]TransportationCheckpointBinding, len(requests))
	for i, request := range requests {
		b, err := r.validateLiveTransportationCheckpoint(ctx, fed, st, request)
		if err != nil {
			return nil, err
		}
		bindings[i] = b
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	if len(requests) > 4096-len(r.transports.tickets) {
		return nil, core.ErrTransportationPendingLimit
	}
	entries := make(map[string]*transportationTicket, len(requests))
	remove := func() {
		for key, entry := range entries {
			if r.transports.tickets[key] == entry {
				delete(r.transports.tickets, key)
			}
		}
		r.transports.completed = slices.DeleteFunc(r.transports.completed, func(key string) bool { return r.transports.tickets[key] == nil })
	}
	for i, request := range requests {
		b := bindings[i]
		for _, pending := range r.transports.tickets {
			if pending.change.Scope.Member.Federation != fed {
				continue
			}
			prior := TransportationCheckpointRestore{Scope: pending.change.Scope, Binding: TransportationCheckpointBinding{
				Object: pending.change.Object, Interaction: pending.change.Interaction,
				ActualAttributes: pending.actualAttributes, Activated: pending.activated,
			}}
			if err := transportationCheckpointConflict(TransportationCheckpointRestore{Scope: request.Scope, Binding: b}, prior); err != nil {
				remove()
				return nil, err
			}
		}
		change, err := r.transports.prepareLocked(core.TransportationChange{Scope: request.Scope, Object: b.Object, Attributes: b.Attributes, Interaction: b.Interaction, Transportation: b.Transportation})
		if err != nil {
			remove()
			return nil, err
		}
		entry := r.transports.tickets[string(change.Ticket)]
		entry.actualAttributes, entry.activated = b.ActualAttributes, b.Activated
		entry.checkpointAcknowledgement = b.Activated
		entries[string(change.Ticket)] = entry
		reservation.changes = append(reservation.changes, change)
	}
	reservation.rollback = func() {
		r.transports.mu.Lock()
		defer r.transports.mu.Unlock()
		remove()
	}
	return reservation, nil
}

// The first actual activation of an already-applied restored intent only
// acknowledges it and moves its fresh ticket into the existing bounded cache.
func (r *RegistryOf[T]) consumeCheckpointTransportationAcknowledgement(ticket []byte, entry *transportationTicket) error {
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	if r.transports.tickets[string(ticket)] != entry {
		return core.ErrTransportationTicketInvalid
	}
	if entry.checkpointAcknowledgement {
		entry.checkpointAcknowledgement = false
		r.transports.completed = append(r.transports.completed, string(ticket))
		if len(r.transports.completed) > 256 {
			delete(r.transports.tickets, r.transports.completed[0])
			r.transports.completed = r.transports.completed[1:]
		}
	}
	return nil
}

func validTransportationBindingShape(b TransportationCheckpointBinding) bool {
	return !((b.Object == 0) == (b.Interaction == 0) || b.Object == ^core.ObjectHandle(0) || b.Interaction == ^core.InteractionClassHandle(0) || !validTransportation(b.Transportation) || len(b.Attributes) != len(b.ActualAttributes) || (b.Interaction != 0 && len(b.Attributes) != 0))
}

func (r *RegistryOf[T]) validateLiveTransportationCheckpoint(ctx context.Context, fed core.FederationName, st *federationStateOf[T], request TransportationCheckpointRestore) (TransportationCheckpointBinding, error) {
	id, b := request.Scope.Member, cloneTransportationCheckpoint(request.Binding)
	if id.Federation != fed || fed == "" || id.Handle == 0 || id.Handle == ^core.FederateHandle(0) || id.Incarnation == 0 {
		return TransportationCheckpointBinding{}, core.ErrTransportationTicketInvalid
	}
	if err := ValidateTransportationCheckpointBinding(b); err != nil {
		return TransportationCheckpointBinding{}, err
	}
	if b.Object != 0 {
		actual, err := r.validateAttributeTransportation(ctx, st, request.Scope, b.Object, b.Attributes, b.Transportation)
		if err != nil {
			return TransportationCheckpointBinding{}, err
		}
		if !slices.Equal(actual, b.ActualAttributes) {
			return TransportationCheckpointBinding{}, fmt.Errorf("transportation checkpoint: registered attribute mapping changed")
		}
	} else {
		fom, err := r.transportationFOM(ctx, fed)
		if err != nil {
			return TransportationCheckpointBinding{}, err
		}
		if err := transportationInteractionDefined(fom, b.Interaction); err != nil {
			return TransportationCheckpointBinding{}, err
		}
	}
	return b, nil
}

func transportationCheckpointConflict(request, prior TransportationCheckpointRestore) error {
	b, previous := request.Binding, prior.Binding
	if b.Activated || previous.Activated {
		return nil
	}
	if b.Object != 0 && b.Object == previous.Object {
		for _, attr := range b.ActualAttributes {
			if slices.Contains(previous.ActualAttributes, attr) {
				return core.ErrAttributeAlreadyBeingChanged
			}
		}
	} else if b.Interaction != 0 && request.Scope == prior.Scope && b.Interaction == previous.Interaction {
		return core.ErrInteractionClassAlreadyBeingChanged
	}
	return nil
}
