package object

import (
	"context"
	"fmt"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type momOwnershipAdjustment interface {
	ModifyAttributeState(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, core.AttributeHandle, bool) error
}

// ModifyAttributeState pins the known object until the ownership change is
// committed. It shares the object -> ownership lock order of acquisition.
func (r *RegistryOf[T]) ModifyAttributeState(ctx context.Context, fed core.FederationName, target core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle, owned bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	owner, ok := r.opts.Ownership.(momOwnershipAdjustment)
	if !ok {
		return fmt.Errorf("MOM ownership: atomic adjustment unavailable")
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, err := r.transportationObject(st, fed, target, obj, []core.AttributeHandle{attr})
	if err != nil {
		return err
	}
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return err
	}
	if err := transportationAttributeDefined(fom, inst.cls, attr); err != nil {
		return err
	}
	names, ok := fom.(core.FOMHandleNameLookup)
	if !ok {
		return fmt.Errorf("MOM ownership: class names unavailable")
	}
	name, ok := names.ObjectClassName(inst.cls)
	if !ok {
		return fmt.Errorf("MOM ownership: class name unavailable")
	}
	name = strings.TrimPrefix(name, "HLAobjectRoot.")
	if name == "HLAmanager" || strings.HasPrefix(name, "HLAmanager.") {
		return fmt.Errorf("MOM ownership: predefined management attribute is RTI-owned")
	}
	if owned {
		pubs, ok := r.opts.Declarations.(core.ObjectPublicationValidator)
		if !ok {
			return fmt.Errorf("MOM ownership: publication validation unavailable")
		}
		if err := pubs.ValidateObjectPublication(ctx, fed, target, inst.cls, []core.AttributeHandle{attr}); err != nil {
			return err
		}
	}
	return owner.ModifyAttributeState(ctx, fed, target, obj, attr, owned)
}
