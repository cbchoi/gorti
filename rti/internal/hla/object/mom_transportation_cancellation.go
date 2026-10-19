package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// CancelTransportationChange rolls back an unpublished MOM confirmation. It
// never touches achieved transport or completed activation retry evidence.
func (r *RegistryOf[T]) CancelTransportationChange(ctx context.Context, scope core.TransportationScope, ticket []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(ticket) != 32 {
		return core.ErrTransportationTicketInvalid
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	entry := r.transports.tickets[string(ticket)]
	if entry == nil || entry.change.Scope != scope || entry.activated {
		return core.ErrTransportationTicketInvalid
	}
	delete(r.transports.tickets, string(ticket))
	return nil
}
