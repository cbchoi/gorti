package object

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type localDeletionOwnership interface {
	PrepareLocalDelete(core.FederationName, core.FederateHandle, core.ObjectHandle) (func(), error)
}

// LocalDelete forgets one federate's knowledge, never the shared object.
// Continuing subscription interests may immediately rediscover the instance.
func (r *RegistryOf[T]) LocalDelete(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle) error {
	r.mu.RLock()
	st := r.federations[fed]
	r.mu.RUnlock()
	if st == nil {
		return core.ErrObjectHandleInvalid
	}
	st.mu.Lock()
	inst := st.instances[obj]
	st.mu.Unlock()
	if inst == nil {
		return core.ErrObjectHandleInvalid
	}
	inst.deliveryMu.Lock()
	defer inst.deliveryMu.Unlock()
	st.mu.Lock()
	_, known := st.discovered[discoverKey{sub: caller, obj: obj}]
	if st.instances[obj] != inst || !known {
		st.mu.Unlock()
		return core.ErrObjectHandleInvalid
	}
	var release func()
	if guard, ok := r.opts.Ownership.(localDeletionOwnership); ok {
		var err error
		release, err = guard.PrepareLocalDelete(fed, caller, obj)
		if err != nil {
			st.mu.Unlock()
			return err
		}
	} else if r.opts.Ownership != nil {
		st.mu.Unlock()
		return fmt.Errorf("object: local deletion ownership validation unavailable")
	} else if inst.owner == caller {
		st.mu.Unlock()
		return core.ErrFederateOwnsAttributes
	}
	r.forgetObjectRegionAssociations(fed, caller, obj)
	st.forgetKnowledgeLocked(discoverKey{sub: caller, obj: obj})
	if scope := st.scope[obj]; scope != nil {
		delete(scope, caller)
		if len(scope) == 0 {
			delete(st.scope, obj)
		}
	}
	if release != nil {
		release()
	}
	st.mu.Unlock()
	r.rediscoverIfRelevantLocked(ctx, fed, st, caller, inst)
	return nil
}

func (r *RegistryOf[T]) LookupKnownObjectInstance(fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle) (string, core.ObjectClassHandle, bool) {
	r.mu.RLock()
	st := r.federations[fed]
	r.mu.RUnlock()
	if st == nil {
		return "", 0, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	inst := st.instances[obj]
	cls, known := st.discovered[discoverKey{sub: caller, obj: obj}]
	if !known {
		return "", 0, false
	}
	if inst == nil {
		d, ok := st.deletions[obj]
		return d.ObjectName, cls, ok && d.Committed && d.ObjectName != "" && cls != 0
	}
	return inst.name, cls, true
}

func (r *RegistryOf[T]) LookupKnownObjectInstanceByName(fed core.FederationName, caller core.FederateHandle, name string) (core.ObjectHandle, bool) {
	r.mu.RLock()
	st := r.federations[fed]
	r.mu.RUnlock()
	if st == nil {
		return 0, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	obj, exists := st.nameToHandle[name]
	_, known := st.discovered[discoverKey{sub: caller, obj: obj}]
	if exists && known {
		return obj, true
	}
	for handle, deletion := range st.deletions {
		if deletion.Committed && deletion.ObjectName == name {
			if _, known := st.discovered[discoverKey{sub: caller, obj: handle}]; known {
				return handle, true
			}
		}
	}
	return 0, false
}
