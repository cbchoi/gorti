package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type ObjectOf[T core.LogicalTimeScalar] struct{ Manager *object.RegistryOf[T] }

// Version 1 uses the domain DTO's exported field names inside state. Version 2
// adds deletion ownership snapshots; version 3 retains object identity and
// recipient knowledge after deletion. Version 4 retains each recipient's known
// class when it differs from the registered class. These are not RPC versions.
type objectSnapshotV1Of[T core.LogicalTimeScalar] struct {
	LogicalTimeImplementationName string                     `json:"logical_time_implementation_name,omitempty"`
	Version                       uint32                     `json:"version"`
	State                         *object.RegistryStateOf[T] `json:"state"`
	minimumVersion                uint32
}

func (s *objectSnapshotV1Of[T]) UnmarshalJSON(data []byte) error {
	type plain objectSnapshotV1Of[T]
	var decoded plain
	if err := decodeSnapshotJSON(data, &decoded); err != nil {
		return err
	}
	var fields struct {
		State struct {
			Discoveries []map[string]json.RawMessage
			Deletions   []map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	for _, discovery := range fields.State.Discoveries {
		if err := knownClassPresence(discovery, &decoded.minimumVersion); err != nil {
			return err
		}
	}
	if err := validateDeletionFieldVersions[T](fields.State.Deletions, &decoded.minimumVersion); err != nil {
		return err
	}
	*s = objectSnapshotV1Of[T](decoded)
	return nil
}

func (s ObjectOf[T]) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("object snapshot: manager is required")
	}
	state := s.Manager.ExportState(fed)
	// A legacy service path may have left inconsistent state. Do not publish a
	// save image that this adapter cannot later restore.
	if _, err := s.Manager.PrepareImportState(fed, state); err != nil {
		return nil, err
	}
	wire := objectSnapshotV1Of[T]{Version: objectSnapshotVersionOf[T](state), State: state}
	if integerScalar[T]() {
		wire.Version = 5
		wire.LogicalTimeImplementationName = checkpointHLAinteger64Time
	}
	return json.Marshal(wire)
}

func objectSnapshotVersionOf[T core.LogicalTimeScalar](state *object.RegistryStateOf[T]) uint32 {
	version := uint32(1)
	if state != nil {
		for _, discovery := range state.Discoveries {
			if discovery.KnownClass != 0 {
				version = 4
			}
		}
		for _, deletion := range state.Deletions {
			if deletion.OwnershipSnapshot || len(deletion.AttributeOwners) != 0 {
				version = max(version, 2)
			}
			if deletion.ObjectName != "" || deletion.ObjectClass != 0 || len(deletion.Recipients) != 0 {
				version = max(version, 3)
			}
			for _, recipient := range deletion.Recipients {
				if recipient.KnownClass != 0 {
					version = 4
				}
			}
		}
	}
	return version
}

func validateObjectSnapshotVersionOf[T core.LogicalTimeScalar](wire objectSnapshotV1Of[T]) error {
	if integerScalar[T]() {
		if wire.Version != 5 || wire.LogicalTimeImplementationName != checkpointHLAinteger64Time {
			return fmt.Errorf("object snapshot: integer time requires version 5 and HLAinteger64Time")
		}
	} else if wire.Version < 1 || wire.Version > 4 || wire.LogicalTimeImplementationName != "" && wire.LogicalTimeImplementationName != checkpointHLAfloat64Time {
		return fmt.Errorf("object snapshot: unsupported version %d", wire.Version)
	}
	if wire.State == nil {
		return fmt.Errorf("object snapshot: missing state")
	}
	if wire.Version < max(wire.minimumVersion, objectSnapshotVersionOf[T](wire.State)) {
		return fmt.Errorf("object snapshot: extension requires a newer version")
	}
	return nil
}

func (s ObjectOf[T]) Unmarshal(fed core.FederationName, data []byte) error {
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	commit()
	return nil
}

// PrepareRestore treats present empty bytes as an empty saved state. A missing
// manifest entry is distinct and is the coordinator's responsibility. Decoding
// and domain validation finish before the returned commit can change anything.
func (s ObjectOf[T]) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("object snapshot: manager is required")
	}
	if len(data) == 0 {
		return s.Manager.PrepareImportState(fed, nil)
	}
	var wire objectSnapshotV1Of[T]
	if err := decodeSnapshotJSON(data, &wire); err != nil {
		return nil, fmt.Errorf("object snapshot for %q: %w", fed, err)
	}
	if err := validateObjectSnapshotVersionOf[T](wire); err != nil {
		return nil, err
	}
	return s.Manager.PrepareImportState(fed, wire.State)
}

func knownClassPresence(entry map[string]json.RawMessage, minimumVersion *uint32) error {
	for key, value := range entry {
		if strings.EqualFold(key, "KnownClass") {
			*minimumVersion = max(*minimumVersion, 4)
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("object snapshot: null %s", key)
			}
		}
	}
	return nil
}

func validateDeletionFieldVersions[T core.LogicalTimeScalar](deletions []map[string]json.RawMessage, minimumVersion *uint32) error {
	for _, deletion := range deletions {
		if integerScalar[T]() {
			if err := requireSnapshotScalarField(deletion, "Timestamp"); err != nil {
				return err
			}
		}
		for key, value := range deletion {
			version := uint32(0)
			switch strings.ToLower(key) {
			case "ownershipsnapshot", "attributeowners":
				version = 2
			case "objectname", "objectclass", "recipients":
				version = 3
			}
			if version == 0 {
				continue
			}
			*minimumVersion = max(*minimumVersion, version)
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("object snapshot: null %s", key)
			}
			if strings.EqualFold(key, "Recipients") {
				var recipients []map[string]json.RawMessage
				if err := json.Unmarshal(value, &recipients); err != nil {
					return err
				}
				for _, recipient := range recipients {
					if err := knownClassPresence(recipient, minimumVersion); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
