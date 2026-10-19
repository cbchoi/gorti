package declaration

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ManagerState excludes derived routing caches and composition hooks.
type ManagerState struct {
	ObjectPublications           []ObjectDeclarationState
	ObjectPublicationHistory     []ObjectPublicationHistoryState
	ObjectSubscriptions          []ObjectDeclarationState
	InteractionPublications      []InteractionDeclarationState
	InteractionSubscriptions     []InteractionDeclarationState
	ObjectOptions                []ObjectSubscriptionOptionsState
	InteractionOptions           []InteractionSubscriptionOptionsState
	AdvisorySwitches             []AdvisorySwitchState
	AdvisoryDefaults             *[4]bool
	RegionalObjectInterests      []ObjectDeclarationState
	RegionalInteractionInterests []InteractionDeclarationState
	UpdateRateObjects            []UpdateRateObjectState
	RegionalUpdateRates          []core.RegionalUpdateRateSubscription
	PendingAdvisories            []PendingAdvisoryState
}

type ObjectSubscriptionOptionsState struct {
	Class     core.ObjectClassHandle
	Attribute core.AttributeHandle
	Federate  core.FederateHandle
	Options   core.SubscriptionOptions
}
type InteractionSubscriptionOptionsState struct {
	Class    core.InteractionClassHandle
	Federate core.FederateHandle
	Active   bool
}
type AdvisorySwitchState struct {
	Federate core.FederateHandle
	Kind     core.AdvisorySwitchKind
	Enabled  bool
}

type ObjectDeclarationState struct {
	Class     core.ObjectClassHandle
	Attribute core.AttributeHandle
	Federates []core.FederateHandle
}

type InteractionDeclarationState struct {
	Class     core.InteractionClassHandle
	Federates []core.FederateHandle
}

// ExportState returns a detached, deterministically sorted snapshot of this
// manager. Coordination with other services requires external quiescence.
func (m *Manager) ExportState(fed core.FederationName) *ManagerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return &ManagerState{}
	}
	state := &ManagerState{
		ObjectPublications:           exportObjectDeclarations(st.objPubs),
		ObjectPublicationHistory:     exportPublicationHistory(st),
		ObjectSubscriptions:          exportObjectDeclarations(st.objSubs),
		InteractionPublications:      exportInteractionDeclarations(st.intPubs),
		InteractionSubscriptions:     exportInteractionDeclarations(st.intSubs),
		RegionalObjectInterests:      exportObjectDeclarations(st.regionalObjects),
		RegionalInteractionInterests: exportInteractionDeclarations(st.regionalInteractions),
	}
	exportOptions(st, state)
	exportUpdateRates(st, state)
	return state
}

func exportObjectDeclarations(table map[objAttrKey]map[core.FederateHandle]struct{}) []ObjectDeclarationState {
	var entries []ObjectDeclarationState
	for key, members := range table {
		entries = append(entries, ObjectDeclarationState{Class: key.cls, Attribute: key.attr, Federates: sortedHandles(members)})
	}
	slices.SortFunc(entries, func(a, b ObjectDeclarationState) int {
		if order := cmp.Compare(a.Class, b.Class); order != 0 {
			return order
		}
		return cmp.Compare(a.Attribute, b.Attribute)
	})
	return entries
}

func exportInteractionDeclarations(table map[core.InteractionClassHandle]map[core.FederateHandle]struct{}) []InteractionDeclarationState {
	var entries []InteractionDeclarationState
	for class, members := range table {
		entries = append(entries, InteractionDeclarationState{Class: class, Federates: sortedHandles(members)})
	}
	slices.SortFunc(entries, func(a, b InteractionDeclarationState) int { return cmp.Compare(a.Class, b.Class) })
	return entries
}

// PrepareRestore validates and detaches every record before returning an
// infallible, one-shot commit. A nil or empty state clears the federation's
// declarations. No advisories or subscription hooks are invoked.
//
// Validation covers handle shape and duplicate records, not membership or FOM
// provenance; the coordinating restore operation must check those and quiesce
// ordinary operations across services before committing.
func (m *Manager) PrepareRestore(fed core.FederationName, state *ManagerState) (func(), error) {
	model, err := m.model(context.Background(), fed)
	if err != nil {
		return nil, err
	}
	return m.prepareRestore(fed, state, model)
}

// PrepareRestoreWithFOM validates against the coordinator's unpublished saved
// model. It never changes the repository or bypasses named-rate validation.
func (m *Manager) PrepareRestoreWithFOM(fed core.FederationName, state *ManagerState, model core.FOMHandle) (func(), error) {
	if model == nil || !model.IsValid() {
		return nil, fmt.Errorf("declaration: valid saved FOM candidate required")
	}
	return m.prepareRestore(fed, state, model)
}

func (m *Manager) prepareRestore(fed core.FederationName, state *ManagerState, model core.FOMHandle) (func(), error) {
	if state == nil {
		state = &ManagerState{}
	}
	st := newFederationState()
	var err error
	if st.objPubs, err = restoreObjectDeclarations("object publications", state.ObjectPublications); err != nil {
		return nil, err
	}
	if err := restorePublicationHistory(st, state, model); err != nil {
		return nil, err
	}
	if st.objSubs, err = restoreObjectDeclarations("object subscriptions", state.ObjectSubscriptions); err != nil {
		return nil, err
	}
	if st.intPubs, err = restoreInteractionDeclarations("interaction publications", state.InteractionPublications); err != nil {
		return nil, err
	}
	if st.intSubs, err = restoreInteractionDeclarations("interaction subscriptions", state.InteractionSubscriptions); err != nil {
		return nil, err
	}
	if st.regionalObjects, err = restoreObjectDeclarations("regional object interests", state.RegionalObjectInterests); err != nil {
		return nil, err
	}
	if st.regionalInteractions, err = restoreInteractionDeclarations("regional interaction interests", state.RegionalInteractionInterests); err != nil {
		return nil, err
	}
	if err := restoreOptions(st, state, model); err != nil {
		return nil, err
	}
	if err := restoreUpdateRates(st, state, model); err != nil {
		return nil, err
	}
	empty := emptyRestoredState(st, state)
	return sync.OnceFunc(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if empty {
			delete(m.fed, fed)
			return
		}
		if m.fed == nil {
			m.fed = make(map[core.FederationName]*federationState)
		}
		m.fed[fed] = st
	}), nil
}

func emptyRestoredState(st *federationState, state *ManagerState) bool {
	return len(st.objPubs) == 0 && len(st.objSubs) == 0 && len(st.intPubs) == 0 && len(st.intSubs) == 0 && len(st.regionalObjects) == 0 && len(st.regionalInteractions) == 0 && len(st.switches) == 0 && state.AdvisoryDefaults == nil && len(st.rateObjects) == 0 && len(st.regionalRates) == 0 && len(st.pendingAdvice) == 0
}

// ImportState validates and immediately replaces this manager's state. Bundle
// coordination should use PrepareRestore to validate all services first.
func (m *Manager) ImportState(fed core.FederationName, state *ManagerState) error {
	commit, err := m.PrepareRestore(fed, state)
	if err != nil {
		return err
	}
	commit()
	return nil
}

func restoreObjectDeclarations(name string, entries []ObjectDeclarationState) (map[objAttrKey]map[core.FederateHandle]struct{}, error) {
	table := make(map[objAttrKey]map[core.FederateHandle]struct{}, len(entries))
	for i, entry := range entries {
		if entry.Class == 0 || entry.Attribute == 0 {
			return nil, fmt.Errorf("declaration %s record %d: zero class or attribute handle", name, i)
		}
		key := objAttrKey{cls: entry.Class, attr: entry.Attribute}
		if _, exists := table[key]; exists {
			return nil, fmt.Errorf("declaration %s record %d: duplicate class/attribute", name, i)
		}
		members, err := restoreDeclarants(entry.Federates)
		if err != nil {
			return nil, fmt.Errorf("declaration %s record %d: %w", name, i, err)
		}
		table[key] = members
	}
	return table, nil
}

func restoreInteractionDeclarations(name string, entries []InteractionDeclarationState) (map[core.InteractionClassHandle]map[core.FederateHandle]struct{}, error) {
	table := make(map[core.InteractionClassHandle]map[core.FederateHandle]struct{}, len(entries))
	for i, entry := range entries {
		if entry.Class == 0 {
			return nil, fmt.Errorf("declaration %s record %d: zero class handle", name, i)
		}
		if _, exists := table[entry.Class]; exists {
			return nil, fmt.Errorf("declaration %s record %d: duplicate class", name, i)
		}
		members, err := restoreDeclarants(entry.Federates)
		if err != nil {
			return nil, fmt.Errorf("declaration %s record %d: %w", name, i, err)
		}
		table[entry.Class] = members
	}
	return table, nil
}

func restoreDeclarants(handles []core.FederateHandle) (map[core.FederateHandle]struct{}, error) {
	if len(handles) == 0 {
		return nil, fmt.Errorf("empty federate set")
	}
	members := make(map[core.FederateHandle]struct{}, len(handles))
	for _, handle := range handles {
		if handle == 0 {
			return nil, fmt.Errorf("zero federate handle")
		}
		if _, exists := members[handle]; exists {
			return nil, fmt.Errorf("duplicate federate handle %d", handle)
		}
		members[handle] = struct{}{}
	}
	return members, nil
}
