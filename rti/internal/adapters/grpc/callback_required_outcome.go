package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (r *CallbackReceipts) reportRequiredOutcome(ctx context.Context, key string, entry *callbackReceipt, success bool, exception string) error {
	for {
		r.mu.Lock()
		if r.entries[key] != entry || !entry.entryApplied {
			r.mu.Unlock()
			return status.Error(codes.FailedPrecondition, "required callback lifecycle unavailable")
		}
		if entry.reported && (entry.success != success || entry.exception != exception) {
			r.mu.Unlock()
			return status.Error(codes.InvalidArgument, "conflicting callback outcome")
		}
		if entry.applied {
			r.mu.Unlock()
			return nil
		}
		if working := entry.outcomeWorking; working != nil {
			r.mu.Unlock()
			select {
			case <-working:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return r.applyRequiredOutcomeLocked(ctx, key, entry, success, exception)
	}
}
func (r *CallbackReceipts) applyRequiredOutcomeLocked(ctx context.Context, key string, entry *callbackReceipt, success bool, exception string) error {
	entry.reported, entry.success, entry.exception = true, success, exception
	working := make(chan struct{})
	entry.outcomeWorking = working
	event := proto.Clone(entry.event).(*rtiv1.FederateEvent)
	r.mu.Unlock()
	// A failed application retains the payload and outcome for idempotent
	// retry. Sending or reporting alone cannot discharge this obligation.
	observer := r.opts.ObserveRequired
	if observer == nil {
		observer = r.opts.Observe
	}
	err := observer(ctx, entry.fed, entry.h, event, success, exception)
	r.mu.Lock()
	applied := false
	if r.entries[key] == entry {
		entry.outcomeWorking = nil
		if err == nil {
			r.completeLocked(key, entry)
			entry.event = nil
			applied = true
		}
	} else if err == nil {
		err = status.Error(codes.FailedPrecondition, "callback lifecycle changed during outcome")
	}
	close(working)
	r.mu.Unlock()
	if applied {
		r.pendingDelta(entry, -1)
		// Optional accounting cannot reopen a successfully applied lifecycle
		// obligation. Its failure invalidates coverage, not HLA state.
		if r.opts.ObserveRequired != nil {
			if metricErr := r.opts.Observe(ctx, entry.fed, entry.h, event, success, exception); metricErr != nil {
				r.unavailable(entry.fed, entry.h, metricErr)
			}
		}
	}
	return err
}
