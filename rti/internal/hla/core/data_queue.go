package core

import (
	"context"
	"errors"
)

var ErrDataQueueCoverageUnavailable = errors.New("data queue coverage unavailable")

type DataQueueCounts struct{ RO, TSO uint64 }

// ReadyDataQueue observes admitted data until the receiving callback starts.
// Implementations must not reenter the time manager while being observed.
type ReadyDataQueue interface {
	PendingDataCounts(context.Context, FederationName, FederateHandle) (DataQueueCounts, error)
}

type DataQueueMessage interface {
	DataReceivedOrder() (OrderType, bool)
}
