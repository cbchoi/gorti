package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type checkpointControlEntryKey struct{}

// CurrentCheckpointControlEntry distinguishes a current control invocation from
// pending controls superseded by a newer attempt or replayed from an old image.
func CurrentCheckpointControlEntry(ctx context.Context) bool {
	current, _ := ctx.Value(checkpointControlEntryKey{}).(bool)
	return current
}

func (r *CallbackReceipts) currentCheckpointControlLocked(entry *callbackReceipt) bool {
	if entry.event == nil || entry.event.GetCallbackRecovery() != nil {
		return false
	}
	if entry.event.GetSaveInitiate() == nil && entry.event.GetRestoreBegun() == nil && entry.event.GetRestoreInitiate() == nil {
		return false
	}
	return !r.newerCheckpointControlLocked(entry)
}

func (r *CallbackReceipts) newerCheckpointControlLocked(entry *callbackReceipt) bool {
	return entry.checkpointSupersededBy > entry.checkpointOrder
}

// Retain supersession on older receipts, not on the newer callback payload,
// which is discarded after completion. Queue preparation alone is not evidence
// of publication; callers mark it only after commit or authenticated delivery.
func (r *CallbackReceipts) publishCheckpointLocked(entry *callbackReceipt) {
	if entry.checkpointPublished {
		return
	}
	entry.checkpointPublished = true
	if entry.event == nil || entry.event.GetCallbackRecovery() != nil ||
		(entry.event.GetSaveInitiate() == nil && entry.event.GetRestoreBegun() == nil) {
		return
	}
	for _, other := range r.entries {
		if other.fed == entry.fed && other.h == entry.h && other.checkpointOrder < entry.checkpointOrder && other.checkpointSupersededBy < entry.checkpointOrder {
			// A stream may already have delivered an older prepared receipt
			// without committing send accounting. Retain its superseding order
			// even if this newer receipt completes or is evicted first. A later
			// mandatory queue commit assigns a fresh order to its own receipt.
			other.checkpointSupersededBy = entry.checkpointOrder
		}
	}
}

func (r *CallbackReceipts) pendingCheckpointTerminalLocked(entry *callbackReceipt) error {
	if entry.event == nil || (entry.event.GetSyncRegistrationSucceeded() == nil && entry.event.GetSyncRegistrationFailed() == nil) {
		return nil
	}
	for _, other := range r.entries {
		if !other.pendingCheckpointFor(entry.fed, entry.h) {
			continue
		}
		isSave := other.event.GetSaveCompleted() != nil || other.event.GetSaveFailed() != nil
		isRestore := other.event.GetRestoreCompleted() != nil || other.event.GetRestoreFailed() != nil
		if (!isSave && !isRestore) || r.newerCheckpointControlLocked(other) {
			continue
		}
		if isRestore {
			return core.ErrRestoreAlreadyInProgress
		}
		return core.ErrSaveAlreadyInProgress
	}
	return nil
}

// ReportEntry must return successfully before the SDK enters a user handler.
// Possession of the authenticated opaque receipt identifies the delivered event;
// a concurrent stream-send commit is not an invocation prerequisite.
func (r *CallbackReceipts) ReportEntry(ctx context.Context, fed core.FederationName, h core.FederateHandle, ticket []byte) error {
	if err := r.opts.Authenticate(ctx, fed, h); err != nil {
		return err
	}
	if len(ticket) != 32 {
		return status.Error(codes.InvalidArgument, "invalid callback entry receipt")
	}
	key := string(ticket)
	for {
		// Queue commit publishes outside the ledger lock. Wait for its outcome
		// before observing checkpoint history, including commit rollback.
		r.publicationMu.Lock()
		r.mu.Lock()
		r.publicationMu.Unlock()
		entry := r.entries[key]
		if entry == nil || entry.fed != fed || entry.h != h {
			r.mu.Unlock()
			return status.Error(codes.FailedPrecondition, "callback receipt expired or unavailable")
		}
		if entry.entryApplied {
			r.mu.Unlock()
			return nil
		}
		// Domain teardown may precede the participant's terminal callback. Keep
		// normal activity deferred until the actual terminal invocation begins.
		if err := r.pendingCheckpointTerminalLocked(entry); err != nil {
			r.mu.Unlock()
			return err
		}
		if pending := entry.entryWorking; pending != nil {
			r.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if entry.event == nil || entry.applied {
			r.mu.Unlock()
			return status.Error(codes.FailedPrecondition, "callback completed without entry evidence")
		}
		return r.observeCallbackEntryLocked(ctx, key, entry)
	}
}
func (entry *callbackReceipt) pendingCheckpointFor(fed core.FederationName, h core.FederateHandle) bool {
	return entry.fed == fed && entry.h == h && entry.checkpointPublished && !entry.entryApplied &&
		!entry.applied && entry.event != nil && entry.event.GetCallbackRecovery() == nil
}

func (r *CallbackReceipts) observeCallbackEntryLocked(ctx context.Context, key string, entry *callbackReceipt) error {
	// Receipt possession proves delivery even if stream-send accounting
	// has not yet committed, or its optional accounting later fails.
	r.publishCheckpointLocked(entry)
	pending := make(chan struct{})
	entry.entryWorking = pending
	event := proto.Clone(entry.event).(*rtiv1.FederateEvent)
	currentControl := r.currentCheckpointControlLocked(entry)
	r.mu.Unlock()
	var err error
	if r.opts.ObserveEntry != nil {
		err = r.opts.ObserveEntry(context.WithValue(ctx, checkpointControlEntryKey{}, currentControl), entry.fed, entry.h, event)
	}
	r.mu.Lock()
	if r.entries[key] == entry {
		entry.entryApplied = err == nil
		entry.entryWorking = nil
	} else if err == nil {
		err = status.Error(codes.FailedPrecondition, "callback lifecycle changed during entry")
	}
	close(pending)
	r.mu.Unlock()
	return err
}
