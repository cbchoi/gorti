package declaration

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type rateObject struct {
	class  core.ObjectClassHandle
	owners map[core.AttributeHandle]core.FederateHandle
}

// The pending set holds at most one current advisory per live target. Retrying
// recomputes its payload, so obsolete rate transitions never form a backlog.
type adviceKey struct {
	Kind      core.AdvisorySwitchKind
	Recipient core.FederateHandle
	Class     uint64
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
}

func (m *Manager) SyncObjectUpdateRateOwners(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, cls core.ObjectClassHandle, owners map[core.AttributeHandle]core.FederateHandle) error {
	if obj == 0 || cls == 0 {
		return core.ErrObjectHandleInvalid
	}
	for attr := range owners {
		if attr == 0 {
			return core.ErrAttributeNotFound
		}
	}
	return m.changeMandatory(ctx, fed, func(st *federationState) { st.rateObjects[obj] = rateObject{class: cls, owners: maps.Clone(owners)} })
}

func (m *Manager) RemoveObjectUpdateRate(ctx context.Context, fed core.FederationName, obj core.ObjectHandle) error {
	return m.changeMandatory(ctx, fed, func(st *federationState) {
		delete(st.rateObjects, obj)
		st.regionalRates = slices.DeleteFunc(st.regionalRates, func(s core.RegionalUpdateRateSubscription) bool { return s.Object == obj })
	})
}

func (m *Manager) SetRegionalUpdateRateSubscriptions(ctx context.Context, fed core.FederationName, entries []core.RegionalUpdateRateSubscription) error {
	seen := map[core.RegionalUpdateRateSubscription]struct{}{}
	for _, e := range entries {
		if e.Object == 0 || e.Attribute == 0 || e.Subscriber == 0 {
			return fmt.Errorf("declaration: invalid regional rate target")
		}
		if err := m.ValidateSubscriptionOptions(ctx, fed, e.Options); err != nil {
			return err
		}
		if _, ok := seen[e]; ok {
			return fmt.Errorf("declaration: duplicate regional rate demand")
		}
		seen[e] = struct{}{}
	}
	return m.change(ctx, fed, func(st *federationState) { st.regionalRates = slices.Clone(entries) })
}

func removeRateParticipant(st *federationState, h core.FederateHandle) {
	for obj, value := range st.rateObjects {
		for attr, owner := range value.owners {
			if owner == h {
				value.owners[attr] = 0
			}
		}
		st.rateObjects[obj] = value
	}
	st.regionalRates = slices.DeleteFunc(st.regionalRates, func(e core.RegionalUpdateRateSubscription) bool { return e.Subscriber == h })
	for key := range st.pendingAdvice {
		if key.Recipient == h {
			delete(st.pendingAdvice, key)
		}
	}
}

func attributeDemand(st *federationState, model core.FOMHandle, obj core.ObjectHandle, attr core.AttributeHandle) core.UpdateRateDemand {
	state := st.rateObjects[obj]
	owner := state.owners[attr]
	demand := core.UpdateRateDemand{}
	add := func(sub core.FederateHandle, options core.SubscriptionOptions) {
		addAttributeDemand(&demand, model, owner, sub, options)
	}
	for key, subs := range st.objSubs {
		if matchingAttribute(model, objAttrKey{state.class, attr}, key) {
			for sub := range subs {
				add(sub, objectOptions(st, key, sub))
			}
		}
	}
	for _, entry := range st.regionalRates {
		if entry.Object == obj && entry.Attribute == attr {
			add(entry.Subscriber, entry.Options)
		}
	}
	return demand
}

func addAttributeDemand(demand *core.UpdateRateDemand, model core.FOMHandle, owner, sub core.FederateHandle, options core.SubscriptionOptions) {
	if sub == owner || !options.Active {
		return
	}
	if options.UpdateRateDesignator == "" {
		*demand = core.UpdateRateDemand{Relevant: true, Default: true}
		return
	}
	if demand.Default {
		return
	}
	rates, ok := model.(core.FOMUpdateRates)
	if !ok {
		return
	}
	rate, ok := rates.UpdateRateValue(options.UpdateRateDesignator)
	if !ok {
		return
	}
	if !demand.Relevant || rate > demand.Rate || (rate == demand.Rate && options.UpdateRateDesignator < demand.Designator) {
		*demand = core.UpdateRateDemand{Relevant: true, Rate: rate, Designator: options.UpdateRateDesignator}
	}
}

func (m *Manager) EffectiveAttributeUpdateRate(fed core.FederationName, obj core.ObjectHandle, attr core.AttributeHandle) (core.UpdateRateDemand, bool) {
	model, err := m.model(context.Background(), fed)
	if err != nil {
		return core.UpdateRateDemand{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return core.UpdateRateDemand{}, false
	}
	state, ok := st.rateObjects[obj]
	if !ok {
		return core.UpdateRateDemand{}, false
	}
	if _, ok := state.owners[attr]; !ok {
		return core.UpdateRateDemand{}, false
	}
	return attributeDemand(st, model, obj, attr), true
}

// RetryAdvisories is explicit and bounded; ordinary declaration changes also
// retry pending lifecycle work. It does not run a wall-clock rate scheduler.
func (m *Manager) RetryAdvisories(ctx context.Context, fed core.FederationName) error {
	return m.change(ctx, fed, func(*federationState) {})
}

func desiredAdvice(st *federationState, model core.FOMHandle) map[adviceKey]core.UpdateRateDemand {
	result := map[adviceKey]core.UpdateRateDemand{}
	for key, value := range relevant(st, model) {
		// MOM uses the same reserved internal identity as object updates. It
		// has no client stream and must not receive external publisher advice.
		if key.publisher == ^core.FederateHandle(0) {
			continue
		}
		result[adviceKey{Kind: key.kind, Recipient: key.publisher, Class: key.class}] = core.UpdateRateDemand{Relevant: value}
	}
	for obj, state := range st.rateObjects {
		for attr, owner := range state.owners {
			if owner != 0 && owner != ^core.FederateHandle(0) {
				result[adviceKey{Kind: core.AdvisoryAttributeRelevance, Recipient: owner, Object: obj, Attribute: attr}] = attributeDemand(st, model, obj, attr)
			}
		}
	}
	return result
}

func pendingDeliveries(st *federationState, desired map[adviceKey]core.UpdateRateDemand) []core.OutboxDelivery {
	var keys []adviceKey
	for key := range st.pendingAdvice {
		if _, live := desired[key]; !live || !switchEnabled(st, key.Recipient, key.Kind) {
			delete(st.pendingAdvice, key)
			continue
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareAdvice)
	var deliveries []core.OutboxDelivery
	for _, key := range keys {
		demand := desired[key]
		var event core.OutboundEvent
		switch key.Kind {
		case core.AdvisoryObjectClassRelevance:
			if demand.Relevant {
				event = startRegistrationEvent(core.ObjectClassHandle(key.Class))
			} else {
				event = stopRegistrationEvent(core.ObjectClassHandle(key.Class))
			}
		case core.AdvisoryInteractionRelevance:
			if demand.Relevant {
				event = turnInteractionsOnEvent(core.InteractionClassHandle(key.Class))
			} else {
				event = turnInteractionsOffEvent(core.InteractionClassHandle(key.Class))
			}
		case core.AdvisoryAttributeRelevance:
			e := &core.UpdateRateAdvisory{Object: key.Object, Attributes: []core.AttributeHandle{key.Attribute}, Enabled: demand.Relevant}
			if demand.Relevant && !demand.Default {
				name := demand.Designator
				e.UpdateRateDesignator = &name
			}
			event = e
		}
		if event != nil {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: key.Recipient, Event: event})
		}
	}
	return deliveries
}
