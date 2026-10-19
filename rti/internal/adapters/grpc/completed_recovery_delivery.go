package grpc

import (
	"bytes"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *CallbackReceipts) pinnedCompletedOtherLocked(replacing core.FederationName) int {
	count := 0
	for _, entry := range r.entries {
		// A sent marker may become retry-pinned on disconnect while another
		// federation prepares replay, so reserve every retained marker's slot.
		if (entry.fed != replacing || entry.restorePrepared) && entry.applied && entry.event.GetCallbackRecovery().GetCompleted() {
			count++
		}
	}
	return count
}

// Completed markers have no capability or acknowledgement RPC. Retain them
// through failed sends/reconnect; repeated SDK cleanup is idempotent.
func (r *CallbackReceipts) completedRecoveryDelivery(fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) (func(bool), error) {
	recovery := event.GetCallbackRecovery()
	if err := validateCompletedRecoveryMarker(event); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, entry := range r.entries {
		if entry.fed != fed || entry.h != h || !entry.applied || !entry.required || !entry.admitted || !entry.event.GetCallbackRecovery().GetCompleted() || !bytes.Equal(entry.recoveryIdentity, recovery.InvocationIdentity) {
			continue
		}
		return func(sent bool) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.entries[key] == entry {
				entry.sent = sent
				entry.retry = !sent
			}
		}, nil
	}
	return nil, status.Error(codes.FailedPrecondition, "completed callback reconciliation marker unavailable")
}
func validateCompletedRecoveryMarker(event *rtiv1.FederateEvent) error {
	recovery := event.GetCallbackRecovery()
	if recovery == nil || !recovery.Completed || !recovery.OutcomeOnly || len(recovery.InvocationIdentity) != 32 || event.Event != nil || len(event.CallbackReceipt) != 0 || event.OwnershipReceipt != nil {
		return status.Error(codes.InvalidArgument, "invalid completed callback reconciliation marker")
	}
	return nil
}
