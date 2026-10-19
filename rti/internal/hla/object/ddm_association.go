package object

import (
	"context"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PrepareKnownRegionAssociation retains object knowledge and ownership while
// DDM validates regions and commits canonical associations. Ownership is a
// routing state, not a precondition for creating an association (9.1.3.2).
func (r *RegistryOf[T]) PrepareKnownRegionAssociation(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, map[core.AttributeHandle]core.FederateHandle, func(), error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, nil, nil, err
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
		return 0, nil, nil, nil, err
	}
	inst, err := r.transportationObject(st, fed, caller, obj, attrs)
	if err != nil {
		return 0, nil, nil, nil, err
	}
	actual, err := r.knownAttributes(ctx, fed, st, caller, inst, attrs)
	if err != nil {
		return 0, nil, nil, nil, err
	}
	owners := make(map[core.AttributeHandle]core.FederateHandle, len(actual))
	if snapshot, ok := r.opts.Ownership.(attributeOwnerLease); ok {
		var unlock func()
		owners, unlock = snapshot.PrepareAttributeOwners(fed, obj, actual)
		objectUnlock := release
		release = sync.OnceFunc(func() { unlock(); objectUnlock() })
	} else if r.opts.Ownership != nil {
		// A read without an ownership pin cannot safely decide activation.
		return 0, nil, nil, nil, core.ErrInvalidRegionContext
	} else {
		for _, attr := range actual {
			if slices.Contains(r.opts.Declarations.PublishersFor(ctx, fed, inst.cls, attr), inst.owner) {
				owners[attr] = inst.owner
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, nil, nil, err
	}
	valid = true
	return inst.cls, actual, owners, release, nil
}

type regionOwnershipLifecycle interface {
	OwnershipChanged(core.FederationName, core.ObjectHandle, map[core.AttributeHandle]core.FederateHandle, map[core.AttributeHandle]core.FederateHandle)
}

// Called at the same object-pinned boundary as transportation cleanup. The
// returned release runs after ownership unlock and before object unlock.
func (r *RegistryOf[T]) ddmOwnershipRelease(fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle, release func()) func() {
	regions, ok := r.opts.RegistrationRegions.(regionOwnershipLifecycle)
	if !ok {
		return release
	}
	ownership, ok := r.opts.Ownership.(ownershipQuerySnapshot)
	if !ok {
		return release
	}
	before := make(map[core.AttributeHandle]core.FederateHandle, len(attrs))
	for _, attr := range attrs {
		before[attr], _ = ownership.QueryOwnership(fed, obj, attr)
	}
	return sync.OnceFunc(func() {
		defer release()
		after := make(map[core.AttributeHandle]core.FederateHandle, len(before))
		for attr := range before {
			after[attr], _ = ownership.QueryOwnership(fed, obj, attr)
		}
		regions.OwnershipChanged(fed, obj, before, after)
	})
}

// Prepare under the object pin before the ownership resignation plan. Invoke
// only on successful commit, after ownership unlock but before object unlock.
func (r *RegistryOf[T]) ddmResignCommit(fed core.FederationName, caller core.FederateHandle) func() {
	regions, ok := r.opts.RegistrationRegions.(interface {
		regionOwnershipLifecycle
		AssociationAttributes(core.FederationName) map[core.ObjectHandle][]core.AttributeHandle
		ForgetFederateAssociations(core.FederationName, core.FederateHandle)
	})
	if !ok {
		return func() {}
	}
	before := map[core.ObjectHandle]map[core.AttributeHandle]core.FederateHandle{}
	ownership, tracked := r.opts.Ownership.(ownershipQuerySnapshot)
	if tracked {
		for obj, attrs := range regions.AssociationAttributes(fed) {
			before[obj] = map[core.AttributeHandle]core.FederateHandle{}
			for _, attr := range attrs {
				before[obj][attr], _ = ownership.QueryOwnership(fed, obj, attr)
			}
		}
	}
	return sync.OnceFunc(func() {
		for obj, previous := range before {
			after := map[core.AttributeHandle]core.FederateHandle{}
			for attr := range previous {
				after[attr], _ = ownership.QueryOwnership(fed, obj, attr)
			}
			regions.OwnershipChanged(fed, obj, previous, after)
		}
		regions.ForgetFederateAssociations(fed, caller)
	})
}
