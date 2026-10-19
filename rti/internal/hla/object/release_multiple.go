package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.MultipleObjectNameReleaser = (*Registry)(nil)

func (r *RegistryOf[T]) ReleaseMultipleObjectInstanceNames(ctx context.Context, fed core.FederationName, holder core.FederateHandle, names []string) error {
	if holder == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s := r.reservations
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	reservations := s.reservations[fed]
	for _, name := range names {
		owner, reserved := reservations[name]
		if !reserved {
			return core.ErrObjectInstanceNameNotReserved
		}
		if owner != holder {
			return core.ErrObjectInstanceNameReservedByOther
		}
	}
	// The wire represents a set as repeated strings. Duplicate entries release
	// the same reservation once, without introducing a second validation phase.
	for _, name := range names {
		delete(reservations, name)
	}
	if len(reservations) == 0 {
		delete(s.reservations, fed)
	}
	return nil
}
