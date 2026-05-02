package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// updateAttributes is the body of Registry.UpdateAttributes. Implemented
// in TASK-032; for TASK-030/031 it returns ErrNotImplemented so the spec
// tests that don't exercise it still compile cleanly while the spec tests
// that DO exercise it remain red on the right error.
func (r *Registry) updateAttributes(
	_ context.Context,
	_ core.FederationName,
	_ core.FederateHandle,
	_ core.ObjectHandle,
	_ map[core.AttributeHandle][]byte,
	_ *core.LogicalTime,
) error {
	return ErrNotImplemented
}
