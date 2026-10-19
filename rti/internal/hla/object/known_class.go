package object

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (st *federationStateOf[T]) registeredClass(obj core.ObjectHandle) core.ObjectClassHandle {
	if inst := st.instances[obj]; inst != nil {
		return inst.cls
	}
	return st.deletions[obj].ObjectClass
}

// The caller holds st.mu. Never reconstruct historical knowledge from current
// subscriptions. The fallback serves legacy ownership-only embedded callers.
func (st *federationStateOf[T]) knownClass(caller core.FederateHandle, inst *objectInstance) core.ObjectClassHandle {
	if cls := st.discovered[discoverKey{sub: caller, obj: inst.handle}]; cls != 0 {
		return cls
	}
	return inst.cls
}

func (r *RegistryOf[T]) knownAttributes(ctx context.Context, fed core.FederationName, st *federationStateOf[T], caller core.FederateHandle, inst *objectInstance, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return nil, err
	}
	cls := st.knownClass(caller, inst)
	mapped := make([]core.AttributeHandle, len(attrs))
	for i, attr := range attrs {
		if err := transportationAttributeDefined(fom, cls, attr); err != nil {
			return nil, err
		}
		actual, ok := r.attributeAtClass(ctx, fed, cls, inst.cls, attr)
		if !ok {
			return nil, core.ErrAttributeNotFound
		}
		mapped[i] = actual
	}
	return mapped, nil
}

// Internal ownership hooks receive registered-class handles. Reject attributes
// unavailable at the remembered class before checking its publication.
func (r *RegistryOf[T]) actualAttributesAtKnownClass(ctx context.Context, fed core.FederationName, st *federationStateOf[T], caller core.FederateHandle, inst *objectInstance, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return nil, err
	}
	cls := st.knownClass(caller, inst)
	mapped := make([]core.AttributeHandle, len(attrs))
	for i, attr := range attrs {
		if err := transportationAttributeDefined(fom, inst.cls, attr); err != nil {
			return nil, err
		}
		known, ok := r.attributeAtClass(ctx, fed, inst.cls, cls, attr)
		if !ok {
			return nil, core.ErrAttributeNotFound
		}
		mapped[i] = known
	}
	return mapped, nil
}

// ProjectKnownObjectAttributes projects internal ownership results/callbacks.
// Invoke only after releasing any object/ownership lease. Unknown objects and
// attributes outside the recipient's remembered class are rejected, not guessed.
func (r *RegistryOf[T]) ProjectKnownObjectAttributes(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, error) {
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, err := r.transportationObject(st, fed, recipient, obj, attrs)
	if err != nil {
		return 0, nil, err
	}
	mapped, err := r.actualAttributesAtKnownClass(ctx, fed, st, recipient, inst, attrs)
	if err != nil {
		return 0, nil, err
	}
	return st.knownClass(recipient, inst), mapped, nil
}

// MapKnownAttributesToRegistered is a read-only adapter boundary. Mutations
// needing a continuous object pin should use PrepareKnownOwnershipOperation.
func (r *RegistryOf[T]) MapKnownAttributesToRegistered(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, err := r.transportationObject(st, fed, caller, obj, attrs)
	if err != nil {
		return nil, err
	}
	return r.knownAttributes(ctx, fed, st, caller, inst, attrs)
}

func (r *RegistryOf[T]) MapRegisteredAttributesToKnown(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	_, mapped, err := r.ProjectKnownObjectAttributes(ctx, fed, recipient, obj, attrs)
	return mapped, err
}

func (r *RegistryOf[T]) classAncestors(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle) []core.ObjectClassHandle {
	if fom, err := r.opts.FOMs.Get(ctx, fed); err == nil {
		if hierarchy, ok := fom.(core.FOMClassAncestry); ok {
			result := []core.ObjectClassHandle{cls}
			for _, ancestor := range hierarchy.ObjectClassAncestors(cls) {
				if !slices.Contains(result, ancestor) {
					result = append(result, ancestor)
				}
			}
			return result
		}
	}
	return []core.ObjectClassHandle{cls}
}

func (r *RegistryOf[T]) attributeAtClass(ctx context.Context, fed core.FederationName, from, to core.ObjectClassHandle, attr core.AttributeHandle) (core.AttributeHandle, bool) {
	if from == to {
		return attr, true
	}
	fom, err := r.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return 0, false
	}
	names, ok := fom.(interface {
		AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool)
	})
	if !ok {
		return 0, false
	}
	name, ok := names.AttributeName(from, attr)
	if !ok {
		return 0, false
	}
	return fom.LookupAttribute(to, name)
}

// Candidate selection uses subscription presence, not region overlap. Only the
// selected class may subsequently make an unknown object discoverable (6.1.1).
func (r *RegistryOf[T]) candidateDiscoveryClasses(ctx context.Context, fed core.FederationName, inst *objectInstance, actualAttrs ...core.AttributeHandle) map[core.FederateHandle]core.ObjectClassHandle {
	classes := make(map[core.FederateHandle]core.ObjectClassHandle)
	fom, _ := r.opts.FOMs.Get(ctx, fed)
	_, enumerated := fom.(interface {
		AttributeHandles(core.ObjectClassHandle) []core.AttributeHandle
	})
	for _, cls := range r.classAncestors(ctx, fed, inst.cls) {
		attrs := r.classAttributes(ctx, fed, cls)
		if !enumerated && cls == inst.cls {
			attrs = append(slices.Clone(attrs), actualAttrs...)
			slices.Sort(attrs)
			attrs = slices.Compact(attrs)
		}
		for _, sub := range r.objectSubscribers(ctx, fed, cls, attrs) {
			if classes[sub] == 0 {
				classes[sub] = cls
			}
		}
		if r.opts.DDM != nil {
			for _, attr := range attrs {
				subs := r.opts.DDM.RegionSubscribersFor(fed, cls, attr)
				// Legacy embedded filters may expose matched recipients only,
				// without FOM enumeration or subscription-presence metadata.
				if !enumerated {
					regions := r.opts.DDM.PublisherRegionsFor(fed, inst.handle, attr)
					if len(regions) != 0 {
						subs = append(slices.Clone(subs), r.opts.DDM.SubscribersForUpdate(fed, cls, attr, regions)...)
					}
				}
				for _, sub := range subs {
					if classes[sub] == 0 {
						classes[sub] = cls
					}
				}
			}
		}
	}
	return classes
}

func (r *RegistryOf[T]) recipientClasses(ctx context.Context, fed core.FederationName, inst *objectInstance, attrs ...core.AttributeHandle) map[core.FederateHandle]core.ObjectClassHandle {
	classes := r.candidateDiscoveryClasses(ctx, fed, inst, attrs...)
	st := r.stateFor(fed)
	st.mu.Lock()
	for key, cls := range st.discovered {
		if key.obj == inst.handle {
			classes[key.sub] = cls
		}
	}
	st.mu.Unlock()
	return classes
}

// Attribute indices are class-relative in the current FOM repository. Routing
// stays in registered-class coordinates; callbacks project into known class.
func (r *RegistryOf[T]) recipientInterested(ctx context.Context, fed core.FederationName, inst *objectInstance, sub core.FederateHandle, cls core.ObjectClassHandle, actual core.AttributeHandle, discovery bool) bool {
	attr, ok := r.attributeAtClass(ctx, fed, inst.cls, cls, actual)
	if !ok {
		return false
	}
	if discovery {
		if owners, ok := r.opts.Ownership.(ownershipQuerySnapshot); ok {
			owner, owned := owners.QueryOwnership(fed, inst.handle, actual)
			if !owned || owner == sub {
				return false
			}
		} else if inst.owner == sub {
			return false
		}
	}
	var regions []DDMRegionHandle
	if r.opts.DDM != nil {
		regions = r.opts.DDM.PublisherRegionsFor(fed, inst.handle, actual)
	}
	if slices.Contains(r.objectSubscribers(ctx, fed, cls, []core.AttributeHandle{attr}), sub) && r.publisherRegionsOverlapDefault(fed, inst.cls, actual, regions) {
		return true
	}
	if r.opts.DDM == nil {
		return false
	}
	if len(regions) == 0 {
		return slices.Contains(r.opts.DDM.RegionSubscribersFor(fed, cls, attr), sub)
	}
	return slices.Contains(r.opts.DDM.SubscribersForUpdate(fed, cls, attr, regions), sub)
}

func (r *RegistryOf[T]) projectAttributes(ctx context.Context, fed core.FederationName, actual, known core.ObjectClassHandle, attrs []core.AttributeHandle) []core.AttributeHandle {
	var result []core.AttributeHandle
	for _, attr := range attrs {
		if mapped, ok := r.attributeAtClass(ctx, fed, actual, known, attr); ok {
			result = append(result, mapped)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
