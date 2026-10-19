package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func newRuntimeOutbox(logger *slog.Logger, capacity, batchSize int, flushInterval time.Duration) *multiOutbox {
	if logger.Enabled(context.Background(), slog.LevelDebug) {
		return delivery.NewObservedWithBatch(capacity, batchSize, flushInterval)
	}
	return newMultiOutboxWithBatch(capacity, batchSize, flushInterval)
}

// The resign hook holds exclusive lifecycle admission. Retain this binding's
// observation before removal, then log only a detached final snapshot afterward.
func retireOutbox(ctx context.Context, outbox *multiOutbox, logger *slog.Logger, fed core.FederationName, h core.FederateHandle) {
	var observation *delivery.OutboxQueueObservation
	if logger.Enabled(ctx, slog.LevelDebug) {
		observation, _ = outbox.ObserveQueue(fed, h)
	}
	outbox.Unbind(fed, h)
	if observation != nil {
		logRetiredOutbox(ctx, logger, observation)
	}
}

func logRetiredOutbox(ctx context.Context, logger *slog.Logger, observation *delivery.OutboxQueueObservation) {
	counts := observation.Inspect()
	// Concurrent subscription cancellation can unmap the binding before closing it.
	if !counts.Closed {
		return
	}
	logger.DebugContext(ctx, "retired delivery queue", "scope", "outbox-only",
		"federation", counts.Federation, "federate", counts.Handle, "callback_epoch", counts.CallbackEpoch,
		"admitted", counts.Admitted, "channel_drained", counts.Drained,
		"observation_invalidated", counts.Invalidated, "scratch", counts.Scratch, "queued", counts.Queued,
		"closed", counts.Closed)
}
