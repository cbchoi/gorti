package federate

import (
	"context"
	"sort"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

// Eligibility comes from the detached server envelope, never mutable fields in
// the user-facing event. Recovery, DDM, TSO and lifecycle callbacks stay confirmed.
func localEntryWireEligible(wire *rtiv1.FederateEvent) bool {
	if wire == nil || len(wire.CallbackReceipt) != 32 || wire.CallbackRecovery != nil || wire.OwnershipReceipt != nil {
		return false
	}
	var metadata *rtiv1.ObjectCallbackMetadata
	switch {
	case wire.GetReflect() != nil:
		data := wire.GetReflect()
		if data.LogicalTime != nil || data.LogicalTimeValue != nil {
			return false
		}
		metadata = data.Metadata
	case wire.GetReceive() != nil:
		data := wire.GetReceive()
		if data.LogicalTime != nil || data.LogicalTimeValue != nil {
			return false
		}
		metadata = data.Metadata
	default:
		return false
	}
	return localEntryMetadataEligible(metadata)
}

func (f *Federate) admitLocalCallbackEntry(ctx context.Context, event Event, entry *genericCallbackInvocation) (bool, error) {
	info, tracked := runtimeInfo(event)
	if !f.localEntrySessionEligible(info, tracked) {
		return false, nil
	}
	select {
	case <-f.drainDone:
		// Buffered events may outlive a lost callback stream. Revalidate them
		// remotely rather than carrying a local admission across disconnection.
		return false, nil
	default:
	}
	q := f.callbackReportTransport()
	q.mu.Lock()
	allowed := q.localEntry && !q.unsupported
	q.mu.Unlock()
	if !allowed {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := q.reserveCompletion(ctx); err != nil {
		return false, err
	}
	accepted := false
	defer func() {
		if !accepted {
			q.releaseCompletion()
		}
	}()
	f.checkpointMu.Lock()
	defer f.checkpointMu.Unlock()
	if err := f.validateRuntimeEventLocked(event); err != nil {
		return false, err
	}
	request := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.federationName,
		FederateHandle: info.federate, CallbackReceipt: info.CallbackReceipt(), Success: true, InvocationEntry: true}
	_, err := q.enqueueReserved(ctx, request, func(err error) {
		if err == nil {
			f.genericCallbackMu.Lock()
			entry.entryReported = true
			entry.releaseCompletedPayload()
			f.genericCallbackMu.Unlock()
		}
	})
	if err != nil {
		return false, err
	}
	accepted = true
	f.genericCallbackMu.Lock()
	entry.localEntry, entry.entered = true, true
	entry.entrySequence = f.callbackLocalEntries.Add(1)
	f.genericCallbackMu.Unlock()
	f.retireRuntimeEventLocked(event)
	return true, nil
}

// Drain accepted tasks before retrying failed entry reports in invocation order.
// Completion reports are flushed afterwards by FlushCallbackInvocationReports.
// An executed body is never rerun to recover an acknowledgement.
func (f *Federate) flushLocalCallbackEntries(ctx context.Context) error {
	if !callbackprofile.Async {
		return nil
	}
	f.callbackReportMu.Lock()
	q := f.callbackReports
	f.callbackReportMu.Unlock()
	if q == nil {
		return nil
	}
	q.mu.Lock()
	pending := append([]*callbackReportTask(nil), q.queue...)
	for task := range q.completing {
		pending = append(pending, task)
	}
	q.mu.Unlock()
	for _, task := range pending {
		select {
		case <-task.done.wait():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.genericCallbackMu.Lock()
	var retry []genericCallbackInvocation
	for _, entry := range f.genericCallbacks {
		if entry.localEntry && entry.complete && !entry.entryReported {
			retry = append(retry, *entry)
		}
	}
	f.genericCallbackMu.Unlock()
	sort.Slice(retry, func(i, j int) bool { return retry[i].entrySequence < retry[j].entrySequence })
	for _, entry := range retry {
		if err := f.ReportCallbackEntry(ctx, entry.event); err != nil {
			return err
		}
	}
	return nil
}

func localEntryMetadataEligible(metadata *rtiv1.ObjectCallbackMetadata) bool {
	return metadata != nil && metadata.MessageRetractionHandle == nil && len(metadata.GetReceivedRegions().GetRegionHandles()) == 0 &&
		metadata.SentOrder != nil && *metadata.SentOrder == rtiv1.OrderType_ORDER_TYPE_RECEIVE &&
		metadata.ReceivedOrder != nil && *metadata.ReceivedOrder == rtiv1.OrderType_ORDER_TYPE_RECEIVE
}

func (f *Federate) localEntrySessionEligible(info CallbackInfo, tracked bool) bool {
	if !callbackprofile.Async || !tracked || !info.localEntry || info.runtimeID == 0 ||
		len(f.sessionToken) == 0 || info.federate != f.CurrentHandle() {
		return false
	}
	return true
}
