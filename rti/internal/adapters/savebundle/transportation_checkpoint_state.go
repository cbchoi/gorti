package savebundle

import "github.com/cbchoi/gorti/rti/internal/hla/core"
import "github.com/cbchoi/gorti/rti/internal/hla/object"

func ReadTransportationCheckpointFOMState(data []byte) (*core.FOMRepositoryState, error) {
	var state core.FOMRepositoryState
	if err := decodeSnapshotJSON(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// ReadTransportationCheckpointObjectStateOf shares the object codec's strict
// private decoder without publishing state or bypassing its version rules.
func ReadTransportationCheckpointObjectStateOf[T core.LogicalTimeScalar](data []byte) (*object.RegistryStateOf[T], error) {
	if len(data) == 0 {
		return nil, nil
	}
	var wire objectSnapshotV1Of[T]
	if err := decodeSnapshotJSON(data, &wire); err != nil {
		return nil, err
	}
	if err := validateObjectSnapshotVersionOf[T](wire); err != nil {
		return nil, err
	}
	return wire.State, nil
}
