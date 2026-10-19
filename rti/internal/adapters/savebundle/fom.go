package savebundle

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type FOMRepository interface {
	ExportState(context.Context, core.FederationName) (*core.FOMRepositoryState, error)
	PrepareImportState(context.Context, core.FederationName, *core.FOMRepositoryState) (core.FOMExtensionPreparation, error)
}

type FOM struct{ Repository FOMRepository }

func (s FOM) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Repository == nil {
		return nil, fmt.Errorf("FOM snapshot: repository required")
	}
	state, err := s.Repository.ExportState(context.Background(), fed)
	if err != nil {
		return nil, err
	}
	return json.Marshal(state)
}

func (s FOM) Unmarshal(fed core.FederationName, data []byte) error {
	prepared, err := s.PrepareRestoreState(fed, data)
	if err != nil {
		return err
	}
	defer prepared.Release()
	return prepared.Commit()
}

func (s FOM) PrepareRestoreState(fed core.FederationName, data []byte) (core.PreparedRestore, error) {
	if s.Repository == nil {
		return nil, fmt.Errorf("FOM snapshot: repository required")
	}
	var state core.FOMRepositoryState
	if err := decodeSnapshotJSON(data, &state); err != nil {
		return nil, err
	}
	prepared, err := s.Repository.PrepareImportState(context.Background(), fed, &state)
	if err != nil {
		return nil, err
	}
	if err := prepared.PrepareCommit(); err != nil {
		prepared.Release()
		return nil, err
	}
	return prepared, nil
}

func (s FOM) ValidateSnapshotDigest(data []byte, expected string) error {
	var state core.FOMRepositoryState
	if err := decodeSnapshotJSON(data, &state); err != nil {
		return err
	}
	if state.Version != 1 || expected == "" || state.View.Digest != expected {
		return fmt.Errorf("FOM snapshot provenance mismatch")
	}
	// PrepareImportState verifies the source digest and every saved binding
	// before any snapshot can commit; this check binds it to the manifest.
	return nil
}
