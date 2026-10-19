package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type ownershipReceiptKey struct{ generation, epoch, id uint64 }

func receiptKey(r *rtiv1.OwnershipCallbackReceipt) ownershipReceiptKey {
	return ownershipReceiptKey{r.FederationGeneration, r.CallbackEpoch, r.CallbackId}
}

// EnableOwnershipCallbackReceipts explicitly negotiates mandatory invocation
// acknowledgments. Merely receiving an Events value never acknowledges it.
func (f *Federate) EnableOwnershipCallbackReceipts(ctx context.Context) error {
	_, err := rtiv1.NewOwnershipCallbackServiceClient(f.conn.rpcConnection()).EnableOwnershipCallbackReceipts(f.sessionContext(ctx), &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), FederationGeneration: f.federationGeneration, CallbackEpoch: f.CallbackEpoch()})
	if err == nil {
		f.ownershipCallbackMu.Lock()
		f.ownershipReceiptsEnabled = true
		f.ownershipCallbackMu.Unlock()
	}
	return wrapStatusErr(err)
}

func (f *Federate) RetryOwnershipCallbacks(ctx context.Context) error {
	if err := f.FlushOwnershipCallbackReceipts(ctx); err != nil {
		return err
	}
	_, err := rtiv1.NewOwnershipCallbackServiceClient(f.conn.rpcConnection()).RetryOwnershipCallbacks(f.sessionContext(ctx), &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), FederationGeneration: f.federationGeneration, CallbackEpoch: f.CallbackEpoch()})
	return wrapStatusErr(err)
}

// ReportOwnershipCallbackInvocation asserts an actual application invocation
// for callers that dispatch Events manually rather than through DispatchEvent.
func (f *Federate) ReportOwnershipCallbackInvocation(ctx context.Context, event Event) error {
	f.checkpointMu.Lock()
	err := f.validateRuntimeEventLocked(event)
	f.checkpointMu.Unlock()
	if err != nil {
		return err
	}
	info, ok := event.(interface {
		callbackOwnershipReceipt() *rtiv1.OwnershipCallbackReceipt
	})
	if !ok || info.callbackOwnershipReceipt() == nil {
		return ErrCallbackReceiptUnavailable
	}
	_, err = f.beginOwnershipInvocation(ctx, event)
	if err != nil {
		return err
	}
	err = f.FlushOwnershipCallbackReceipts(ctx)
	if err == nil {
		f.checkpointMu.Lock()
		f.retireRuntimeEventLocked(event)
		f.checkpointMu.Unlock()
	}
	return err
}

// FlushOwnershipCallbackReceipts retries acknowledgments, never application code.
func (f *Federate) FlushOwnershipCallbackReceipts(ctx context.Context) error {
	f.ownershipCallbackMu.Lock()
	pending := make(map[ownershipReceiptKey]*rtiv1.OwnershipCallbackReceipt, len(f.ownershipPending))
	for key, value := range f.ownershipPending {
		pending[key] = value
	}
	f.ownershipCallbackMu.Unlock()
	for key, receipt := range pending {
		if f.conn == nil || f.conn.cc == nil {
			return ErrNotJoined
		}
		_, err := rtiv1.NewOwnershipCallbackServiceClient(f.conn.rpcConnection()).ReportOwnershipCallbackInvocation(f.sessionContext(ctx), &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), Receipt: receipt})
		if err != nil {
			return wrapStatusErr(err)
		}
		f.ownershipCallbackMu.Lock()
		if f.ownershipPending[key] == receipt {
			delete(f.ownershipPending, key)
		}
		f.ownershipCallbackMu.Unlock()
	}
	return nil
}

func callbackObject(event Event) (uint64, bool) {
	switch e := event.(type) {
	case RequestAttributeOwnershipAssumption:
		return e.ObjectHandle, true
	case DiscoverObjectInstance:
		return e.ObjectHandle, true
	case AttributeOwnershipAcquisitionNotification:
		return e.ObjectHandle, false
	case AttributeOwnershipUnavailable:
		return e.ObjectHandle, false
	case ConfirmAttributeOwnershipAcquisitionCancellation:
		return e.ObjectHandle, false
	default:
		return 0, false
	}
}

func (f *Federate) beginOwnershipInvocation(ctx context.Context, event Event) (bool, error) {
	object, advice := callbackObject(event)
	metadata, ok := event.(interface {
		ObjectKnowledgeEpoch() uint64
		callbackOwnershipReceipt() *rtiv1.OwnershipCallbackReceipt
	})
	for {
		f.ownershipCallbackMu.Lock()
		if wait := f.callbackDeletion[object]; object != 0 && wait != nil {
			f.ownershipCallbackMu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
		if ok && advice && metadata.ObjectKnowledgeEpoch() != 0 {
			epoch := metadata.ObjectKnowledgeEpoch()
			if f.staleCallbackKnowledgeLocked(object, epoch) {
				f.ownershipCallbackMu.Unlock()
				return false, nil
			}
		}
		if !ok || metadata.callbackOwnershipReceipt() == nil {
			f.ownershipCallbackMu.Unlock()
			return true, nil
		}
		receipt := metadata.callbackOwnershipReceipt()
		if err := f.validateOwnershipReceiptLocked(event, receipt); err != nil {
			f.ownershipCallbackMu.Unlock()
			return false, err
		}
		key := receiptKey(receipt)
		if f.ownershipPending == nil {
			f.ownershipPending = make(map[ownershipReceiptKey]*rtiv1.OwnershipCallbackReceipt)
		}
		f.ownershipPending[key] = receipt
		if f.ownershipInvoked == nil {
			f.ownershipInvoked = make(map[ownershipReceiptKey]bool)
		}
		invoke := !f.ownershipInvoked[key]
		f.ownershipInvoked[key] = true
		f.ownershipCallbackMu.Unlock()
		f.applyOwnershipInvocation(event, invoke)
		return invoke, nil
	}
}

func (f *Federate) beginCallbackDeletion(ctx context.Context, object uint64) (func(), error) {
	for {
		f.ownershipCallbackMu.Lock()
		if wait := f.callbackDeletion[object]; wait != nil {
			f.ownershipCallbackMu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if f.callbackDeletion == nil {
			f.callbackDeletion = make(map[uint64]chan struct{})
		}
		done := make(chan struct{})
		f.callbackDeletion[object] = done
		f.ownershipCallbackMu.Unlock()
		return func() {
			f.ownershipCallbackMu.Lock()
			delete(f.callbackDeletion, object)
			close(done)
			f.ownershipCallbackMu.Unlock()
		}, nil
	}
}

func (f *Federate) validateOwnershipReceiptLocked(event Event, receipt *rtiv1.OwnershipCallbackReceipt) error {
	switch event.(type) {
	case AttributeOwnershipAcquisitionNotification, AttributeOwnershipUnavailable, ConfirmAttributeOwnershipAcquisitionCancellation:
	default:
		return fmt.Errorf("federate: receipt attached to a nonterminal ownership callback")
	}
	if !f.ownershipReceiptsEnabled || receipt.FederationGeneration != f.federationGeneration || receipt.CallbackEpoch != f.CallbackEpoch() || receipt.CallbackId == 0 || receipt.DeliveryAttempt == 0 {
		return fmt.Errorf("federate: stale or invalid ownership callback receipt")
	}
	return nil
}

func (f *Federate) applyOwnershipInvocation(event Event, invoke bool) {
	if acquired, ok := event.(AttributeOwnershipAcquisitionNotification); invoke && ok && acquired.OwningFederate == f.CurrentHandle() {
		f.updateOwnedAttributes(acquired.ObjectHandle, acquired.AttributeHandles, true)
	}
}

func (f *Federate) staleCallbackKnowledgeLocked(object, epoch uint64) bool {
	return epoch <= f.retiredKnowledge[object] || epoch < f.callbackKnowledge[object]
}
