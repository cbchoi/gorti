package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

type destroyedSyncRetryRecorder struct {
	*syncpkg.Manager
	retries atomic.Int32
}

func (m *destroyedSyncRetryRecorder) RetryPending(ctx context.Context, fed core.FederationName) error {
	m.retries.Add(1)
	return m.Manager.RetryPending(ctx, fed)
}

func seedDestroyedSyncRetry(t *testing.T) (*syncIntegrationOutbox, *destroyedSyncRetryRecorder, *synchronizationLifecycle) {
	t.Helper()
	out := &syncIntegrationOutbox{}
	manager := &destroyedSyncRetryRecorder{Manager: newSyncIntegrationManager(t, out)}
	if err := manager.RegisterBy(context.Background(), "f", 1, "old-generation", nil, nil); err != nil {
		t.Fatal(err)
	}
	lifecycle := newSynchronizationLifecycle(manager, nil)
	out.setFail(true)
	if err := lifecycle.Joined(context.Background(), "f", 3); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal("pending lifecycle obligation not seeded:", err)
	}
	if len(manager.PendingFederations()) != 1 || len(lifecycle.pending) != 1 {
		t.Fatal("real manager/lifecycle pending obligation absent")
	}
	return out, manager, lifecycle
}

func TestSynchronizationDestroyedRetryDiscardsTerminalMarker(t *testing.T) {
	_, manager, lifecycle := seedDestroyedSyncRetry(t)
	manager.OnFederationDestroyed("f")
	var admissions int
	lifecycle.retryAdmission = func(context.Context, core.FederationName) (func(), error) {
		admissions++
		return nil, fmt.Errorf("generation admission: %w", core.ErrFederationNotFound)
	}
	if err := lifecycle.Restored(context.Background(), "f"); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatal("terminal admission error not surfaced:", err)
	}
	if len(lifecycle.pending) != 0 {
		t.Fatal("destroyed generation is still scheduled for retry")
	}
	lifecycle.Retry(context.Background())
	lifecycle.Retry(context.Background())
	if admissions != 1 || manager.retries.Load() != 0 || len(manager.PendingFederations()) != 0 {
		t.Fatalf("destroyed obligation resurrected: admissions=%d manager retries=%d", admissions, manager.retries.Load())
	}
}

func TestSynchronizationTransientRetryKeepsLiveGeneration(t *testing.T) {
	for _, failure := range []error{core.ErrSaveAlreadyInProgress, core.ErrFederationGenerationMismatch, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			out, manager, lifecycle := seedDestroyedSyncRetry(t)
			deny := true
			lifecycle.retryAdmission = func(context.Context, core.FederationName) (func(), error) {
				if deny {
					return nil, fmt.Errorf("temporary admission: %w", failure)
				}
				return func() {}, nil
			}
			if err := lifecycle.Restored(context.Background(), "f"); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if len(lifecycle.pending) != 1 || len(manager.PendingFederations()) != 1 || manager.retries.Load() != 0 {
				t.Fatal("transient rejection lost live generation work")
			}
			out.setFail(false)
			deny = false
			lifecycle.Retry(context.Background())
			if len(lifecycle.pending) != 0 || len(manager.PendingFederations()) != 0 || manager.retries.Load() != 1 {
				t.Fatal("live generation did not recover after admission resumed")
			}
		})
	}
}

func TestSynchronizationDestroyedRetryDoesNotEraseRecreatedGeneration(t *testing.T) {
	out, manager, lifecycle := seedDestroyedSyncRetry(t)
	attempted, proceed, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	release := func() { once.Do(func() { close(proceed) }) }
	t.Cleanup(release)
	lifecycle.retryAdmission = func(context.Context, core.FederationName) (func(), error) {
		close(attempted)
		<-proceed
		return nil, core.ErrFederationNotFound
	}
	go func() { done <- lifecycle.Restored(context.Background(), "f") }()
	select {
	case <-attempted:
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not reach generation admission")
	}
	manager.OnFederationDestroyed("f")
	out.setFail(false)
	if err := manager.RegisterBy(context.Background(), "f", 1, "new-generation", nil, nil); err != nil {
		t.Fatal(err)
	}
	out.setFail(true)
	if err := lifecycle.Joined(context.Background(), "f", 4); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal("new generation obligation not seeded:", err)
	}
	release()
	select {
	case err := <-done:
		if !errors.Is(err, core.ErrFederationNotFound) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal retry did not finish")
	}
	if len(lifecycle.pending) != 1 || len(manager.PendingFederations()) != 1 {
		t.Fatal("old admission failure erased recreated generation's pending work")
	}
	out.mu.Lock()
	out.sent = nil
	out.mu.Unlock()
	out.setFail(false)
	lifecycle.retryAdmission = func(context.Context, core.FederationName) (func(), error) { return func() {}, nil }
	lifecycle.Retry(context.Background())
	if len(lifecycle.pending) != 0 || len(manager.PendingFederations()) != 0 || manager.retries.Load() != 1 {
		t.Fatal("recreated generation pending work did not drain")
	}
	events := out.events()
	if len(events) != 1 {
		t.Fatalf("new generation delivery count=%d, want 1", len(events))
	}
	for _, event := range events {
		announced, ok := event.Event.(*syncpkg.AnnounceEvent)
		if !ok || announced.Label() != "new-generation" || event.Recipient != 4 {
			t.Fatal("old generation callback replayed after recreation")
		}
	}
}
