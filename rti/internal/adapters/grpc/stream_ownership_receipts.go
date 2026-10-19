package grpc

import (
	"errors"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/proto"
)

type ownershipReceiptEventsStream struct {
	rtiv1.StreamService_EventsServer
	receipts *OwnershipCallbackReceipts
	fed      core.FederationName
	handle   core.FederateHandle
}

func (s *ownershipReceiptEventsStream) Send(event *rtiv1.FederateEvent) error {
	if restored := event.GetRestoreCompleted(); event.GetCallbackRecovery() == nil && restored != nil && restored.GetFederateHandle() != 0 {
		s.handle = core.FederateHandle(restored.GetFederateHandle())
	}
	if event != nil {
		event = proto.Clone(event).(*rtiv1.FederateEvent)
	}
	finish, err := s.receipts.Prepare(s.Context(), s.fed, s.handle, event)
	if errors.Is(err, core.ErrOwnershipCallbackComplete) {
		return nil
	}
	if err != nil {
		return ownershipReceiptStatus(s.Context(), err)
	}
	err = s.StreamService_EventsServer.Send(event)
	if finish != nil {
		finish(err == nil)
	}
	return err
}

type ownershipReceiptBatchesStream struct {
	rtiv1.StreamService_EventBatchesServer
	receipts *OwnershipCallbackReceipts
	fed      core.FederationName
	handle   core.FederateHandle
}

func (s *ownershipReceiptBatchesStream) Send(batch *rtiv1.FederateEventBatch) error {
	cloned := proto.Clone(batch).(*rtiv1.FederateEventBatch)
	// Clone once to isolate receipts and payloads from the retained outbox.
	// Filter the cloned events in place; cloning each event again is redundant.
	kept := cloned.Events[:0]
	finishes := make([]func(bool), 0, len(batch.Events))
	prepared := make(map[core.OwnershipCallbackReceipt]struct{})
	for _, event := range cloned.Events {
		if restored := event.GetRestoreCompleted(); event.GetCallbackRecovery() == nil && restored != nil && restored.GetFederateHandle() != 0 {
			s.handle = core.FederateHandle(restored.GetFederateHandle())
		}
		if wire := event.GetOwnershipReceipt(); wire != nil {
			identity := core.OwnershipCallbackReceipt{FederationGeneration: wire.GetFederationGeneration(), CallbackEpoch: wire.GetCallbackEpoch(), CallbackID: wire.GetCallbackId()}
			if _, duplicate := prepared[identity]; duplicate {
				continue
			}
			prepared[identity] = struct{}{}
		}
		finish, err := s.receipts.Prepare(s.Context(), s.fed, s.handle, event)
		if errors.Is(err, core.ErrOwnershipCallbackComplete) {
			continue
		}
		if err != nil {
			for _, done := range finishes {
				if done != nil {
					done(false)
				}
			}
			return ownershipReceiptStatus(s.Context(), err)
		}
		kept = append(kept, event)
		finishes = append(finishes, finish)
	}
	clear(cloned.Events[len(kept):])
	cloned.Events = kept
	if len(cloned.Events) == 0 && !cloned.Ready {
		return nil
	}
	err := s.StreamService_EventBatchesServer.Send(cloned)
	for _, done := range finishes {
		if done != nil {
			done(err == nil)
		}
	}
	return err
}
