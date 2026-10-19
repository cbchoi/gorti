package time

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type deletionLockLifecycle struct{ mu sync.Mutex }

func (l *deletionLockLifecycle) PrepareDeletionDelivery(core.FederationName, []core.OutboundEvent) core.OutboxReservation {
	l.mu.Lock()
	return &deletionLockReservation{release: l.mu.Unlock}
}
func (*deletionLockLifecycle) FinishDeletionDelivery(core.FederationName) {}

type deletionLockReservation struct {
	once    sync.Once
	release func()
}

func (r *deletionLockReservation) Commit() error { r.once.Do(r.release); return nil }
func (r *deletionLockReservation) Release()      { r.once.Do(r.release) }

type deletionLockOutbox struct {
	gate      chan struct{}
	attempted chan struct{}
	once      sync.Once
}

func (o *deletionLockOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}
func (o *deletionLockOutbox) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	if r, ok := enableFixtureAdmission(ds); ok {
		return r, nil
	}
	o.once.Do(func() { close(o.attempted) })
	<-o.gate
	return &deletionLockReservation{release: func() {
		select {
		case o.gate <- struct{}{}:
		default:
		}
	}}, nil
}

func TestDeletionGrantDoesNotInvertObjectAndOutboxLocks(t *testing.T) {
	ctx := context.Background()
	out := &deletionLockOutbox{gate: make(chan struct{}, 1), attempted: make(chan struct{})}
	lifecycle := &deletionLockLifecycle{}
	m, err := New(Options{Clock: core.NewRealClock(), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	m.SetDeletionLifecycle(lifecycle)
	if err := m.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := m.BufferTSO(ctx, "fed", 2, 10, &fakeRetractEvent{}); err != nil {
		t.Fatal(err)
	}
	grantDone := make(chan error, 1)
	go func() { grantDone <- m.TimeAdvanceRequest(ctx, "fed", 2, 10) }()
	<-out.attempted
	// An immediate object operation already owns the outbox reservation and is
	// now ready to acquire object state, matching production Delete admission.
	immediateDone := make(chan struct{})
	go func() {
		lifecycle.mu.Lock()
		lifecycle.mu.Unlock() //nolint:staticcheck // SA2001: acquiring the lock is the deadlock probe; no protected data is needed.
		select {
		case out.gate <- struct{}{}:
		default:
		}
		close(immediateDone)
	}()
	select {
	case <-immediateDone:
	case <-stdtime.After(150 * stdtime.Millisecond):
		t.Error("time delivery holds object state while waiting for an outbox reservation")
		out.gate <- struct{}{} // Release the test barrier so failed RED runs terminate.
	}
	select {
	case err := <-grantDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-stdtime.After(stdtime.Second):
		t.Fatal("grant did not finish")
	}
}
