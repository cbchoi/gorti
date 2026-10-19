package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

// AutomaticResign delegates coordinated publication to the membership
// transition. It deliberately does not implement a no-op PrepareRestore.
type AutomaticResign struct{ Manager *federation.Manager }

func (s AutomaticResign) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("automatic resign snapshot: manager required")
	}
	state, err := s.Manager.ExportAutomaticResignState(fed)
	if err != nil {
		return nil, err
	}
	return json.Marshal(state)
}
func (s AutomaticResign) Unmarshal(fed core.FederationName, data []byte) error {
	if s.Manager == nil {
		return fmt.Errorf("automatic resign snapshot: manager required")
	}
	var participants []core.RestoreParticipant
	for _, member := range s.Manager.ListMembers(fed) {
		participants = append(participants, core.RestoreParticipant{Name: member.Name, CurrentHandle: member.Handle, SavedHandle: member.Handle})
	}
	prepared, err := s.PrepareRestoreParticipants(fed, data, participants)
	if err != nil {
		return err
	}
	state := federation.AutomaticResignState{Version: 1}
	for _, p := range prepared {
		state.Directives = append(state.Directives, federation.AutomaticResignDirective{Name: p.Name, Action: *p.SavedAutomaticResign})
	}
	return s.Manager.ImportAutomaticResignState(fed, state)
}
func (s AutomaticResign) PrepareRestoreParticipants(_ core.FederationName, data []byte, participants []core.RestoreParticipant) ([]core.RestoreParticipant, error) {
	names := make(map[string]bool, len(participants))
	for _, p := range participants {
		if p.Name == "" || names[p.Name] {
			return nil, fmt.Errorf("automatic resign snapshot needs unique participant names")
		}
		names[p.Name] = true
	}
	values := map[string]core.ResignAction{}
	if len(data) != 0 {
		if err := validateAutomaticResignGraph(data, names); err != nil {
			return nil, err
		}
		var state federation.AutomaticResignState
		if err := decodeSnapshotJSON(data, &state); err != nil {
			return nil, err
		}
		for _, d := range state.Directives {
			values[d.Name] = d.Action
		}
	}
	result := append([]core.RestoreParticipant(nil), participants...)
	for i := range result {
		action, ok := values[result[i].Name]
		if !ok {
			action = core.ResignActionNoAction
		}
		result[i].SavedAutomaticResign = &action
	}
	return result, nil
}
func (AutomaticResign) RewriteFederates(data []byte, _ federateRewrite) ([]byte, error) {
	var state federation.AutomaticResignState
	if err := decodeSnapshotJSON(data, &state); err != nil {
		return nil, err
	}
	if err := federation.ValidateAutomaticResignState(state); err != nil {
		return nil, err
	}
	return bytes.Clone(data), nil
}

var _ core.TransitionSnapshotter = AutomaticResign{}
