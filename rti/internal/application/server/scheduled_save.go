package server

import (
	"context"
	"log/slog"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// scheduledCheckpointAdmission rechecks a pending logical-time checkpoint after
// the transport has released membership and service admission, including mutation
// stream frames and lifecycle calls. It never upgrades a held read lease. Time
// evaluation therefore cannot acquire checkpoint admission in reverse lock order.
type scheduledCheckpointAdmission struct {
	manager scheduledSaveManager
	logger  *slog.Logger
}

type scheduledSaveManager interface {
	AcquireService(core.FederationName) (func(), error)
	AcquireCheckpoint(core.FederationName) func()
	HasPendingScheduledSave(core.FederationName) bool
	ProgressScheduledSave(context.Context, core.FederationName) error
}

func newScheduledCheckpointAdmission(manager scheduledSaveManager, logger *slog.Logger) *scheduledCheckpointAdmission {
	if logger == nil {
		logger = slog.Default()
	}
	return &scheduledCheckpointAdmission{manager: manager, logger: logger}
}

func (a *scheduledCheckpointAdmission) AcquireService(fed core.FederationName) (func(), error) {
	release, err := a.manager.AcquireService(fed)
	if err != nil {
		return nil, err
	}
	return a.afterRelease(fed, release), nil
}

func (a *scheduledCheckpointAdmission) AcquireFederateService(fed core.FederationName, h core.FederateHandle) (func(), error) {
	var release func()
	var err error
	if manager, ok := a.manager.(core.FederateServiceAdmission); ok {
		release, err = manager.AcquireFederateService(fed, h)
	} else {
		release, err = a.manager.AcquireService(fed)
	}
	if err != nil {
		return nil, err
	}
	return a.afterRelease(fed, release), nil
}

func (a *scheduledCheckpointAdmission) AcquireCheckpoint(fed core.FederationName) func() {
	return a.afterRelease(fed, a.manager.AcquireCheckpoint(fed))
}

func (a *scheduledCheckpointAdmission) afterRelease(fed core.FederationName, release func()) func() {
	return func() {
		release()
		if !a.manager.HasPendingScheduledSave(fed) {
			return
		}
		// A completed service's request context may already be cancelled. Time
		// readiness is owned by the accepted schedule, not that caller's lifetime.
		if err := a.manager.ProgressScheduledSave(context.Background(), fed); err != nil {
			a.logger.Error("scheduled checkpoint progression failed", "federation", fed, "error", err)
		}
	}
}
