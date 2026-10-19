package federation

import (
	"fmt"
	"sort"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// MemberIdentity reads membership under the roster lock. Callers that authorize
// a mutation must retain an operation lease through that mutation.
func (m *Manager) MemberIdentity(fed core.FederationName, h core.FederateHandle) (core.MemberIdentity, error) {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	if !ok || fs.destroying {
		m.mu.RUnlock()
		return core.MemberIdentity{}, core.ErrFederationNotFound
	}
	m.mu.RUnlock()
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	name, ok := fs.handleToName[h]
	if !ok {
		return core.MemberIdentity{}, core.ErrFederateNotJoined
	}
	return core.MemberIdentity{Federation: fed, Generation: fs.generation, Name: name, Handle: h, Incarnation: fs.membershipIncarnation[h]}, nil
}

func validResignAction(action core.ResignAction) bool {
	switch action {
	case core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteThenDivest,
		core.ResignActionCancelThenDelete, core.ResignActionCancelPendingOwnership,
		core.ResignActionNoAction, core.ResignActionDeleteObjects:
		return true
	}
	return false
}

func (m *Manager) SetAutomaticResignDirective(fed core.FederationName, h core.FederateHandle, action core.ResignAction) error {
	if !validResignAction(action) {
		return core.ErrResignActionUnsupported
	}
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return core.ErrFederationNotFound
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	name, ok := fs.handleToName[h]
	if !ok {
		return core.ErrFederateNotJoined
	}
	fs.automaticResign[name] = action
	return nil
}

func (m *Manager) GetAutomaticResignDirective(fed core.FederationName, h core.FederateHandle) (core.ResignAction, error) {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return 0, core.ErrFederationNotFound
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	name, ok := fs.handleToName[h]
	if !ok {
		return 0, core.ErrFederateNotJoined
	}
	return fs.automaticResign[name], nil
}

type AutomaticResignDirective struct {
	Name   string            `json:"name"`
	Action core.ResignAction `json:"action"`
}

type AutomaticResignState struct {
	Version    uint32                     `json:"version"`
	Directives []AutomaticResignDirective `json:"directives"`
}

func (m *Manager) ExportAutomaticResignState(fed core.FederationName) (AutomaticResignState, error) {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return AutomaticResignState{}, core.ErrFederationNotFound
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	state := AutomaticResignState{Version: 1, Directives: make([]AutomaticResignDirective, 0, len(fs.automaticResign))}
	for name, action := range fs.automaticResign {
		state.Directives = append(state.Directives, AutomaticResignDirective{Name: name, Action: action})
	}
	sort.Slice(state.Directives, func(i, j int) bool { return state.Directives[i].Name < state.Directives[j].Name })
	return state, nil
}

func ValidateAutomaticResignState(state AutomaticResignState) error {
	if state.Version != 1 {
		return fmt.Errorf("automatic resign: unsupported state version %d", state.Version)
	}
	names := make(map[string]bool, len(state.Directives))
	for _, d := range state.Directives {
		if d.Name == "" || names[d.Name] || !validResignAction(d.Action) {
			return fmt.Errorf("automatic resign: invalid or duplicate directive")
		}
		names[d.Name] = true
	}
	return nil
}

func (m *Manager) ImportAutomaticResignState(fed core.FederationName, state AutomaticResignState) error {
	if err := ValidateAutomaticResignState(state); err != nil {
		return err
	}
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return core.ErrFederationNotFound
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(state.Directives) != len(fs.nameToHandle) {
		return fmt.Errorf("automatic resign: roster mismatch")
	}
	values := make(map[string]core.ResignAction, len(state.Directives))
	for _, d := range state.Directives {
		if _, ok := fs.nameToHandle[d.Name]; !ok {
			return fmt.Errorf("automatic resign: unknown federate %q", d.Name)
		}
		values[d.Name] = d.Action
	}
	fs.automaticResign = values
	return nil
}
