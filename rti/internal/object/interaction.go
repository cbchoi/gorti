package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// sendInteraction is the body of Registry.SendInteraction. Implemented in
// TASK-033; placeholder during TASK-030/031 keeps the package compiling
// while the spec tests for the interaction path remain red.
func (r *Registry) sendInteraction(
	_ context.Context,
	_ core.FederationName,
	_ core.FederateHandle,
	_ core.InteractionClassHandle,
	_ map[core.ParameterHandle][]byte,
	_ *core.LogicalTime,
) error {
	return ErrNotImplemented
}
