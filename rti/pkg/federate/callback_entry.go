package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

const genericCallbackLimit = 8192

func (f *Federate) CallbackInvocationEntrySupported() bool {
	return f != nil && f.callbackEntrySupported
}

type genericCallbackInvocation struct {
	event                       Event
	reportedReceipt             []byte
	entered, complete, reported bool
	success                     bool
	exception                   string
	localEntry                  bool
	entryReported               bool
	entrySequence               uint64
}

// Completed bodies are unnecessary for checkpointing or duplicate suppression.
// Keep only the receipt needed to distinguish a fresh recovery authorization.
func (entry *genericCallbackInvocation) releaseCompletedPayload() {
	if !entry.reported || entry.localEntry && !entry.entryReported || entry.event == nil {
		return
	}
	if carrier, ok := entry.event.(interface{ CallbackReceipt() []byte }); ok {
		entry.reportedReceipt = carrier.CallbackReceipt()
	}
	entry.event = nil
}

// ReportCallbackEntry authenticates invocation admission before entering user code.
func (f *Federate) ReportCallbackEntry(ctx context.Context, event Event) error {
	if info, _ := runtimeInfo(event); info.outcomeOnly {
		return fmt.Errorf("federate: outcome recovery cannot enter an application callback")
	}
	if f == nil || !f.callbackEntrySupported {
		return fmt.Errorf("federate: server did not advertise callback invocation entry support")
	}
	f.checkpointMu.Lock()
	err := f.validateRuntimeEventLocked(event)
	f.checkpointMu.Unlock()
	if err != nil {
		return err
	}
	info, ok := event.(interface {
		CallbackReceipt() []byte
		callbackReportingHandle() uint64
	})
	if !ok || len(info.CallbackReceipt()) == 0 {
		return ErrCallbackReceiptUnavailable
	}
	if f == nil || f.conn == nil || f.conn.cc == nil {
		return ErrNotJoined
	}
	err = f.sendCallbackReport(ctx, &rtiv1.ReportCallbackInvocationRequest{
		WireVersion: 1, FederationName: f.federationName, FederateHandle: info.callbackReportingHandle(),
		CallbackReceipt: info.CallbackReceipt(), Success: true, InvocationEntry: true,
	})
	if err == nil {
		f.checkpointMu.Lock()
		defer f.checkpointMu.Unlock()
		if err := f.validateRuntimeEventLocked(event); err != nil {
			return err
		}
		key := genericReceipt(event)
		f.genericCallbackMu.Lock()
		if f.genericCallbacks == nil {
			f.genericCallbacks = make(map[string]*genericCallbackInvocation)
		}
		if f.genericCallbacks[key] == nil {
			f.genericCallbacks[key] = &genericCallbackInvocation{event: event, entered: true}
		}
		f.genericCallbacks[key].entryReported = true
		f.genericCallbacks[key].releaseCompletedPayload()
		f.genericCallbackMu.Unlock()
		f.retireRuntimeEventLocked(event)
	}
	return wrapStatusErr(err)
}

func genericReceipt(event Event) string {
	if carrier, ok := event.(interface{ runtimeCallbackInfo() CallbackInfo }); ok {
		if info := carrier.runtimeCallbackInfo(); info.identity != "" {
			return info.identity
		}
	}
	if carrier, ok := event.(interface{ CallbackReceipt() []byte }); ok {
		return callbackIdentity(carrier.CallbackReceipt())
	}
	return ""
}

func (f *Federate) retainCallbackOutcome(event Event, success bool, exception string, reported bool) {
	key := genericReceipt(event)
	if key == "" {
		return
	}
	f.genericCallbackMu.Lock()
	defer f.genericCallbackMu.Unlock()
	if f.genericCallbacks == nil {
		f.genericCallbacks = make(map[string]*genericCallbackInvocation)
	}
	entry := f.genericCallbacks[key]
	if entry == nil {
		entry = &genericCallbackInvocation{event: event}
		f.genericCallbacks[key] = entry
	}
	entry.complete, entry.success, entry.exception = true, success, exception
	if reported && !entry.reported {
		entry.reported = true
		f.genericCallbackCompleted = append(f.genericCallbackCompleted, key)
		if len(f.genericCallbackCompleted) > genericCallbackLimit {
			delete(f.genericCallbacks, f.genericCallbackCompleted[0])
			f.genericCallbackCompleted = f.genericCallbackCompleted[1:]
		}
	}
	entry.releaseCompletedPayload()
}

func (f *Federate) beginGenericCallback(ctx context.Context, event Event, admit func() error) (bool, error) {
	key := genericReceipt(event)
	if key == "" {
		return true, admit()
	}
	f.genericCallbackMu.Lock()
	if previous := f.genericCallbacks[key]; previous != nil {
		f.genericCallbackMu.Unlock()
		return false, f.FlushCallbackInvocationReports(ctx)
	}
	if f.genericCallbacks == nil {
		f.genericCallbacks = make(map[string]*genericCallbackInvocation)
	}
	if len(f.genericCallbacks)-len(f.genericCallbackCompleted) >= genericCallbackLimit {
		f.genericCallbackMu.Unlock()
		return false, fmt.Errorf("federate: pending callback invocation capacity exceeded")
	}
	entry := &genericCallbackInvocation{event: event}
	f.genericCallbacks[key] = entry
	f.genericCallbackMu.Unlock()
	err := admit()
	if err == nil && f.callbackEntrySupported {
		var local bool
		local, err = f.admitLocalCallbackEntry(ctx, event, entry)
		if err == nil && !local {
			err = f.ReportCallbackEntry(ctx, event)
		}
	}
	if err != nil {
		f.rollbackGenericAdmission(event)
		return false, err
	}
	f.genericCallbackMu.Lock()
	entry.entered = true
	f.genericCallbackMu.Unlock()
	return true, nil
}

// FlushCallbackInvocationReports retries retained outcomes, never application code.
func (f *Federate) FlushCallbackInvocationReports(ctx context.Context) error {
	if err := f.flushLocalCallbackEntries(ctx); err != nil {
		return err
	}
	f.genericCallbackMu.Lock()
	var pending []*genericCallbackInvocation
	for _, entry := range f.genericCallbacks {
		if entry.complete && !entry.reported && entry.event != nil {
			entryCopy := *entry
			pending = append(pending, &entryCopy)
		}
	}
	f.genericCallbackMu.Unlock()
	for _, entry := range pending {
		if err := f.ReportCallbackInvocation(ctx, entry.event, entry.success, entry.exception); err != nil {
			return err
		}
		key := genericReceipt(entry.event)
		f.genericCallbackMu.Lock()
		if current := f.genericCallbacks[key]; current != nil && !current.reported {
			current.reported = true
			current.releaseCompletedPayload()
			f.genericCallbackCompleted = append(f.genericCallbackCompleted, key)
			if len(f.genericCallbackCompleted) > genericCallbackLimit {
				delete(f.genericCallbacks, f.genericCallbackCompleted[0])
				f.genericCallbackCompleted = f.genericCallbackCompleted[1:]
			}
		}
		f.genericCallbackMu.Unlock()
	}
	return nil
}

func (f *Federate) rollbackOwnershipEntry(event Event) {
	carrier, ok := event.(interface {
		callbackOwnershipReceipt() *rtiv1.OwnershipCallbackReceipt
	})
	if !ok || carrier.callbackOwnershipReceipt() == nil {
		return
	}
	key := receiptKey(carrier.callbackOwnershipReceipt())
	f.ownershipCallbackMu.Lock()
	delete(f.ownershipInvoked, key)
	delete(f.ownershipPending, key)
	f.ownershipCallbackMu.Unlock()
}
