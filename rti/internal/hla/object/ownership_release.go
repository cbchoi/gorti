package object

import (
	"context"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PrepareOwnershipReleaseDenied pins known object identity and validates the
// full attribute set. Ownership mutations use object -> ownership lock order.
// The caller must release before delivering callbacks.
func (r *RegistryOf[T]) PrepareOwnershipReleaseDenied(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
	st := r.stateFor(fed)
	st.mu.Lock()
	release := sync.OnceFunc(st.mu.Unlock)
	valid := false
	defer func() {
		if !valid {
			release()
		}
	}()
	inst, err := r.transportationObject(st, fed, caller, obj, attrs)
	if err != nil {
		return nil, err
	}
	if _, err := r.actualAttributesAtKnownClass(ctx, fed, st, caller, inst, attrs); err != nil {
		return nil, err
	}
	valid = true
	return r.transportationOwnershipRelease(fed, obj, attrs, release), nil
}
