package grpc

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type receiptEventsStream struct {
	rtiv1.StreamService_EventsServer
	receipts *CallbackReceipts
	fed      core.FederationName
	handle   core.FederateHandle
}

func (s *receiptEventsStream) Send(event *rtiv1.FederateEvent) error {
	if restored := event.GetRestoreCompleted(); event.GetCallbackRecovery() == nil && restored != nil && restored.GetFederateHandle() != 0 {
		s.handle = core.FederateHandle(restored.GetFederateHandle())
	}
	if event.GetCallbackRecovery().GetCompleted() {
		commit, err := s.receipts.completedRecoveryDelivery(s.fed, s.handle, event)
		if err != nil {
			return err
		}
		err = s.StreamService_EventsServer.Send(event)
		commit(err == nil)
		return err
	}
	if len(event.GetCallbackReceipt()) != 0 {
		commit, skip, err := s.receipts.preparedDelivery(s.fed, s.handle, event)
		if err != nil || skip {
			return err
		}
		err = s.StreamService_EventsServer.Send(event)
		commit(err == nil)
		return err
	}
	receipt, commit, err := s.receipts.Prepare(s.Context(), s.fed, s.handle, event)
	if err != nil {
		return err
	}
	event.CallbackReceipt = receipt
	err = s.StreamService_EventsServer.Send(event)
	if commit != nil {
		commit(err == nil)
	}
	return err
}

type receiptBatchesStream struct {
	rtiv1.StreamService_EventBatchesServer
	receipts *CallbackReceipts
	fed      core.FederationName
	handle   core.FederateHandle
}

func (s *receiptBatchesStream) Send(batch *rtiv1.FederateEventBatch) error {
	if prepared, handled, err := s.receipts.prepareDataDeliveryBatch(s.fed, s.handle, batch.Events); handled {
		if err != nil {
			prepared.commit(false)
			return err
		}
		batch.Events = prepared.events
		if len(batch.Events) == 0 && !batch.GetReady() {
			return nil
		}
		err = s.StreamService_EventBatchesServer.Send(batch)
		prepared.commit(err == nil)
		return err
	}
	commits := make([]func(bool), 0, len(batch.Events))
	kept := batch.Events[:0]
	for _, event := range batch.Events {
		if restored := event.GetRestoreCompleted(); event.GetCallbackRecovery() == nil && restored != nil && restored.GetFederateHandle() != 0 {
			s.handle = core.FederateHandle(restored.GetFederateHandle())
		}
		receipt, commit, skip, err := s.prepareEventReceipt(event)
		if skip {
			continue
		}
		if err != nil {
			commitCallbackBatch(commits, false)
			return err
		}
		event.CallbackReceipt = receipt
		kept = append(kept, event)
		commits = append(commits, commit)
	}
	batch.Events = kept
	if len(kept) == 0 && !batch.GetReady() {
		return nil
	}
	err := s.StreamService_EventBatchesServer.Send(batch)
	commitCallbackBatch(commits, err == nil)
	return err
}
func (s *receiptBatchesStream) prepareEventReceipt(event *rtiv1.FederateEvent) ([]byte, func(bool), bool, error) {
	var receipt []byte
	var commit func(bool)
	var err error
	switch {
	case event.GetCallbackRecovery().GetCompleted():
		commit, err = s.receipts.completedRecoveryDelivery(s.fed, s.handle, event)
	case len(event.GetCallbackReceipt()) != 0:
		var skip bool
		commit, skip, err = s.receipts.preparedDelivery(s.fed, s.handle, event)
		if err == nil && skip {
			return nil, nil, true, nil
		}
		receipt = event.CallbackReceipt
	default:
		receipt, commit, err = s.receipts.Prepare(s.Context(), s.fed, s.handle, event)
	}
	return receipt, commit, false, err
}
func commitCallbackBatch(commits []func(bool), sent bool) {
	for _, done := range commits {
		if done != nil {
			done(sent)
		}
	}
}
