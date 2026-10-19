package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type dataReportBatcher interface {
	tryDataReportPrefix(context.Context, []*rtiv1.ReportCallbackInvocationRequest) int
}

func (s *supportService) tryDataReportPrefix(ctx context.Context, reports []*rtiv1.ReportCallbackInvocationRequest) int {
	if s.callbackReceipts == nil {
		return 0
	}
	return s.callbackReceipts.tryDataReportPrefix(ctx, reports)
}

func (s *routedSupportService) tryDataReportPrefix(ctx context.Context, reports []*rtiv1.ReportCallbackInvocationRequest) int {
	if len(reports) == 0 {
		return 0
	}
	backend, err := routeUnary(ctx, s.router, reports[0], s.floats, s.integers)
	if err != nil {
		return 0
	}
	if batcher, ok := backend.(dataReportBatcher); ok {
		return batcher.tryDataReportPrefix(ctx, reports)
	}
	return 0
}

// The unchanged single-report path handles the first ineligible report. This
// preserves successful-prefix ACKs and original exception precedence.
func (r *CallbackReceipts) tryDataReportPrefix(ctx context.Context, reports []*rtiv1.ReportCallbackInvocationRequest) int {
	port := r.opts.DataReports
	if !r.dataReportBatchEnabled(len(reports)) {
		return 0
	}
	first := reports[0]
	if !successfulDataReport(first) || first.FederationName == "" || first.FederateHandle == 0 {
		return 0
	}
	fed, h := core.FederationName(first.FederationName), core.FederateHandle(first.FederateHandle)
	release, err := port.Acquire(ctx, fed, h)
	if err != nil || release == nil {
		return 0
	}
	defer release()
	r.publicationMu.Lock()
	r.mu.Lock()
	r.publicationMu.Unlock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return 0
	}

	var actions [callbackReportBatchLimit]dataReportAction
	var observations [callbackReportBatchLimit]*rtiv1.FederateEvent
	count, changes := r.planDataReportPrefixLocked(fed, h, reports, actions[:], observations[:])
	if count == 0 || !port.Apply(fed, h, observations[:changes]) {
		return 0
	}
	for _, change := range actions[:changes] {
		entry := change.receipt
		if change.entry {
			r.publishCheckpointLocked(entry)
			entry.entryApplied = true
		} else {
			entry.reported, entry.success, entry.exception = true, true, ""
			r.completeLocked(change.key, entry)
			entry.event = nil
		}
	}
	return count
}

func successfulDataReport(report *rtiv1.ReportCallbackInvocationRequest) bool {
	return report != nil && validateWireVersion(report.WireVersion) == nil && report.Success && report.Exception == "" && len(report.CallbackReceipt) == 32
}

func ordinaryROData(event *rtiv1.FederateEvent) bool {
	if event == nil || event.CallbackRecovery != nil || event.OwnershipReceipt != nil {
		return false
	}
	var metadata *rtiv1.ObjectCallbackMetadata
	if data := event.GetReflect(); data != nil {
		if data.LogicalTime != nil || data.LogicalTimeValue != nil || data.ObjectHandle == 0 || data.ObjectClassHandle == 0 {
			return false
		}
		metadata = data.Metadata
	} else if data := event.GetReceive(); data != nil {
		if data.LogicalTime != nil || data.LogicalTimeValue != nil || data.InteractionClassHandle == 0 {
			return false
		}
		metadata = data.Metadata
	} else {
		return false
	}
	return ordinaryRODataMetadata(metadata)
}

func (r *CallbackReceipts) dataReportBatchEnabled(count int) bool {
	port := r.opts.DataReports
	return r.opts.LocalDataEntry && port != nil && port.Acquire != nil && port.Apply != nil && count > 0 && count <= callbackReportBatchLimit
}

type dataReportAction struct {
	key     string
	receipt *callbackReceipt
	entry   bool
}
type dataReportState struct{ entered, completed bool }

func (r *CallbackReceipts) planDataReportPrefixLocked(fed core.FederationName, h core.FederateHandle, reports []*rtiv1.ReportCallbackInvocationRequest, actions []dataReportAction, observations []*rtiv1.FederateEvent) (int, int) {
	planned := make(map[*callbackReceipt]dataReportState)
	count, changes := 0, 0
	for _, report := range reports {
		if !successfulDataReport(report) || report.FederationName != string(fed) || report.FederateHandle != uint64(h) {
			break
		}
		key := string(report.CallbackReceipt)
		entry := r.entries[key]
		if !eligibleDataReportReceipt(entry, fed, h) {
			break
		}
		current, exists := planned[entry]
		if !exists {
			current.entered = entry.entryApplied
		}
		// Completion may evict its own ticket when recovery markers pin the
		// retention window. Resolve a later retry against the committed ledger.
		if current.completed {
			break
		}
		if report.InvocationEntry {
			if !current.entered {
				actions[changes] = dataReportAction{key, entry, true}
				observations[changes] = entry.event
				changes++
				current.entered = true
			}
		} else {
			if !current.entered {
				break
			}
			if !current.completed {
				actions[changes] = dataReportAction{key, entry, false}
				changes++
				current.completed = true
			}
		}
		planned[entry] = current
		count++
	}
	return count, changes
}

func eligibleDataReportReceipt(entry *callbackReceipt, fed core.FederationName, h core.FederateHandle) bool {
	return entry != nil && entry.fed == fed && entry.h == h && entry.required && entry.admitted && !entry.applied &&
		entry.entryWorking == nil && entry.outcomeWorking == nil && ordinaryROData(entry.event) &&
		(!entry.reported || (entry.success && entry.exception == ""))
}

func ordinaryRODataMetadata(metadata *rtiv1.ObjectCallbackMetadata) bool {
	return metadata != nil && metadata.GetSentOrder() == rtiv1.OrderType_ORDER_TYPE_RECEIVE &&
		metadata.GetReceivedOrder() == rtiv1.OrderType_ORDER_TYPE_RECEIVE && metadata.MessageRetractionHandle == nil &&
		metadata.ReceivedRegions == nil && (metadata.GetTransportationType() == rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE ||
		metadata.GetTransportationType() == rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT)
}
