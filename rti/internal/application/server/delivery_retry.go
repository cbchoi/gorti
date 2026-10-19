package server

import (
	"context"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const deliveryRetryInterval = 10 * time.Millisecond

type deliveryRetrySource interface {
	PendingDeliveryRetries() []core.FederationName
	RetryPendingDeliveries(context.Context, core.FederationName) error
}

type deliveryRetryMembership interface {
	GenerationFor(core.FederationName) (uint64, bool)
	AcquireGeneration(core.FederationName, uint64) (func(), error)
}

type deliveryRetryAdmission interface {
	AcquireService(core.FederationName) (func(), error)
}

// One server-owned worker services explicit retry markers. Construction is inert;
// Start and Close belong to the listener lifecycle, not to HLA services.
type deliveryRetryWorker struct {
	source     deliveryRetrySource
	membership deliveryRetryMembership
	admission  deliveryRetryAdmission
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	closed     bool
}

func newDeliveryRetryWorker(source deliveryRetrySource, membership deliveryRetryMembership, admission deliveryRetryAdmission) *deliveryRetryWorker {
	return &deliveryRetryWorker{source: source, membership: membership, admission: admission}
}

func (w *deliveryRetryWorker) Start(parent context.Context) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.done != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.done = make(chan struct{})
	go w.run(ctx, w.done)
}

func (w *deliveryRetryWorker) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	cancel, done := w.cancel, w.done
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (w *deliveryRetryWorker) run(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(deliveryRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			return
		}
		for _, fed := range w.source.PendingDeliveryRetries() {
			if ctx.Err() != nil {
				return
			}
			// Failure remains marked by the time manager. Admission failures leave
			// it untouched; neither case starts another goroutine or busy-retries.
			_ = w.retryFederation(ctx, fed)
		}
	}
}

func (w *deliveryRetryWorker) retryFederation(ctx context.Context, fed core.FederationName) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.admission != nil {
		release, err := w.admission.AcquireService(fed)
		if err != nil {
			return err
		}
		defer release()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	generation, exists := w.membership.GenerationFor(fed)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !exists {
		return core.ErrFederationNotFound
	}
	release, err := w.membership.AcquireGeneration(fed, generation)
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	return w.source.RetryPendingDeliveries(ctx, fed)
}
