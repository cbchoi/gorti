package declaration

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func exportOptions(st *federationState, state *ManagerState) {
	defaults := st.defaults
	state.AdvisoryDefaults = &defaults
	for key, members := range st.objOptions {
		for h, options := range members {
			state.ObjectOptions = append(state.ObjectOptions, ObjectSubscriptionOptionsState{key.cls, key.attr, h, options})
		}
	}
	slices.SortFunc(state.ObjectOptions, func(a, b ObjectSubscriptionOptionsState) int {
		if c := cmp.Compare(a.Class, b.Class); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Attribute, b.Attribute); c != 0 {
			return c
		}
		return cmp.Compare(a.Federate, b.Federate)
	})
	for cls, members := range st.intActive {
		for h, active := range members {
			state.InteractionOptions = append(state.InteractionOptions, InteractionSubscriptionOptionsState{cls, h, active})
		}
	}
	slices.SortFunc(state.InteractionOptions, func(a, b InteractionSubscriptionOptionsState) int {
		if c := cmp.Compare(a.Class, b.Class); c != 0 {
			return c
		}
		return cmp.Compare(a.Federate, b.Federate)
	})
	for h, kinds := range st.switches {
		for kind, enabled := range kinds {
			state.AdvisorySwitches = append(state.AdvisorySwitches, AdvisorySwitchState{h, kind, enabled})
		}
	}
	slices.SortFunc(state.AdvisorySwitches, func(a, b AdvisorySwitchState) int {
		if c := cmp.Compare(a.Federate, b.Federate); c != 0 {
			return c
		}
		return cmp.Compare(a.Kind, b.Kind)
	})
}

func restoreOptions(st *federationState, state *ManagerState, model core.FOMHandle) error {
	if state.AdvisoryDefaults != nil {
		st.defaults = *state.AdvisoryDefaults
	}
	for _, entry := range state.ObjectOptions {
		key := objAttrKey{entry.Class, entry.Attribute}
		if _, exists := st.objSubs[key][entry.Federate]; !exists {
			return fmt.Errorf("declaration: options for absent object subscription")
		}
		if _, exists := st.objOptions[key][entry.Federate]; exists {
			return fmt.Errorf("declaration: duplicate object subscription options")
		}
		if err := validateSubscriptionOptions(model, entry.Options); err != nil {
			return err
		}
		if st.objOptions[key] == nil {
			st.objOptions[key] = map[core.FederateHandle]core.SubscriptionOptions{}
		}
		st.objOptions[key][entry.Federate] = entry.Options
	}
	for _, entry := range state.InteractionOptions {
		if _, exists := st.intSubs[entry.Class][entry.Federate]; !exists {
			return fmt.Errorf("declaration: options for absent interaction subscription")
		}
		if _, exists := st.intActive[entry.Class][entry.Federate]; exists {
			return fmt.Errorf("declaration: duplicate interaction subscription options")
		}
		if st.intActive[entry.Class] == nil {
			st.intActive[entry.Class] = map[core.FederateHandle]bool{}
		}
		st.intActive[entry.Class][entry.Federate] = entry.Active
	}
	return restoreAdvisorySwitches(st, state.AdvisorySwitches)
}

func restoreAdvisorySwitches(st *federationState, entries []AdvisorySwitchState) error {
	for _, entry := range entries {
		if entry.Federate == 0 || entry.Kind < core.AdvisoryObjectClassRelevance || entry.Kind > core.AdvisoryInteractionRelevance {
			return fmt.Errorf("declaration: invalid advisory switch record")
		}
		if _, exists := st.switches[entry.Federate][entry.Kind]; exists {
			return fmt.Errorf("declaration: duplicate advisory switch record")
		}
		if st.switches[entry.Federate] == nil {
			st.switches[entry.Federate] = map[core.AdvisorySwitchKind]bool{}
		}
		st.switches[entry.Federate][entry.Kind] = entry.Enabled
	}
	return nil
}
