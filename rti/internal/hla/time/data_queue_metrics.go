package time

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// DataQueueMetrics observes waiting data plus committed callbacks not yet entered.
// Production drains remove, commit or restore their waiting suffix while holding
// this evaluator lease, so the two stages are disjoint at the observation point.
// Lifecycle/membership admission and ready-queue coverage are the caller's ports.
func (m *ManagerOf[T]) DataQueueMetrics(ctx context.Context, fed core.FederationName, h core.FederateHandle, ready core.ReadyDataQueue) (core.DataQueueCounts, error) {
	var counts core.DataQueueCounts
	if err := ctx.Err(); err != nil {
		return counts, err
	}
	if ready == nil {
		return counts, fmt.Errorf("%w: ready data observer missing", core.ErrDataQueueCoverageUnavailable)
	}
	ext := extOfOf[T](m)
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	defer lease.Unlock()
	if err := ctx.Err(); err != nil {
		return counts, err
	}

	ext.mu.Lock()
	var waitingErr error
	if ns := ext.getLocked(fed, h); ns != nil {
		for _, queue := range [][]bufferedTSOEventOf[T]{ns.roBuffer, ns.tsoBuffer} {
			for _, item := range queue {
				if waitingErr = countWaitingDataMessage(&counts, item.event); waitingErr != nil {
					break
				}
			}
			if waitingErr != nil {
				break
			}
		}
	}
	ext.mu.Unlock()
	if waitingErr != nil {
		return core.DataQueueCounts{}, waitingErr
	}
	// No state/store lock crosses this injected observer; it must not reenter time.
	committed, err := ready.PendingDataCounts(ctx, fed, h)
	if err != nil {
		return core.DataQueueCounts{}, fmt.Errorf("ready data queue: %w", err)
	}
	if ^uint64(0)-counts.RO < committed.RO || ^uint64(0)-counts.TSO < committed.TSO {
		return core.DataQueueCounts{}, fmt.Errorf("%w: data count overflow", core.ErrDataQueueCoverageUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return core.DataQueueCounts{}, err
	}
	counts.RO += committed.RO
	counts.TSO += committed.TSO
	return counts, nil
}

func countWaitingDataMessage(counts *core.DataQueueCounts, event core.OutboundEvent) error {
	message, marked := event.(core.DataQueueMessage)
	if !marked {
		return nil
	}
	order, data := message.DataReceivedOrder()
	if !data {
		return nil
	}
	switch order {
	case core.OrderReceive:
		counts.RO++
	case core.OrderTimestamp:
		counts.TSO++
	default:
		return fmt.Errorf("%w: waiting data has undefined received order", core.ErrDataQueueCoverageUnavailable)
	}
	return nil
}
