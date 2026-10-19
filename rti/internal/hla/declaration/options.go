package declaration

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// SetFOMRepository configures immutable subscription metadata before serving.
func (m *Manager) SetFOMRepository(foms core.FOMRepository) { m.foms = foms }

func (m *Manager) model(ctx context.Context, fed core.FederationName) (core.FOMHandle, error) {
	if m.foms == nil {
		return nil, nil
	}
	return m.foms.Get(ctx, fed)
}

func (m *Manager) ValidateSubscriptionOptions(ctx context.Context, fed core.FederationName, options core.SubscriptionOptions) error {
	if options.UpdateRateDesignator == "" {
		return nil
	}
	model, err := m.model(ctx, fed)
	if err != nil {
		return err
	}
	return validateSubscriptionOptions(model, options)
}

func validateSubscriptionOptions(model core.FOMHandle, options core.SubscriptionOptions) error {
	if options.UpdateRateDesignator == "" {
		return nil
	}
	rates, ok := model.(core.FOMUpdateRates)
	if ok {
		if _, exists := rates.UpdateRateValue(options.UpdateRateDesignator); exists {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", core.ErrInvalidUpdateRateDesignator, options.UpdateRateDesignator)
}

func (m *Manager) SubscribeObjectClassAttributesWithOptions(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, options core.SubscriptionOptions) error {
	if err := m.ValidateSubscriptionOptions(ctx, fed, options); err != nil {
		return err
	}
	model, err := m.model(ctx, fed)
	if err != nil {
		return err
	}
	if names, ok := model.(core.FOMHandleNameLookup); ok {
		if _, exists := names.ObjectClassName(cls); !exists {
			return core.ErrObjectClassNotFound
		}
		for _, attr := range attrs {
			if _, exists := names.AttributeName(cls, attr); !exists {
				return core.ErrAttributeNotFound
			}
		}
	}
	err = m.change(ctx, fed, func(st *federationState) {
		for _, attr := range attrs {
			key := objAttrKey{cls, attr}
			if st.objSubs[key] == nil {
				st.objSubs[key] = map[core.FederateHandle]struct{}{}
			}
			if st.objOptions[key] == nil {
				st.objOptions[key] = map[core.FederateHandle]core.SubscriptionOptions{}
			}
			st.objSubs[key][sub] = struct{}{}
			st.objOptions[key][sub] = options
			delete(st.objSubSnapshots, key)
		}
	})
	// Discovery follows successful admission for both active and passive modes.
	if err == nil && m.onSubscribeObjectClass != nil {
		m.onSubscribeObjectClass(ctx, fed, sub, cls, slices.Clone(attrs))
	}
	return err
}

func (m *Manager) SubscribeInteractionClassWithOptions(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.InteractionClassHandle, active bool) error {
	model, err := m.model(ctx, fed)
	if err != nil {
		return err
	}
	if names, ok := model.(core.FOMHandleNameLookup); ok {
		if _, exists := names.InteractionClassName(cls); !exists {
			return core.ErrInteractionClassNotFound
		}
	}
	return m.change(ctx, fed, func(st *federationState) {
		if st.intSubs[cls] == nil {
			st.intSubs[cls] = map[core.FederateHandle]struct{}{}
		}
		if st.intActive[cls] == nil {
			st.intActive[cls] = map[core.FederateHandle]bool{}
		}
		st.intSubs[cls][sub] = struct{}{}
		st.intActive[cls][sub] = active
		delete(st.intSubSnapshots, cls)
	})
}

func (m *Manager) ObjectSubscriptionOptions(fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attr core.AttributeHandle) (core.SubscriptionOptions, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return core.SubscriptionOptions{}, false
	}
	key := objAttrKey{cls, attr}
	if _, ok := st.objSubs[key][sub]; !ok {
		return core.SubscriptionOptions{}, false
	}
	return objectOptions(st, key, sub), true
}

func objectOptions(st *federationState, key objAttrKey, sub core.FederateHandle) core.SubscriptionOptions {
	if options, ok := st.objOptions[key][sub]; ok {
		return options
	}
	return core.SubscriptionOptions{Active: true}
}

func interactionActive(st *federationState, cls core.InteractionClassHandle, sub core.FederateHandle) bool {
	if active, ok := st.intActive[cls][sub]; ok {
		return active
	}
	return true
}

func defaultSwitches(model core.FOMHandle) [4]bool {
	// This is an explicit compatibility fallback for pre-option gorti behavior,
	// not a claim about normative 2010 defaults. FDD values override it.
	values := [4]bool{true, true, true, true}
	if defaults, ok := model.(core.FOMAdvisoryDefaults); ok {
		for i, kind := range []core.AdvisorySwitchKind{core.AdvisoryObjectClassRelevance, core.AdvisoryAttributeRelevance, core.AdvisoryAttributeScope, core.AdvisoryInteractionRelevance} {
			if value, exists := defaults.AdvisorySwitchDefault(kind); exists {
				values[i] = value
			}
		}
	}
	return values
}

func switchEnabled(st *federationState, h core.FederateHandle, kind core.AdvisorySwitchKind) bool {
	if kind < core.AdvisoryObjectClassRelevance || kind > core.AdvisoryInteractionRelevance {
		return false
	}
	if value, ok := st.switches[h][kind]; ok {
		return value
	}
	return st.defaults[int(kind)-1]
}

func (m *Manager) AdvisorySwitchEnabled(fed core.FederationName, h core.FederateHandle, kind core.AdvisorySwitchKind) bool {
	if kind < core.AdvisoryObjectClassRelevance || kind > core.AdvisoryInteractionRelevance {
		return false
	}
	m.mu.RLock()
	st := m.fed[fed]
	if st != nil {
		enabled := switchEnabled(st, h, kind)
		m.mu.RUnlock()
		return enabled
	}
	m.mu.RUnlock()
	model, _ := m.model(context.Background(), fed)
	return defaultSwitches(model)[int(kind)-1]
}

func (m *Manager) SetAdvisorySwitch(ctx context.Context, fed core.FederationName, h core.FederateHandle, kind core.AdvisorySwitchKind, enabled bool) error {
	if kind < core.AdvisoryObjectClassRelevance || kind > core.AdvisoryInteractionRelevance {
		return core.ErrInvalidAdvisorySwitch
	}
	return m.changeChecked(ctx, fed, func(st *federationState) error {
		if switchEnabled(st, h, kind) == enabled {
			return &core.AdvisorySwitchAlreadySetError{Kind: kind, Enabled: enabled}
		}
		if st.switches[h] == nil {
			st.switches[h] = map[core.AdvisorySwitchKind]bool{}
		}
		st.switches[h][kind] = enabled
		return nil
	}, false)
}

type relevanceKey struct {
	kind      core.AdvisorySwitchKind
	publisher core.FederateHandle
	class     uint64
}

func relevant(st *federationState, model core.FOMHandle) map[relevanceKey]bool {
	result := map[relevanceKey]bool{}
	objectRelevance(st, model, result)
	interactionRelevance(st, model, result)
	return result
}

func objectRelevance(st *federationState, model core.FOMHandle, result map[relevanceKey]bool) {
	for pubKey, pubs := range st.objPubs {
		for publisher := range pubs {
			key := relevanceKey{core.AdvisoryObjectClassRelevance, publisher, uint64(pubKey.cls)}
			if result[key] {
				continue
			}
			result[key] = false
			for regional, table := range []map[objAttrKey]map[core.FederateHandle]struct{}{st.objSubs, st.regionalObjects} {
				for subKey, subs := range table {
					if !matchingAttribute(model, pubKey, subKey) {
						continue
					}
					for sub := range subs {
						if sub != publisher && (regional == 1 || objectOptions(st, subKey, sub).Active) {
							result[key] = true
							break
						}
					}
					if result[key] {
						break
					}
				}
				if result[key] {
					break
				}
			}
		}
	}
}

func interactionRelevance(st *federationState, model core.FOMHandle, result map[relevanceKey]bool) {
	for cls, pubs := range st.intPubs {
		ancestors := []core.InteractionClassHandle{cls}
		if hierarchy, ok := model.(core.FOMClassAncestry); ok {
			ancestors = hierarchy.InteractionClassAncestors(cls)
		}
		for publisher := range pubs {
			key := relevanceKey{core.AdvisoryInteractionRelevance, publisher, uint64(cls)}
			result[key] = false
			for _, parent := range ancestors {
				for sub := range st.intSubs[parent] {
					if sub != publisher && interactionActive(st, parent, sub) {
						result[key] = true
					}
				}
				for sub := range st.regionalInteractions[parent] {
					if sub != publisher {
						result[key] = true
					}
				}
			}
		}
	}
}

func matchingAttribute(model core.FOMHandle, pub, sub objAttrKey) bool {
	if pub.cls == sub.cls {
		return pub.attr == sub.attr
	}
	hierarchy, ok := model.(core.FOMClassAncestry)
	if !ok || !slices.Contains(hierarchy.ObjectClassAncestors(pub.cls), sub.cls) {
		return false
	}
	names, ok := model.(core.FOMHandleNameLookup)
	if !ok {
		return false
	}
	name, exists := names.AttributeName(sub.cls, sub.attr)
	if !exists {
		return false
	}
	handle, exists := model.LookupAttribute(pub.cls, name)
	return exists && handle == pub.attr
}

func (m *Manager) change(ctx context.Context, fed core.FederationName, mutate func(*federationState)) error {
	return m.changeMode(ctx, fed, mutate, false)
}

func (m *Manager) changeMandatory(ctx context.Context, fed core.FederationName, mutate func(*federationState)) error {
	return m.changeMode(ctx, fed, mutate, true)
}

func (m *Manager) changeMode(ctx context.Context, fed core.FederationName, mutate func(*federationState), mandatory bool) error {
	return m.changeChecked(ctx, fed, func(st *federationState) error { mutate(st); return nil }, mandatory)
}

func (m *Manager) changeChecked(ctx context.Context, fed core.FederationName, mutate func(*federationState) error, mandatory bool) error {
	model, err := m.model(ctx, fed)
	if err != nil && !mandatory {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.fed[fed]
	st := cloneFederationState(previous)
	if previous == nil {
		st.defaults = defaultSwitches(model)
	}
	before := desiredAdvice(st, model)
	if err := mutate(st); err != nil {
		return err
	}
	if m.advisoryOutbox == nil {
		m.fed[fed] = st
		return nil
	}
	after := desiredAdvice(st, model)
	for key, value := range after {
		newlyEnabled := previous != nil && !switchEnabled(previous, key.Recipient, key.Kind) && value.Relevant
		if (before[key] != value || newlyEnabled) && switchEnabled(st, key.Recipient, key.Kind) {
			st.pendingAdvice[key] = struct{}{}
		}
	}
	deliveries := pendingDeliveries(st, after)
	err = m.commitDeclarationChange(ctx, fed, previous, st, deliveries, mandatory)
	if err == nil {
		clear(st.pendingAdvice)
	}
	return err
}
