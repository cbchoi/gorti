package savepoint

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func saveParticipantStarted[T core.LogicalTimeScalar](as *activeSaveOf[T], h core.FederateHandle) bool {
	if as == nil {
		return false
	}
	if as.participantTime == nil {
		return !as.waiting
	}
	_, ok := as.initiated[h]
	return ok
}

func (m *ManagerOf[T]) reserveInitiation(ctx context.Context, fed core.FederationName, as *activeSaveOf[T], recipients []core.FederateHandle) (core.OutboxReservation, error) {
	out, ok := m.opts.Outbox.(core.ReservableOutbox)
	if !ok {
		return nil, ErrScheduledSaveUnsupported
	}
	deliveries := make([]core.OutboxDelivery, 0, len(recipients))
	for _, h := range recipients {
		deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: initiateFederateSaveEventOf[T](as.label, as.saveTime)})
	}
	r, err := out.Reserve(ctx, fed, deliveries)
	if err == nil && r == nil {
		err = errors.New("savepoint: missing initiation reservation")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil && r != nil {
		r.Release()
	}
	return r, err
}

func (m *ManagerOf[T]) progressParticipants(ctx context.Context, fed core.FederationName, as *activeSaveOf[T]) error {
	ready, err := as.participantTime.Ready(ctx)
	if err != nil {
		return m.failScheduledSave(ctx, fed, as, err)
	}
	var recipients []core.FederateHandle
	m.mu.Lock()
	for _, h := range ready {
		if _, started := as.initiated[h]; !started {
			recipients = append(recipients, h)
		}
	}
	m.mu.Unlock()
	if len(recipients) == 0 {
		return nil
	}
	if err = as.participantTime.Pause(recipients); err != nil {
		return m.failScheduledSave(ctx, fed, as, err)
	}
	r, err := m.reserveInitiation(ctx, fed, as, recipients)
	if err != nil {
		return m.failParticipantInitiation(ctx, fed, as, err)
	}
	defer r.Release()
	m.mu.Lock()
	for _, h := range recipients {
		as.initiated[h] = struct{}{}
	}
	as.waiting = len(as.initiated) < len(as.required)
	if !as.waiting {
		m.pendingSchedules.Delete(fed)
	}
	m.mu.Unlock()
	if err = r.Commit(); err != nil {
		r.Release()
		return m.failParticipantInitiation(ctx, fed, as, err)
	}
	return nil
}

func (m *ManagerOf[T]) failParticipantInitiation(ctx context.Context, fed core.FederationName, as *activeSaveOf[T], cause error) error {
	live := m.currentMembers(fed)
	var recipients []core.FederateHandle
	for _, h := range as.federates {
		if _, ok := live[h]; ok {
			recipients = append(recipients, h)
		}
	}
	m.finalizeSave(fed, as.label, true)
	return errors.Join(cause, m.emitSaveOutcome(ctx, fed, as.label, recipients, RTIUnableToSave))
}
