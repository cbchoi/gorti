package declaration

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type objectPublicationKey struct {
	class    core.ObjectClassHandle
	federate core.FederateHandle
}

// objPubs contains effective publications. History distinguishes implicit
// privilege from explicit publication/unpublication within the current lifetime.
type ObjectPublicationHistoryState struct {
	Class                  core.ObjectClassHandle
	Federate               core.FederateHandle
	DeletePrivilegeTouched bool
}

func deletePrivilege(model core.FOMHandle, cls core.ObjectClassHandle) core.AttributeHandle {
	if model == nil {
		return 0
	}
	privilege, ok := model.LookupAttribute(cls, "HLAprivilegeToDeleteObject")
	if !ok {
		return 0
	}
	return privilege
}

func exportPublicationHistory(st *federationState) []ObjectPublicationHistoryState {
	var result []ObjectPublicationHistoryState
	for key, touched := range st.objPubHistory {
		result = append(result, ObjectPublicationHistoryState{key.class, key.federate, touched})
	}
	slices.SortFunc(result, func(a, b ObjectPublicationHistoryState) int {
		if order := cmp.Compare(a.Class, b.Class); order != 0 {
			return order
		}
		return cmp.Compare(a.Federate, b.Federate)
	})
	return result
}

func restorePublicationHistory(st *federationState, state *ManagerState, model core.FOMHandle) error {
	for i, entry := range state.ObjectPublicationHistory {
		key := objectPublicationKey{entry.Class, entry.Federate}
		if entry.Class == 0 || entry.Federate == 0 {
			return fmt.Errorf("declaration publication history %d: zero class or federate", i)
		}
		if _, exists := st.objPubHistory[key]; exists {
			return fmt.Errorf("declaration publication history %d: duplicate class/federate", i)
		}
		attrs := publishedAttributes(st, entry.Federate, entry.Class)
		if len(attrs) == 0 {
			return fmt.Errorf("declaration publication history %d: no active publication", i)
		}
		privilege := deletePrivilege(model, entry.Class)
		if model != nil && (privilege == 0 || !entry.DeletePrivilegeTouched && (len(attrs) < 2 || !slices.Contains(attrs, privilege))) {
			return fmt.Errorf("declaration publication history %d: inconsistent implicit privilege", i)
		}
		st.objPubHistory[key] = entry.DeletePrivilegeTouched
	}
	return restoreMissingPublicationHistory(st, state, model)
}

func restoreMissingPublicationHistory(st *federationState, state *ManagerState, model core.FOMHandle) error {
	active := map[objectPublicationKey]struct{}{}
	for key, members := range st.objPubs {
		for federate := range members {
			active[objectPublicationKey{key.cls, federate}] = struct{}{}
		}
	}
	for key := range active {
		privilege := deletePrivilege(model, key.class)
		if privilege == 0 {
			continue
		}
		if _, exists := st.objPubHistory[key]; exists {
			continue
		}
		if state.ObjectPublicationHistory != nil {
			return fmt.Errorf("declaration publication history: missing active class/federate")
		}
		// Older snapshots stored only explicit publications and no history.
		// Preserve those explicit entries; lost earlier unpublication is unknowable.
		attrKey := objAttrKey{key.class, privilege}
		_, explicit := st.objPubs[attrKey][key.federate]
		st.objPubHistory[key] = explicit
		if !explicit {
			if st.objPubs[attrKey] == nil {
				st.objPubs[attrKey] = map[core.FederateHandle]struct{}{}
			}
			st.objPubs[attrKey][key.federate] = struct{}{}
		}
	}
	return nil
}
