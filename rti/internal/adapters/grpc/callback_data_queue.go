package grpc

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func dataCallbackOrder(event *rtiv1.FederateEvent) (core.OrderType, bool) {
	var metadata *rtiv1.ObjectCallbackMetadata
	switch {
	case event.GetReflect() != nil:
		metadata = event.GetReflect().GetMetadata()
	case event.GetReceive() != nil:
		metadata = event.GetReceive().GetMetadata()
	case event.GetRemove() != nil:
		metadata = event.GetRemove().GetMetadata()
	default:
		return core.OrderUnspecified, false
	}
	return protoOrderTypeToCore(metadata.GetReceivedOrder()), true
}

// IsDataCallback identifies data, not grants or other control callbacks.
func IsDataCallback(event core.OutboundEvent) bool {
	wire, err := toDispatchFederateEvent(event)
	if err != nil || wire == nil {
		return false
	}
	_, data := dataCallbackOrder(wire)
	return data
}

// Discovery is retained with data so an unentered reflection cannot outlive
// the discover callback required by the same recipient's object knowledge.
func IsCheckpointCallback(event core.OutboundEvent) bool {
	wire, err := toDispatchFederateEvent(event)
	if err != nil || wire == nil {
		return false
	}
	return checkpointReportable(wire)
}

func checkpointReportable(event *rtiv1.FederateEvent) bool {
	return callbackServiceName(event) != ""
}

// PendingDataCounts is called under the time evaluator's observation lease.
// Each admitted opaque receipt represents one delivery, even for equal payloads.
func (r *CallbackReceipts) PendingDataCounts(ctx context.Context, fed core.FederationName, h core.FederateHandle) (core.DataQueueCounts, error) {
	if err := ctx.Err(); err != nil {
		return core.DataQueueCounts{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var counts core.DataQueueCounts
	for _, entry := range r.entries {
		if entry.fed != fed || entry.h != h || !entry.required || !entry.admitted || entry.entryApplied || entry.applied {
			continue
		}
		order, data := dataCallbackOrder(entry.event)
		if !data {
			continue
		}
		switch order {
		case core.OrderReceive:
			counts.RO++
		case core.OrderTimestamp:
			counts.TSO++
		default:
			return core.DataQueueCounts{}, fmt.Errorf("MOM: pending data has no established received order")
		}
	}
	return counts, nil
}
