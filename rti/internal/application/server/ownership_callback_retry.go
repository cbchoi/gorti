package server

import (
	"context"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

// The root owns the worker lifetime. Transient queue pressure needs recovery
// even when the recipient's existing stream never disconnects.
func startOwnershipCallbackRetries(ctx context.Context, owner *ownership.Manager, members *federation.Manager, checkpoint saveObservationAdmission, interval time.Duration) func() {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			for _, scope := range owner.PendingOwnershipAdmissions() {
				checkpointRelease := func() {}
				if checkpoint != nil {
					checkpointRelease = checkpoint.AcquireObservation(scope.Federation)
				}
				release, err := members.AcquireMember(scope.Federation, scope.Recipient)
				if err == nil {
					_ = owner.RetryUnadmittedOwnershipCallbacks(ctx, scope)
					release()
				}
				checkpointRelease()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }
}
