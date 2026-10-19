package savebundle

import (
	"encoding/json"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// SaveHistoryRecord is a versioned completion journal entry, not a manager
// snapshot. The boundary identifies a durable coordinator decision made with
// all successful-save notifications reserved, not observed SDK invocations.
type SaveHistoryRecordOf[T core.LogicalTimeScalar] struct {
	LogicalTimeImplementationName string              `json:"logical_time_implementation_name,omitempty"`
	Version                       uint32              `json:"version"`
	Sequence                      uint64              `json:"sequence"`
	Federation                    core.FederationName `json:"federation"`
	Generation                    *uint64             `json:"generation"`
	Boundary                      string              `json:"boundary"`
	Name                          string              `json:"name"`
	Time                          *T                  `json:"time"`
}

const (
	SaveHistoryFreshBoundary  = "fresh-execution"
	SaveHistoryCommitBoundary = "reserved-success-fanout"
)

func EncodeSaveHistoryRecordOf[T core.LogicalTimeScalar](record SaveHistoryRecordOf[T]) ([]byte, error) {
	if err := validateSaveHistoryRecordOf[T](record); err != nil {
		return nil, err
	}
	return json.Marshal(record)
}

func DecodeSaveHistoryRecordOf[T core.LogicalTimeScalar](data []byte) (SaveHistoryRecordOf[T], error) {
	// Pointer fields reject missing/null scalars; raw nullable fields distinguish
	// an absent key from the explicit null emitted by the v1 encoder. The shared
	// decoder still rejects duplicate (including folded/escaped) and unknown keys.
	var fields struct {
		Version                       *uint32              `json:"version"`
		Sequence                      *uint64              `json:"sequence"`
		Federation                    *core.FederationName `json:"federation"`
		Generation                    json.RawMessage      `json:"generation"`
		Boundary                      *string              `json:"boundary"`
		Name                          *string              `json:"name"`
		Time                          json.RawMessage      `json:"time"`
		LogicalTimeImplementationName string               `json:"logical_time_implementation_name,omitempty"`
	}
	if err := decodeSnapshotJSON(data, &fields); err != nil {
		return SaveHistoryRecordOf[T]{}, fmt.Errorf("save history: %w", err)
	}
	for _, field := range []struct {
		name    string
		present bool
	}{
		{"version", fields.Version != nil}, {"sequence", fields.Sequence != nil},
		{"federation", fields.Federation != nil}, {"generation", len(fields.Generation) != 0},
		{"boundary", fields.Boundary != nil}, {"name", fields.Name != nil},
		{"time", len(fields.Time) != 0},
	} {
		if !field.present {
			return SaveHistoryRecordOf[T]{}, fmt.Errorf("save history: missing or null required field %q", field.name)
		}
	}
	record := SaveHistoryRecordOf[T]{Version: *fields.Version, Sequence: *fields.Sequence, Federation: *fields.Federation, Boundary: *fields.Boundary, Name: *fields.Name}
	record.LogicalTimeImplementationName = fields.LogicalTimeImplementationName
	if err := json.Unmarshal(fields.Generation, &record.Generation); err != nil {
		return SaveHistoryRecordOf[T]{}, fmt.Errorf("save history: generation: %w", err)
	}
	if err := json.Unmarshal(fields.Time, &record.Time); err != nil {
		return SaveHistoryRecordOf[T]{}, fmt.Errorf("save history: time: %w", err)
	}
	if err := validateSaveHistoryRecordOf[T](record); err != nil {
		return SaveHistoryRecordOf[T]{}, err
	}
	return record, nil
}

func validateSaveHistoryRecordOf[T core.LogicalTimeScalar](record SaveHistoryRecordOf[T]) error {
	if integerScalar[T]() {
		if record.Version != 2 || record.LogicalTimeImplementationName != checkpointHLAinteger64Time {
			return fmt.Errorf("save history: integer time requires version 2 and HLAinteger64Time")
		}
	} else if record.Version != 1 || record.LogicalTimeImplementationName != "" && record.LogicalTimeImplementationName != checkpointHLAfloat64Time {
		return fmt.Errorf("save history: incompatible time representation")
	}
	if record.Sequence == 0 || record.Federation == "" {
		return fmt.Errorf("save history: invalid version, sequence or federation")
	}
	switch record.Boundary {
	case SaveHistoryFreshBoundary:
		if record.Name != "" || record.Time != nil {
			return fmt.Errorf("save history: fresh execution contains a save")
		}
	case SaveHistoryCommitBoundary:
	default:
		return fmt.Errorf("save history: unsupported decision boundary %q", record.Boundary)
	}
	if record.Time != nil && !finiteSnapshotScalar(*record.Time) {
		return fmt.Errorf("save history: invalid logical time")
	}
	return nil
}
