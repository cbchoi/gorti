package object

import (
	"context"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type objectClassUnpublication struct {
	attributes map[core.ObjectHandle]map[core.AttributeHandle]core.AttributeHandle
	release    func()
	project    func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error)
	mu         sync.RWMutex
	released   bool
}

func (p *objectClassUnpublication) SelectAttributes(attrs []core.AttributeHandle) map[core.ObjectHandle][]core.AttributeHandle {
	result := make(map[core.ObjectHandle][]core.AttributeHandle)
	for obj, mapping := range p.attributes {
		for _, attr := range attrs {
			if actual, exists := mapping[attr]; exists {
				result[obj] = append(result[obj], actual)
			}
		}
		if len(result[obj]) > 0 {
			slices.Sort(result[obj])
			result[obj] = slices.Compact(result[obj])
		}
	}
	return result
}

func (p *objectClassUnpublication) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.released {
		p.released = true
		p.release()
	}
}

func (p *objectClassUnpublication) ProjectKnownObjectAttributes(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.released {
		return 0, 0, nil, core.ErrObjectHandleInvalid
	}
	return p.project(ctx, fed, recipient, obj, attrs)
}

func (r *RegistryOf[T]) PrepareObjectClassUnpublication(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) (core.PreparedObjectClassUnpublication, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	if err := ctx.Err(); err != nil {
		st.mu.Unlock()
		return nil, err
	}
	p := &objectClassUnpublication{attributes: make(map[core.ObjectHandle]map[core.AttributeHandle]core.AttributeHandle)}
	var once sync.Once
	p.release = func() { once.Do(st.mu.Unlock) }
	p.project = func(ctx context.Context, requested core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error) {
		if err := ctx.Err(); err != nil {
			return 0, 0, nil, err
		}
		inst := st.instances[obj]
		if requested != fed || inst == nil {
			return 0, 0, nil, core.ErrObjectHandleInvalid
		}
		known := st.discovered[discoverKey{sub: recipient, obj: obj}]
		if known == 0 {
			return 0, 0, nil, core.ErrObjectHandleInvalid
		}
		mapped, err := r.actualAttributesAtKnownClass(ctx, fed, st, recipient, inst, attrs)
		if err != nil {
			return 0, 0, nil, err
		}
		return inst.cls, known, mapped, nil
	}
	for obj, inst := range st.instances {
		if st.knownClass(caller, inst) != cls {
			continue
		}
		mapping := make(map[core.AttributeHandle]core.AttributeHandle, len(attrs))
		for _, attr := range attrs {
			actual, ok := r.attributeAtClass(ctx, fed, cls, inst.cls, attr)
			if !ok {
				p.Release()
				return nil, core.ErrAttributeNotFound
			}
			mapping[attr] = actual
		}
		p.attributes[obj] = mapping
	}
	return p, nil
}
