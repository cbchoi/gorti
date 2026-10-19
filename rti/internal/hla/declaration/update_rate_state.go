package declaration

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type UpdateRateOwnerState struct {
	Attribute core.AttributeHandle
	Owner     core.FederateHandle
}
type UpdateRateObjectState struct {
	Object core.ObjectHandle
	Class  core.ObjectClassHandle
	Owners []UpdateRateOwnerState
}
type PendingAdvisoryState adviceKey

func compareAdvice(a, b adviceKey) int {
	if c := cmp.Compare(a.Recipient, b.Recipient); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Class, b.Class); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Object, b.Object); c != 0 {
		return c
	}
	return cmp.Compare(a.Attribute, b.Attribute)
}

func exportUpdateRates(st *federationState, state *ManagerState) {
	for obj, v := range st.rateObjects {
		e := UpdateRateObjectState{Object: obj, Class: v.class}
		for attr, owner := range v.owners {
			e.Owners = append(e.Owners, UpdateRateOwnerState{attr, owner})
		}
		slices.SortFunc(e.Owners, func(a, b UpdateRateOwnerState) int { return cmp.Compare(a.Attribute, b.Attribute) })
		state.UpdateRateObjects = append(state.UpdateRateObjects, e)
	}
	slices.SortFunc(state.UpdateRateObjects, func(a, b UpdateRateObjectState) int { return cmp.Compare(a.Object, b.Object) })
	state.RegionalUpdateRates = slices.Clone(st.regionalRates)
	slices.SortFunc(state.RegionalUpdateRates, func(a, b core.RegionalUpdateRateSubscription) int {
		if c := cmp.Compare(a.Object, b.Object); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Attribute, b.Attribute); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Subscriber, b.Subscriber); c != 0 {
			return c
		}
		if a.Options.Active != b.Options.Active {
			if a.Options.Active {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.Options.UpdateRateDesignator, b.Options.UpdateRateDesignator)
	})
	for key := range st.pendingAdvice {
		state.PendingAdvisories = append(state.PendingAdvisories, PendingAdvisoryState(key))
	}
	slices.SortFunc(state.PendingAdvisories, func(a, b PendingAdvisoryState) int { return compareAdvice(adviceKey(a), adviceKey(b)) })
}

func restoreUpdateRates(st *federationState, state *ManagerState, model core.FOMHandle) error {
	for _, entry := range state.UpdateRateObjects {
		if entry.Object == 0 || entry.Class == 0 {
			return fmt.Errorf("declaration: invalid update-rate object")
		}
		if _, ok := st.rateObjects[entry.Object]; ok {
			return fmt.Errorf("declaration: duplicate update-rate object")
		}
		v := rateObject{class: entry.Class, owners: map[core.AttributeHandle]core.FederateHandle{}}
		for _, owner := range entry.Owners {
			if owner.Attribute == 0 {
				return fmt.Errorf("declaration: invalid update-rate attribute")
			}
			if _, ok := v.owners[owner.Attribute]; ok {
				return fmt.Errorf("declaration: duplicate update-rate owner")
			}
			v.owners[owner.Attribute] = owner.Owner
		}
		st.rateObjects[entry.Object] = v
	}
	return restoreRateDemands(st, state, model)
}

func restoreRateDemands(st *federationState, state *ManagerState, model core.FOMHandle) error {
	seen := map[core.RegionalUpdateRateSubscription]struct{}{}
	for _, e := range state.RegionalUpdateRates {
		if e.Object == 0 || e.Attribute == 0 || e.Subscriber == 0 {
			return fmt.Errorf("declaration: invalid regional update-rate demand")
		}
		if _, ok := seen[e]; ok {
			return fmt.Errorf("declaration: duplicate regional update-rate demand")
		}
		if err := validateSubscriptionOptions(model, e.Options); err != nil {
			return err
		}
		seen[e] = struct{}{}
	}
	st.regionalRates = slices.Clone(state.RegionalUpdateRates)
	desired := desiredAdvice(st, model)
	for _, entry := range state.PendingAdvisories {
		key := adviceKey(entry)
		if _, ok := desired[key]; !ok {
			return fmt.Errorf("declaration: orphan pending advisory")
		}
		if _, ok := st.pendingAdvice[key]; ok {
			return fmt.Errorf("declaration: duplicate pending advisory")
		}
		if !switchEnabled(st, key.Recipient, key.Kind) {
			return fmt.Errorf("declaration: disabled pending advisory")
		}
		st.pendingAdvice[key] = struct{}{}
	}
	return nil
}
