package core

import "context"

// ClientCheckpointCoordinator persists SDK-side RTI state with the save decision.
type ClientCheckpointCoordinator interface {
	FederateSaveCompleteWithClientState(context.Context, FederationName, FederateHandle, []byte) error
}

// CheckpointManifest is the persistence-neutral checkpoint header. Source
// generation is explicit; cross-execution translation creates a separate copy.
type CheckpointManifest = CheckpointManifestOf[LogicalTime]

type CheckpointManifestOf[T LogicalTimeScalar] struct {
	Version                       uint32           `json:"version"`
	Federation                    FederationName   `json:"federation"`
	FederationGeneration          *uint64          `json:"federation_generation,omitempty"`
	FOMSHA256                     string           `json:"fom_sha256,omitempty"`
	LogicalTimeImplementationName string           `json:"logical_time_implementation_name,omitempty"`
	Label                         string           `json:"label"`
	SaveTime                      *T               `json:"save_time,omitempty"`
	Federates                     []FederateHandle `json:"federates"`
	FederateNames                 []string         `json:"federate_names,omitempty"`
	FederateTypes                 []string         `json:"federate_types,omitempty"`
	// Nil outer slice means unknown legacy module history, not no modules.
	FederateModules [][]FOMModule `json:"federate_modules,omitempty"`
	// Private runtime checkpoints, index-parallel to Federates. Nil is legacy absence.
	FederateClientStates [][]byte          `json:"federate_client_states,omitempty"`
	EventLogBytes        uint64            `json:"event_log_bytes"`
	ManagerSnapshots     map[string][]byte `json:"manager_snapshots,omitempty"`
}

type CheckpointSnapshotter interface {
	Marshal(FederationName) ([]byte, error)
	Unmarshal(FederationName, []byte) error
}

type FederateSnapshotRebinder interface {
	RewriteFederates([]byte, func(FederateHandle) (FederateHandle, error)) ([]byte, error)
}

// RestoreParticipant maps one consenting live session to its installed target
// identity. SavedHandle may be freshly allocated for cross-generation restore.
type RestoreParticipant struct {
	Name                 string
	CurrentHandle        FederateHandle
	SavedHandle          FederateHandle
	SavedModules         []FOMModule
	ModulesKnown         bool
	SavedAutomaticResign *ResignAction
}

// TransitionSnapshotter delegates state publication to the checked membership
// transition, rather than returning a misleading no-op commit. It is invoked
// for absent optional entries too, so a legacy default is an explicit policy.
type TransitionSnapshotter interface {
	PrepareRestoreParticipants(FederationName, []byte, []RestoreParticipant) ([]RestoreParticipant, error)
}
