package federate

import (
	"context"
	"errors"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

// CallbackInfo retains a server-issued receipt without interpreting its bytes.
// Delivery on Events does not mean the application's callback was invoked.
type CallbackInfo struct {
	receipt        []byte
	federate       uint64
	ownership      *rtiv1.OwnershipCallbackReceipt
	knowledgeEpoch uint64
	runtimeOwner   *Federate
	runtimeEpoch   uint64
	runtimeID      uint64
	identity       string
	outcomeOnly    bool
	completed      bool
	recovered      bool
	localEntry     bool
}

func (i CallbackInfo) runtimeCallbackInfo() CallbackInfo { return i }

func (i CallbackInfo) CallbackReceipt() []byte                                   { return append([]byte(nil), i.receipt...) }
func (i CallbackInfo) callbackReportingHandle() uint64                           { return i.federate }
func (i CallbackInfo) callbackOwnershipReceipt() *rtiv1.OwnershipCallbackReceipt { return i.ownership }
func (i CallbackInfo) ObjectKnowledgeEpoch() uint64                              { return i.knowledgeEpoch }

var ErrCallbackReceiptUnavailable = errors.New("federate: callback invocation receipt unavailable")

// ReportCallbackInvocation is called after the application actually invokes its
// callback. It must not be used to report queued, ignored, or disabled events.
// Reporting failure never authorizes replay of the application callback.
// Failed callbacks require a nonempty exception detail; successful ones require none.
func (f *Federate) ReportCallbackInvocation(ctx context.Context, event Event, success bool, exception string) error {
	if f != nil {
		f.checkpointMu.Lock()
		err := f.validateRuntimeEventLocked(event)
		f.checkpointMu.Unlock()
		if err != nil {
			return err
		}
	}
	receipt, handle, err := callbackReportReceipt(event, success, exception)
	if err != nil {
		return err
	}
	if f == nil || f.conn == nil || f.conn.cc == nil {
		return ErrNotJoined
	}
	if err := f.validateRecoveredResult(event, success, exception); err != nil {
		return err
	}
	f.checkpointMu.Lock()
	if err := f.validateRuntimeEventLocked(event); err != nil {
		f.checkpointMu.Unlock()
		return err
	}
	f.genericCallbackMu.Lock()
	previous := f.genericCallbacks[genericReceipt(event)]
	conflict := previous != nil && previous.complete && (previous.success != success || previous.exception != exception)
	f.genericCallbackMu.Unlock()
	if conflict {
		f.checkpointMu.Unlock()
		return errors.New("federate: callback outcome conflicts with the known finished result")
	}
	f.retainCallbackOutcome(event, success, exception, false)
	f.checkpointMu.Unlock()
	err = f.sendCallbackReport(ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: handle, CallbackReceipt: receipt, Success: success, Exception: exception})
	if err == nil {
		return f.finishReportedCallback(event, success, exception)
	}
	return wrapStatusErr(err)
}

// DispatchEvent invokes an admitted callback at most once per tracked receipt.
// The two errors distinguish application failure from admission/report failure.
// A rejected entry does not invoke the callback. Retain that event and retry it
// after the reported condition clears, continuing to dispatch checkpoint controls
// so that a save or restore can finish. Retained outcomes retry reporting only.
// An error with an empty message is reported using its Go type name.
// Panics retain normal Go propagation; use an error-returning callback when
// failures should be reported to the server.
// With callback_async and negotiated local admission, ordinary nonregional RO
// data enters locally; entry and outcome reports retain their wire order. Other
// callbacks and servers without that capability use confirmed entry. Call
// FlushCallbackInvocationReports to establish the remote completion boundary;
// background report failures remain retryable and surface on a subsequent call.
func (f *Federate) DispatchEvent(ctx context.Context, event Event, callback func(Event) error) (callbackErr, reportErr error) {
	if event == nil || callback == nil {
		return fmt.Errorf("federate: event and callback required"), nil
	}
	f.checkpointMu.Lock()
	if err := f.validateRuntimeEventLocked(event); err != nil {
		f.checkpointMu.Unlock()
		return nil, err
	}
	if f.runtimeEventConsumedLocked(event) {
		f.checkpointMu.Unlock()
		return nil, errors.Join(f.FlushOwnershipCallbackReceipts(ctx), f.FlushCallbackInvocationReports(ctx))
	}
	if info, _ := runtimeInfo(event); info.outcomeOnly {
		f.checkpointMu.Unlock()
		outcome, err := f.recoveredOutcome(event)
		if err != nil {
			return nil, err
		}
		return nil, f.ReportCallbackInvocation(ctx, event, outcome.Success, outcome.Exception)
	}
	active := true
	tracked, _ := runtimeInfo(event)
	f.trackRuntimeAdmissionLocked(tracked)
	f.runtimeActive++
	f.checkpointMu.Unlock()
	defer func() {
		f.checkpointMu.Lock()
		if active {
			f.runtimeActive--
		}
		delete(f.runtimeAdmission, tracked.runtimeID)
		f.checkpointMu.Unlock()
	}()
	finish := func(success bool, detail string) {
		f.checkpointMu.Lock()
		f.retainCallbackOutcome(event, success, detail, false)
		f.runtimeActive--
		active = false
		f.checkpointMu.Unlock()
	}
	invoke, err := f.beginOwnershipInvocation(ctx, event)
	if err != nil {
		return nil, err
	}
	if !invoke {
		return nil, f.FlushOwnershipCallbackReceipts(ctx)
	}
	admitted := event
	invoke, err = f.beginGenericCallback(ctx, event, func() error {
		var admissionErr error
		admitted, admissionErr = f.activateTransportationCallback(ctx, event)
		return admissionErr
	})
	if err != nil {
		f.rollbackOwnershipEntry(event)
		return nil, err
	}
	if !invoke {
		return nil, nil
	}
	f.checkpointMu.Lock()
	f.retireRuntimeEventLocked(event)
	f.checkpointMu.Unlock()
	defer func() {
		mandatory := f.FlushOwnershipCallbackReceipts(ctx)
		if mandatory != nil {
			reportErr = errors.Join(mandatory, reportErr)
		}
		if recovered := recover(); recovered != nil {
			finish(false, fmt.Sprintf("panic: %v", recovered))
			_ = f.FlushCallbackInvocationReports(ctx)
			panic(recovered)
		}
	}()
	callbackErr = callback(admitted)
	detail := callbackExceptionDetail(callbackErr)
	finish(callbackErr == nil, detail)
	reportErr = f.reportDispatchedOutcome(ctx, event, callbackErr == nil, detail)
	return
}

func (f *Federate) callbackInfo(evt *rtiv1.FederateEvent) CallbackInfo {
	h := f.CurrentHandle()
	if restored := evt.GetRestoreCompleted(); restored != nil && restored.FederateHandle != nil && evt.GetCallbackRecovery() == nil {
		h = *restored.FederateHandle
	}
	var ownership *rtiv1.OwnershipCallbackReceipt
	if value := evt.GetOwnershipReceipt(); value != nil {
		ownership = &rtiv1.OwnershipCallbackReceipt{FederationGeneration: value.FederationGeneration, CallbackEpoch: value.CallbackEpoch, CallbackId: value.CallbackId, DeliveryAttempt: value.DeliveryAttempt}
	}
	if d := evt.GetDiscover(); d != nil && evt.GetObjectKnowledgeEpoch() != 0 {
		f.ownershipCallbackMu.Lock()
		if f.callbackKnowledge == nil {
			f.callbackKnowledge = make(map[uint64]uint64)
		}
		f.callbackKnowledge[d.ObjectHandle] = max(f.callbackKnowledge[d.ObjectHandle], evt.GetObjectKnowledgeEpoch())
		f.ownershipCallbackMu.Unlock()
	}
	info, _ := callbackRecoveryInfo(evt)
	info.receipt, info.federate, info.ownership, info.knowledgeEpoch = append([]byte(nil), evt.GetCallbackReceipt()...), h, ownership, evt.GetObjectKnowledgeEpoch()
	return info
}

func attachCallbackInfo(event Event, info CallbackInfo) Event {
	switch event.(type) {
	case RequestAttributeOwnershipAssumption, AttributeOwnershipAcquisitionNotification, RequestDivestitureConfirmation, RequestAttributeOwnershipRelease, AttributeOwnershipUnavailable, ConfirmAttributeOwnershipAcquisitionCancellation:
		return attachOwnershipCallbackInfo(event, info)
	case InitiateFederateSave, FederationSaved, FederationNotSaved, InitiateFederateRestore, FederationNotRestored, RequestFederationRestoreSucceeded, RequestFederationRestoreFailed, FederationRestoreBegun, FederationRestored, FederationHalted:
		return attachLifecycleCallbackInfo(event, info)
	case StartRegistrationForObjectClass, StopRegistrationForObjectClass, TurnInteractionsOn, TurnInteractionsOff, AttributesInScope, AttributesOutOfScope, TransportationChangeConfirmed, TurnUpdatesOnForObjectInstance, TurnUpdatesOffForObjectInstance:
		return attachDeclarationCallbackInfo(event, info)
	case DiscoverObjectInstance, ReflectAttributeValues, ReflectAttributeValuesByHandle, ReceiveInteraction, ReceiveInteractionByHandle, RemoveObjectInstance, ProvideAttributeValueUpdate, RequestRetraction:
		return attachDataCallbackInfo(event, info)
	case SynchronizationPointRegistrationSucceeded, SynchronizationPointRegistrationFailed, MultipleObjectInstanceNameReservationSucceeded, MultipleObjectInstanceNameReservationFailed, ObjectInstanceNameReservationSucceeded, ObjectInstanceNameReservationFailed, TimeAdvanceGrant, TimeRegulationEnabled, TimeConstrainedEnabled, SynchronizationPointAnnounced, FederationSynchronized:
		return attachNotificationCallbackInfo(event, info)
	default:
		return event
	}
}

func attachOwnershipCallbackInfo(event Event, info CallbackInfo) Event {
	switch e := event.(type) {
	case RequestAttributeOwnershipAssumption:
		e.CallbackInfo = info
		return e
	case AttributeOwnershipAcquisitionNotification:
		e.CallbackInfo = info
		return e
	case RequestDivestitureConfirmation:
		e.CallbackInfo = info
		return e
	case RequestAttributeOwnershipRelease:
		e.CallbackInfo = info
		return e
	case AttributeOwnershipUnavailable:
		e.CallbackInfo = info
		return e
	case ConfirmAttributeOwnershipAcquisitionCancellation:
		e.CallbackInfo = info
		return e
	default:
		return event
	}
}

func attachLifecycleCallbackInfo(event Event, info CallbackInfo) Event {
	switch e := event.(type) {
	case InitiateFederateSave:
		e.CallbackInfo = info
		return e
	case FederationSaved:
		e.CallbackInfo = info
		return e
	case FederationNotSaved:
		e.CallbackInfo = info
		return e
	case InitiateFederateRestore:
		e.CallbackInfo = info
		return e
	case FederationNotRestored:
		e.CallbackInfo = info
		return e
	case RequestFederationRestoreSucceeded:
		e.CallbackInfo = info
		return e
	case RequestFederationRestoreFailed:
		e.CallbackInfo = info
		return e
	case FederationRestoreBegun:
		e.CallbackInfo = info
		return e
	case FederationRestored:
		e.CallbackInfo = info
		return e
	case FederationHalted:
		e.CallbackInfo = info
		return e
	default:
		return event
	}
}

func attachDeclarationCallbackInfo(event Event, info CallbackInfo) Event {
	switch e := event.(type) {
	case StartRegistrationForObjectClass:
		e.CallbackInfo = info
		return e
	case StopRegistrationForObjectClass:
		e.CallbackInfo = info
		return e
	case TurnInteractionsOn:
		e.CallbackInfo = info
		return e
	case TurnInteractionsOff:
		e.CallbackInfo = info
		return e
	case AttributesInScope:
		e.CallbackInfo = info
		return e
	case AttributesOutOfScope:
		e.CallbackInfo = info
		return e
	case TransportationChangeConfirmed:
		e.CallbackInfo = info
		return e
	case TurnUpdatesOnForObjectInstance:
		e.CallbackInfo = info
		return e
	case TurnUpdatesOffForObjectInstance:
		e.CallbackInfo = info
		return e
	default:
		return event
	}
}

func attachDataCallbackInfo(event Event, info CallbackInfo) Event {
	switch e := event.(type) {
	case DiscoverObjectInstance:
		e.CallbackInfo = info
		return e
	case ReflectAttributeValues:
		e.CallbackInfo = info
		return e
	case ReflectAttributeValuesByHandle:
		e.CallbackInfo = info
		return e
	case ReceiveInteraction:
		e.CallbackInfo = info
		return e
	case ReceiveInteractionByHandle:
		e.CallbackInfo = info
		return e
	case RemoveObjectInstance:
		e.CallbackInfo = info
		return e
	case ProvideAttributeValueUpdate:
		e.CallbackInfo = info
		return e
	case RequestRetraction:
		e.CallbackInfo = info
		return e
	default:
		return event
	}
}

func attachNotificationCallbackInfo(event Event, info CallbackInfo) Event {
	switch e := event.(type) {
	case SynchronizationPointRegistrationSucceeded:
		e.CallbackInfo = info
		return e
	case SynchronizationPointRegistrationFailed:
		e.CallbackInfo = info
		return e
	case MultipleObjectInstanceNameReservationSucceeded:
		e.CallbackInfo = info
		return e
	case MultipleObjectInstanceNameReservationFailed:
		e.CallbackInfo = info
		return e
	case ObjectInstanceNameReservationSucceeded:
		e.CallbackInfo = info
		return e
	case ObjectInstanceNameReservationFailed:
		e.CallbackInfo = info
		return e
	case TimeAdvanceGrant:
		e.CallbackInfo = info
		return e
	case TimeRegulationEnabled:
		e.CallbackInfo = info
		return e
	case TimeConstrainedEnabled:
		e.CallbackInfo = info
		return e
	case SynchronizationPointAnnounced:
		e.CallbackInfo = info
		return e
	case FederationSynchronized:
		e.CallbackInfo = info
		return e
	default:
		return event
	}
}

func (f *Federate) validateRecoveredResult(event Event, success bool, exception string) error {
	if recovery, _ := runtimeInfo(event); recovery.outcomeOnly {
		outcome, err := f.recoveredOutcome(event)
		if err != nil {
			return err
		}
		if outcome.Success != success || outcome.Exception != exception {
			return errors.New("federate: recovery outcome does not match the saved application result")
		}
	}
	return nil
}

func (f *Federate) finishReportedCallback(event Event, success bool, exception string) error {
	f.checkpointMu.Lock()
	if stale := f.validateRuntimeEventLocked(event); stale != nil {
		f.checkpointMu.Unlock()
		return stale
	}
	f.genericCallbackMu.Lock()
	present := f.genericCallbacks[genericReceipt(event)] != nil
	f.genericCallbackMu.Unlock()
	if present {
		f.retainCallbackOutcome(event, success, exception, true)
	}
	f.retireRuntimeEventLocked(event)
	f.checkpointMu.Unlock()
	return nil
}

func callbackExceptionDetail(callbackErr error) string {
	detail := ""
	if callbackErr != nil {
		detail = callbackErr.Error()
		if detail == "" {
			detail = fmt.Sprintf("%T", callbackErr)
		}
	}
	return detail
}

func (f *Federate) reportDispatchedOutcome(ctx context.Context, event Event, success bool, detail string) error {
	if f.asyncCallbackOutcomeEnabled(event) {
		return f.enqueueCallbackOutcome(ctx, event, success, detail)
	}
	if genericReceipt(event) == "" {
		return f.ReportCallbackInvocation(ctx, event, success, detail)
	}
	return f.FlushCallbackInvocationReports(ctx)
}

func callbackReportReceipt(event Event, success bool, exception string) ([]byte, uint64, error) {
	info, ok := event.(interface {
		CallbackReceipt() []byte
		callbackReportingHandle() uint64
	})
	if !ok {
		return nil, 0, ErrCallbackReceiptUnavailable
	}
	receipt := info.CallbackReceipt()
	if len(receipt) == 0 {
		return nil, 0, ErrCallbackReceiptUnavailable
	}
	if success && exception != "" {
		return nil, 0, fmt.Errorf("federate: successful callback cannot carry an exception")
	}
	if !success && exception == "" {
		return nil, 0, fmt.Errorf("federate: failed callback requires an exception detail")
	}
	return receipt, info.callbackReportingHandle(), nil
}

func (f *Federate) trackRuntimeAdmissionLocked(tracked CallbackInfo) {
	if tracked.runtimeID != 0 {
		if f.runtimeAdmission == nil {
			f.runtimeAdmission = make(map[uint64]bool)
		}
		f.runtimeAdmission[tracked.runtimeID] = true
	}
}
