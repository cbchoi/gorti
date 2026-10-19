package core

import "context"

// CommittedCheckpointLabelResolver resolves a durable save decision without
// changing the target execution or weakening exact source-provenance checks.
type CommittedCheckpointLabelResolver interface {
	ResolveCommittedCheckpointLabel(context.Context, FederationName, string, uint64) (uint64, error)
}
