package savepoint

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// AcquireLoss serializes a detected session fault with checkpoint controls.
// Unlike an ordinary service, it must be able to fail an in-progress save.
func (m *ManagerOf[T]) AcquireLoss(fed core.FederationName) (func(), error) {
	release := m.AcquireCheckpoint(fed)
	m.mu.Lock()
	_, restoring := m.restore[fed]
	m.mu.Unlock()
	if restoring {
		release()
		return nil, core.ErrRestoreAlreadyInProgress
	}
	return release, nil
}

// FederateLost requires checkpoint and membership admission, in that order.
// Only surviving, already-instructed participants receive the fault outcome.
// Related MOM reports share the reservation, including when no save is active.
// Admission failure publishes neither reports nor outcomes and leaves save state.
func (m *ManagerOf[T]) FederateLost(ctx context.Context, fed core.FederationName, lost core.FederateHandle, reports ...core.OutboxDelivery) error {
	unlock := m.lockOperation(fed)
	defer unlock()
	m.mu.Lock()
	as := m.saves[fed]
	if as != nil {
		if _, required := as.required[lost]; !required {
			as = nil
		}
	}
	var recipients []core.FederateHandle
	if as != nil {
		for _, h := range as.federates {
			if h != lost && saveParticipantStarted(as, h) {
				recipients = append(recipients, h)
			}
		}
	}
	m.mu.Unlock()
	live := m.currentMembers(fed)
	deliveries := append([]core.OutboxDelivery(nil), reports...)
	for _, h := range recipients {
		if _, present := live[h]; present {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: federationNotSavedEvent(as.label, RTIDetectedFailureDuringSave)})
		}
	}
	release, err := m.deliverLossOutcome(ctx, fed, deliveries)
	if err != nil {
		return err
	}
	defer release()
	if as != nil {
		m.finalizeSave(fed, as.label, true)
		if m.opts.EventLog != nil {
			_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtFederationNotSaved, label: as.label})
		}
	}
	return nil
}

func (m *ManagerOf[T]) deliverLossOutcome(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (func(), error) {
	if len(deliveries) == 0 {
		return func() {}, nil
	}
	out, ok := m.opts.Outbox.(core.ReservableOutbox)
	if !ok {
		return nil, fmt.Errorf("savepoint: detected loss requires reservable delivery")
	}
	reservation, err := out.Reserve(ctx, fed, deliveries)
	if err != nil {
		if reservation != nil {
			reservation.Release()
		}
		return nil, err
	}
	if reservation == nil {
		return nil, fmt.Errorf("savepoint: missing fault outcome reservation")
	}

	handedOff := false
	defer func() {
		if !handedOff {
			reservation.Release()
		}
	}()
	if err := reservation.Commit(); err != nil {
		return nil, err
	}
	handedOff = true
	return reservation.Release, nil
}
