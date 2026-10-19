package savebundle

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// Retractions persists all accepted originating IDs, not merely pending TSO.
// A missing or incomplete legacy ledger cannot establish safe ID reuse.
type RetractionsOf[T core.LogicalTimeScalar] struct{ Manager *timepkg.ManagerOf[T] }

type retractionSnapshotOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar] struct {
	Version uint32                          `json:"version"`
	History *timepkg.RetractionHistoryOf[T] `json:"history"`
}

func (s RetractionsOf[T]) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("retraction snapshot: manager is required")
	}
	history, err := s.Manager.ExportRetractionHistory(fed)
	if err != nil {
		return nil, err
	}
	return writeRetractionsOf[T](history)
}

func decodeRetractionSnapshotOf[T core.LogicalTimeScalar, N core.LogicalTimeScalar](data []byte) (*timepkg.RetractionHistoryOf[T], error) {
	if len(data) == 0 {
		return nil, core.ErrRetractionHistoryUnavailable
	}
	var wire retractionSnapshotOf[T, N]
	if err := decodeSnapshotJSON(data, &wire); err != nil {
		return nil, fmt.Errorf("retraction snapshot: %w", err)
	}
	if integerScalar[T]() && wire.Version != 3 || !integerScalar[T]() && (wire.Version < 1 || wire.Version > 2) {
		return nil, fmt.Errorf("retraction snapshot: unsupported version %d", wire.Version)
	}
	if err := timepkg.ValidateRetractionHistoryOf[T](wire.History); err != nil {
		return nil, err
	}
	return wire.History, nil
}

func (s RetractionsOf[T]) PrepareRestoreState(fed core.FederationName, data []byte) (core.PreparedRestore, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("retraction snapshot: manager is required")
	}
	history, err := readRetractionsOf[T](data)
	if err != nil {
		return nil, err
	}
	return s.Manager.PrepareRetractionHistory(fed, history)
}

func (s RetractionsOf[T]) Unmarshal(fed core.FederationName, data []byte) error {
	prepared, err := s.PrepareRestoreState(fed, data)
	if err != nil {
		return err
	}
	defer prepared.Release()
	return prepared.Commit()
}

func (s RetractionsOf[T]) PreviewNextRetractionHandle(fed core.FederationName, data []byte, producer core.FederateHandle) (uint64, error) {
	if s.Manager == nil {
		return 0, fmt.Errorf("retraction snapshot: manager is required")
	}
	history, err := readRetractionsOf[T](data)
	if err != nil {
		return 0, err
	}
	return s.Manager.PreviewNextRetractionHandle(fed, history, producer)
}
