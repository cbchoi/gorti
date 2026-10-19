package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

const SynchronizationCheckpointPrefix = "GSY2\n"

// StrictSynchronization preserves admitted callbacks until invocation completion.
// Root restore must schedule lifecycle retry after committing this snapshot.
type StrictSynchronization struct{ Manager *syncpkg.Manager }

func (s StrictSynchronization) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("sync snapshot: manager required")
	}
	state := s.Manager.ExportState(fed)
	if state == nil {
		return nil, nil
	}
	return marshalStrictSynchronization(state)
}

func marshalStrictSynchronization(state *syncpkg.ManagerState) ([]byte, error) {
	if err := syncpkg.ValidateSnapshot(state); err != nil {
		return nil, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return append([]byte(SynchronizationCheckpointPrefix), data...), nil
}

func isStrictSynchronization(data []byte) bool {
	return bytes.HasPrefix(data, []byte(SynchronizationCheckpointPrefix))
}

func readStrictSynchronization(data []byte) (*syncpkg.ManagerState, error) {
	if !isStrictSynchronization(data) {
		return nil, fmt.Errorf("sync snapshot: missing versioned envelope")
	}
	var state syncpkg.ManagerState
	if err := decodeSnapshotJSON(data[len(SynchronizationCheckpointPrefix):], &state); err != nil {
		return nil, fmt.Errorf("sync snapshot: %w", err)
	}
	if err := syncpkg.ValidateSnapshot(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s StrictSynchronization) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("sync snapshot: manager required")
	}
	if len(data) == 0 {
		return s.Manager.PrepareImport(fed, nil)
	}
	if !isStrictSynchronization(data) {
		if s.Manager.RequiresInvocationReceipts() {
			return nil, fmt.Errorf("sync snapshot: legacy image lacks exact admitted callback history")
		}
		return (Synchronization{Manager: s.Manager}).PrepareRestore(fed, data)
	}
	state, err := readStrictSynchronization(data)
	if err != nil {
		return nil, err
	}
	return s.Manager.PrepareImport(fed, state)
}

func (s StrictSynchronization) Unmarshal(fed core.FederationName, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	commit()
	return nil
}

func (StrictSynchronization) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	if !isStrictSynchronization(data) {
		return (Synchronization{}).RewriteFederates(data, rewrite)
	}
	state, err := readStrictSynchronization(data)
	if err != nil {
		return nil, err
	}
	if err := rewriteTypedFederates(reflect.ValueOf(state), rewrite); err != nil {
		return nil, err
	}
	return marshalStrictSynchronization(state)
}

func (v *graphValidationOf[T]) validateStrictSynchronization(data []byte) error {
	state, err := readStrictSynchronization(data)
	if err != nil {
		return err
	}
	for _, point := range state.Points {
		if point.Label == "" {
			return fmt.Errorf("sync snapshot: empty label")
		}
		for _, h := range point.Required {
			if !v.member(h) {
				return fmt.Errorf("sync snapshot: participant not in saved roster")
			}
		}
	}
	for _, callback := range state.Obligations {
		if !v.member(callback.Recipient) {
			return fmt.Errorf("sync snapshot: callback recipient not in saved roster")
		}
	}
	return nil
}
