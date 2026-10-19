package object

import (
	"context"
	"fmt"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PrepareOwnershipAcquisition validates the complete request and pins object
// identity through the ownership mutation. Its handles are in registered-class
// coordinates, as used by the ownership manager. Lock order is object then ownership,
// as in deletion and resignation. Callers must release before delivering events.
func (r *RegistryOf[T]) PrepareOwnershipAcquisition(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
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
	known, err := r.actualAttributesAtKnownClass(ctx, fed, st, caller, inst, attrs)
	if err != nil {
		return nil, err
	}
	publications, ok := r.opts.Declarations.(core.ObjectPublicationValidator)
	if !ok {
		return nil, fmt.Errorf("ownership: coherent publication validation unavailable")
	}
	if err := publications.ValidateObjectPublication(ctx, fed, caller, st.knownClass(caller, inst), known); err != nil {
		return nil, err
	}
	valid = true
	return r.transportationOwnershipRelease(fed, obj, attrs, release), nil
}

// PrepareKnownOwnershipAcquisition is the public-coordinate counterpart of the
// internal hook. The ownership manager's prepared path must mutate the returned
// handles while retaining the lease, then release after unlocking ownership and
// before callbacks. Do not call a self-preparing manager method with this lease.
// Caller-owned slices are never rewritten.
func (r *RegistryOf[T]) PrepareKnownOwnershipAcquisition(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
	return r.prepareKnownOwnership(ctx, fed, caller, obj, attrs, true)
}

// PrepareKnownOwnershipOperation maps non-acquisition ownership requests without
// requiring publication. It pins the same object -> ownership mutation boundary.
func (r *RegistryOf[T]) PrepareKnownOwnershipOperation(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
	return r.prepareKnownOwnership(ctx, fed, caller, obj, attrs, false)
}

func (r *RegistryOf[T]) prepareKnownOwnership(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, acquisition bool) ([]core.AttributeHandle, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	release := sync.OnceFunc(st.mu.Unlock)
	valid := false
	defer func() {
		if !valid {
			release()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	inst, err := r.transportationObject(st, fed, caller, obj, attrs)
	if err != nil {
		return nil, nil, err
	}
	actual, err := r.knownAttributes(ctx, fed, st, caller, inst, attrs)
	if err != nil {
		return nil, nil, err
	}
	if acquisition {
		publications, ok := r.opts.Declarations.(core.ObjectPublicationValidator)
		if !ok {
			return nil, nil, fmt.Errorf("ownership: coherent publication validation unavailable")
		}
		if err := publications.ValidateObjectPublication(ctx, fed, caller, st.knownClass(caller, inst), attrs); err != nil {
			return nil, nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	valid = true
	return actual, r.transportationOwnershipRelease(fed, obj, actual, release), nil
}
