package object

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type ownershipQuerySnapshot interface {
	QueryOwnership(core.FederationName, core.ObjectHandle, core.AttributeHandle) (core.FederateHandle, bool)
}

// QueryAttributeOwnership validates the caller's object knowledge and the
// attribute definition before reading its actual owner. The object lock remains
// held through the ownership snapshot, preserving object -> ownership lock order.
func (r *RegistryOf[T]) QueryAttributeOwnership(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle) (core.FederateHandle, bool, error) {
	r.mu.RLock()
	st := r.federations[fed]
	r.mu.RUnlock()
	if st == nil {
		return 0, false, core.ErrObjectHandleInvalid
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, err := r.transportationObject(st, fed, caller, obj, []core.AttributeHandle{attr})
	if err != nil {
		return 0, false, err
	}
	attrs, err := r.knownAttributes(ctx, fed, st, caller, inst, []core.AttributeHandle{attr})
	if err != nil {
		return 0, false, err
	}
	ownership, ok := r.opts.Ownership.(ownershipQuerySnapshot)
	if !ok {
		return 0, false, fmt.Errorf("ownership: current-owner lookup unavailable")
	}
	owner, owned := ownership.QueryOwnership(fed, obj, attrs[0])
	if !owned {
		return 0, false, nil
	}
	return owner, true, nil
}
