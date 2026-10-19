package server

import (
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Compatibility aliases keep the application composition and its existing
// contract tests stable while queue ownership lives in the delivery adapter.
const (
	defaultMultiEventCapacity = delivery.DefaultEventCapacity
	maxMultiEventCapacity     = delivery.MaxEventCapacity
	defaultMultiBatchSize     = delivery.DefaultBatchSize
	maxMultiBatchSize         = delivery.MaxBatchSize
)

const defaultMultiFlushInterval = delivery.DefaultFlushInterval

type multiOutbox = delivery.MultiOutbox

// fedHandleKey remains local to the legacy in-process demos. Production
// delivery queue keys are private to the delivery adapter.
type fedHandleKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

func resolveMultiOutboxConfig(batchSize int, flushInterval time.Duration) (int, time.Duration, error) {
	return delivery.ResolveConfig(batchSize, flushInterval)
}

func resolveMultiOutboxCapacity(eventCapacity int) (int, error) {
	return delivery.ResolveCapacity(eventCapacity)
}

func newMultiOutboxWithBatch(eventCapacity, batchSize int, flushInterval time.Duration) *multiOutbox {
	return delivery.NewWithBatch(eventCapacity, batchSize, flushInterval)
}
