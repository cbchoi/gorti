package object

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (r *RegistryOf[T]) subscriptionModel(ctx context.Context, fed core.FederationName, cached ...core.FOMHandle) (core.FOMHandle, error) {
	var model core.FOMHandle
	if references, ok := r.opts.FOMs.(core.FOMHandleReferenceRepository); ok {
		st := r.stateFor(fed)
		st.managementClassMu.RLock()
		reference := st.subscriptionReference
		st.managementClassMu.RUnlock()
		if reference == nil {
			st.managementClassMu.Lock()
			if st.subscriptionReference == nil {
				st.subscriptionReference = references.FOMHandleReference(fed)
			}
			reference = st.subscriptionReference
			st.managementClassMu.Unlock()
		}
		if reference != nil {
			model = reference.CurrentFOMHandle()
			if model == nil {
				return nil, core.ErrFederationNotFound
			}
		}
	} else if len(cached) != 0 {
		model = cached[0]
	}
	if model == nil {
		var err error
		model, err = r.opts.FOMs.Get(ctx, fed)
		if err != nil {
			return nil, err
		}
	}
	return model, nil
}

func (r *RegistryOf[T]) delayedSubscriptionCandidates(ctx context.Context, fed core.FederationName, original []core.FederateHandle, cached ...core.FOMHandle) ([]core.FederateHandle, bool, error) {
	model, err := r.subscriptionModel(ctx, fed, cached...)
	if err != nil {
		return nil, false, err
	}
	policy, ok := model.(core.FOMSubscriptionEvaluation)
	if !ok {
		return original, false, nil
	}
	enabled, _ := policy.DelaySubscriptionEvaluation()
	if !enabled {
		return original, false, nil
	}
	roster, ok := r.opts.Federations.(core.SubscriptionEvaluationRoster)
	if !ok {
		return nil, false, fmt.Errorf("delayed subscription evaluation requires a federation roster")
	}
	members := roster.ListMembers(fed)
	candidates := make([]core.FederateHandle, 0, len(members))
	for _, member := range members {
		candidates = append(candidates, member.Handle)
	}
	slices.Sort(candidates)
	return slices.Compact(candidates), true, nil
}

// projectDelayedReflection never edits the queued canonical envelope. Retrying
// admission must re-evaluate interest against the complete original payload.
func (r *RegistryOf[T]) projectDelayedReflection(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, callback *CallbackEventOf[T], admitting bool) *CallbackEventOf[T] {
	if callback.Metadata == nil || !callback.Metadata.DelayedSubscription || callback.Kind != CallbackReflect {
		return callback
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	known := st.discovered[discoverKey{sub: recipient, obj: callback.Object}]
	st.mu.Unlock()
	// Newly prepared discoveries precede immediate reflections in the same
	// atomic fanout; their state is not committed until admission succeeds.
	if known == 0 && admitting {
		known = callback.ObjectClass
	}
	if known == 0 {
		return nil
	}
	projected := *callback
	projected.ObjectClass = known
	projected.Attributes = make(map[uint64][]byte, len(callback.Attributes))
	meta := *callback.Metadata
	meta.DelayedSubscription = false
	meta.RegisteredClass = 0
	if callback.Metadata.AttributeConveyedRegions != nil {
		meta.AttributeConveyedRegions = make(map[core.AttributeHandle][]core.ConveyedRegionCopy)
	}
	for raw, value := range callback.Attributes {
		actual := core.AttributeHandle(raw)
		mapped, ok := r.attributeAtClass(ctx, fed, callback.Metadata.RegisteredClass, known, actual)
		if !ok {
			continue
		}
		projected.Attributes[uint64(mapped)] = value
		if copies, found := callback.Metadata.AttributeConveyedRegions[actual]; found {
			meta.AttributeConveyedRegions[mapped] = slices.Clone(copies)
		}
	}
	projected.Metadata = &meta
	return &projected
}

func (r *RegistryOf[T]) projectDelayedInteraction(ctx context.Context, fed core.FederationName, callback *CallbackEventOf[T], cls core.InteractionClassHandle) core.OutboundEvent {
	if callback.Metadata == nil || !callback.Metadata.DelayedSubscription {
		return callback
	}
	projected := *callback
	meta := *callback.Metadata
	meta.DelayedSubscription = false
	projected.Metadata = &meta
	if cls == callback.InteractionClass {
		return &projected
	}
	model, err := r.opts.FOMs.Get(ctx, fed)
	names, ok := model.(core.FOMHandleNameLookup)
	if err != nil || !ok {
		return nil
	}
	projected.InteractionClass = cls
	projected.Parameters = make(map[uint64][]byte)
	for raw, value := range callback.Parameters {
		name, found := names.ParameterName(callback.InteractionClass, core.ParameterHandle(raw))
		if !found {
			continue
		}
		if mapped, found := model.LookupParameter(cls, name); found {
			projected.Parameters[uint64(mapped)] = value
		}
	}
	return &projected
}

func (r *RegistryOf[T]) projectImmediateDelayed(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) core.OutboundEvent {
	callback, ok := event.(*CallbackEventOf[T])
	if !ok || callback.Metadata == nil || !callback.Metadata.DelayedSubscription {
		return event
	}
	callback = r.projectDelayedReflection(ctx, fed, recipient, callback, true)
	if callback == nil {
		return nil
	}
	return r.FilterQueuedMessage(ctx, fed, recipient, callback)
}
