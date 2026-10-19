package server

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// This observer is composed with, not gated by, optional MOM reporting.
// Existing receipt authentication owns authorization of this notification.
func syncCallbackInvocationObserver(observer core.SyncCallbackObserver) func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, _ bool, _ string) error {
		if observer == nil || event == nil {
			return nil
		}
		if announced := event.GetSyncAnnounced(); announced != nil {
			return observer.ObserveSyncCallback(ctx, fed, h, core.SyncCallbackAnnounced, announced.GetLabel(), event.GetSeq())
		}
		if completed := event.GetSyncSynchronized(); completed != nil {
			return observer.ObserveSyncCallback(ctx, fed, h, core.SyncCallbackSynchronized, completed.GetLabel(), event.GetSeq())
		}
		if registered := event.GetSyncRegistrationSucceeded(); registered != nil {
			return observer.ObserveSyncCallback(ctx, fed, h, core.SyncCallbackRegistrationSucceeded, registered.GetLabel(), event.GetSeq())
		}
		if rejected := event.GetSyncRegistrationFailed(); rejected != nil {
			return observer.ObserveSyncCallback(ctx, fed, h, core.SyncCallbackRegistrationFailed, rejected.GetLabel(), event.GetSeq())
		}
		return nil
	}
}

// The receipt adapter must apply this before the SDK enters the handler body.
// It is not called by stream-send commit or by completion-only MOM telemetry.
func syncCallbackEntryObserver(observer core.SyncCallbackObserver) func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
	return func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
		if observer == nil || event == nil {
			return nil
		}
		if announced := event.GetSyncAnnounced(); announced != nil {
			return observer.ObserveSyncCallbackEntry(ctx, fed, h, core.SyncCallbackAnnounced, announced.GetLabel(), event.GetSeq())
		}
		if completed := event.GetSyncSynchronized(); completed != nil {
			return observer.ObserveSyncCallbackEntry(ctx, fed, h, core.SyncCallbackSynchronized, completed.GetLabel(), event.GetSeq())
		}
		if registered := event.GetSyncRegistrationSucceeded(); registered != nil {
			return observer.ObserveSyncCallbackEntry(ctx, fed, h, core.SyncCallbackRegistrationSucceeded, registered.GetLabel(), event.GetSeq())
		}
		if rejected := event.GetSyncRegistrationFailed(); rejected != nil {
			return observer.ObserveSyncCallbackEntry(ctx, fed, h, core.SyncCallbackRegistrationFailed, rejected.GetLabel(), event.GetSeq())
		}
		return nil
	}
}

// Member hooks may run inside the FM writer lease and must not reacquire it.
// Background retry pins checkpoint/generation before taking operationMu.
type synchronizationLifecycle struct {
	manager core.SyncLifecycle
	onError func(error)
	// Installed before Run starts. Member hooks never acquire this admission.
	retryAdmission func(context.Context, core.FederationName) (func(), error)
	operationMu    sync.Mutex
	mu             sync.Mutex
	pending        map[core.FederationName]*synchronizationPendingRetry
}

// Failure identity fences cleanup against work recorded by a recreated federation.
type synchronizationPendingRetry struct{ cause error }

func newSynchronizationLifecycle(manager core.SyncLifecycle, onError func(error)) *synchronizationLifecycle {
	return &synchronizationLifecycle{manager: manager, onError: onError, pending: map[core.FederationName]*synchronizationPendingRetry{}}
}

func (l *synchronizationLifecycle) Joined(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	return l.apply(fed, func() error { return l.manager.FederateJoined(ctx, fed, h) })
}

func (l *synchronizationLifecycle) Resigned(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	return l.apply(fed, func() error { return l.manager.FederateResigned(ctx, fed, h) })
}

// Call after restore commit and outside checkpoint/member locks. Retry failure
// schedules subsequent attempts; admitted obligations remain in the manager.
func (l *synchronizationLifecycle) Restored(ctx context.Context, fed core.FederationName) error {
	return l.retry(ctx, fed)
}

func (l *synchronizationLifecycle) apply(fed core.FederationName, operation func() error) error {
	l.operationMu.Lock()
	err := operation()
	l.recordResult(fed, err)
	l.operationMu.Unlock()
	l.reportError(err)
	return err
}

func (l *synchronizationLifecycle) recordResult(fed core.FederationName, err error) {
	l.mu.Lock()
	if err != nil {
		l.pending[fed] = &synchronizationPendingRetry{cause: err}
	} else {
		delete(l.pending, fed)
	}
	l.mu.Unlock()
}

func (l *synchronizationLifecycle) reportError(err error) {
	if err != nil && l.onError != nil {
		l.onError(err)
	}
}

func (l *synchronizationLifecycle) retry(ctx context.Context, fed core.FederationName) error {
	if l.retryAdmission != nil {
		l.mu.Lock()
		observed := l.pending[fed]
		l.mu.Unlock()
		release, err := l.retryAdmission(ctx, fed)
		if err != nil {
			if errors.Is(err, core.ErrFederationNotFound) {
				l.mu.Lock()
				if l.pending[fed] == observed {
					delete(l.pending, fed)
				}
				l.mu.Unlock()
			} else {
				l.recordResult(fed, err)
			}
			l.reportError(err)
			return err
		}
		if release != nil {
			defer release()
		}
	}
	return l.apply(fed, func() error { return l.manager.RetryPending(ctx, fed) })
}

func (l *synchronizationLifecycle) Retry(ctx context.Context) {
	namesSet := map[core.FederationName]struct{}{}
	if query, ok := l.manager.(core.SyncPendingQuery); ok {
		for _, fed := range query.PendingFederations() {
			namesSet[fed] = struct{}{}
		}
	}
	l.mu.Lock()
	var names []core.FederationName
	for fed := range l.pending {
		namesSet[fed] = struct{}{}
	}
	l.mu.Unlock()
	for fed := range namesSet {
		names = append(names, fed)
	}
	slices.Sort(names)
	for _, fed := range names {
		if ctx.Err() != nil {
			return
		}
		// retry records the pending obligation and reports any failure itself.
		if err := l.retry(ctx, fed); err != nil {
			continue
		}
	}
}

func (l *synchronizationLifecycle) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.Retry(ctx)
		}
	}
}

type synchronizationCheckpoint = savebundle.StrictSynchronization
