package server

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type synchronizationRetryGenerationAdmission interface {
	GenerationFor(core.FederationName) (uint64, bool)
	AcquireGeneration(core.FederationName, uint64) (func(), error)
}

// Install before Serve starts. Member hooks do not use this path because their
// caller may already own the FM writer. Checkpoint observation precedes the
// generation lease, matching the application's existing service admission order.
func newSynchronizationRetryAdmission(members synchronizationRetryGenerationAdmission, checkpoint saveObservationAdmission) func(context.Context, core.FederationName) (func(), error) {
	return func(ctx context.Context, fed core.FederationName) (func(), error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		checkpointRelease := func() {}
		if checkpoint != nil {
			checkpointRelease = checkpoint.AcquireObservation(fed)
		}
		if members == nil {
			checkpointRelease()
			return nil, core.ErrFederationNotFound
		}
		generation, exists := members.GenerationFor(fed)
		if !exists {
			checkpointRelease()
			return nil, core.ErrFederationNotFound
		}
		generationRelease, err := members.AcquireGeneration(fed, generation)
		if err != nil {
			checkpointRelease()
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			generationRelease()
			checkpointRelease()
			return nil, err
		}
		return func() { generationRelease(); checkpointRelease() }, nil
	}
}
