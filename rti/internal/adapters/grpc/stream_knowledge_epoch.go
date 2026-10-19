package grpc

import (
	"errors"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func toDispatchFederateEvent(event core.OutboundEvent) (*rtiv1.FederateEvent, error) {
	if required, ok := event.(*requiredCallbackEvent); ok && required.source != nil {
		if _, err := delivery.ResolveEvent(required.source); err != nil {
			if errors.Is(err, delivery.ErrObsoleteAdvisory) {
				if required.receipts != nil {
					required.receipts.discardObsolete(required.event.CallbackReceipt)
				}
				return nil, nil
			}
			return nil, err
		}
	}
	resolved, err := delivery.ResolveEvent(event)
	if errors.Is(err, delivery.ErrObsoleteAdvisory) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toFederateEvent(resolved)
}

// A preceding chunk's Send may block after the next event was converted. Check
// the retained envelopes again immediately before sending this chunk. This is
// a server dispatch boundary, not an SDK callback-invocation acknowledgement.
func sendKnowledgeCheckedBatch(batch *rtiv1.FederateEventBatch, events []core.OutboundEvent, stream rtiv1.StreamService_EventBatchesServer) error {
	kept := batch.Events[:0]
	for i, event := range events {
		required, _ := event.(*requiredCallbackEvent)
		if required != nil && required.source != nil {
			event = required.source
		}
		_, err := delivery.ResolveEvent(event)
		if errors.Is(err, delivery.ErrObsoleteAdvisory) {
			if required != nil && required.receipts != nil {
				required.receipts.discardObsolete(required.event.CallbackReceipt)
			}
			continue
		}
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		kept = append(kept, batch.Events[i])
	}
	batch.Events = kept
	if len(kept) == 0 {
		return nil
	}
	return stream.Send(batch)
}
