package time

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RetractWithError implements 8.21. Admission serializes with sends, grants,
// lookahead changes, and queued delivery through the federation evaluator.
func (m *ManagerOf[T]) RetractWithError(ctx context.Context, fed core.FederationName, sender core.FederateHandle, id uint64) error {
	return m.RetractWithCleanup(ctx, fed, sender, id, nil)
}

// RetractWithCleanup lets object management cancel a pending timed deletion in
// the same transaction as callbacks and queued-message cancellation. Prepare
// must return a rollback-capable, non-failing-on-commit object reservation.
func (m *ManagerOf[T]) RetractWithCleanup(ctx context.Context, fed core.FederationName, sender core.FederateHandle, id uint64, prepare func() (core.OutboxReservation, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ext := extOfOf[T](m)
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	defer lease.Unlock()
	floor, err := m.retractionFloorLocked(ext, fed, sender)
	if err != nil {
		return err
	}
	message, state, err := m.retractableMessage(fed, sender, id, floor)
	if err != nil {
		return err
	}
	callbacks := make([]core.OutboxDelivery, 0, len(state.Delivered))
	for _, recipient := range state.Delivered {
		callbacks = append(callbacks, core.OutboxDelivery{Recipient: recipient, Event: &RequestRetraction{Sender: sender, RetractionHandle: id}})
	}
	delivery, err := m.reserveRetractionCallbacks(ctx, fed, callbacks)
	if delivery != nil {
		defer delivery.Release()
	}
	if err != nil {
		return err
	}
	var cleanup core.OutboxReservation
	if prepare != nil {
		var err error
		cleanup, err = prepare()
		if cleanup != nil {
			defer cleanup.Release()
		}
		if err != nil {
			return err
		}
	}
	if err := m.publishRetractionCallbacks(ctx, fed, callbacks, delivery, cleanup); err != nil {
		return err
	}
	ext.mu.Lock()
	for key, ns := range ext.states {
		if key.fed != fed {
			continue
		}
		ns.tsoBuffer = removeRetractedMessagesOf[T](ns.tsoBuffer, sender, id)
		ns.roBuffer = removeRetractedMessagesOf[T](ns.roBuffer, sender, id)
	}
	ext.mu.Unlock()
	m.retractions.mu.Lock()
	message.Retracted = true
	m.retractions.mu.Unlock()
	return nil
}

func removeRetractedMessagesOf[T core.LogicalTimeScalar](buffer []bufferedTSOEventOf[T], sender core.FederateHandle, id uint64) []bufferedTSOEventOf[T] {
	kept := buffer[:0]
	for _, message := range buffer {
		if message.sender != sender || message.retractionHandle != id {
			kept = append(kept, message)
		}
	}
	clear(buffer[len(kept):])
	return kept
}

// The caller retains the evaluator lease across validation and retraction.
func (m *ManagerOf[T]) retractionFloorLocked(ext *nerStoreOf[T], fed core.FederationName, sender core.FederateHandle) (core.TimeBoundOf[T], error) {
	m.states.mu.Lock()
	ext.mu.Lock()
	fs := m.states.getLocked(fed, sender)
	if fs == nil || !fs.regulating {
		ext.mu.Unlock()
		m.states.mu.Unlock()
		return core.TimeBoundOf[T]{}, core.ErrTimeNotRegulating
	}
	base := T(0)
	if ns := ext.getLocked(fed, sender); ns != nil {
		base = ns.currentTime
		if ns.pendingNER {
			base = ns.requestedTime
		}
	}
	// Unlike send admission, 8.21 uses actual CURRENT lookahead even while
	// advancing. A requested reduction has not taken effect at a future grant.
	floor := addHorizon(base, fs.lookahead)
	ext.mu.Unlock()
	m.states.mu.Unlock()
	return floor, nil
}

func (m *ManagerOf[T]) retractableMessage(fed core.FederationName, sender core.FederateHandle, id uint64, floor core.TimeBoundOf[T]) (*RetractionMessageStateOf[T], RetractionMessageStateOf[T], error) {
	m.retractions.mu.Lock()
	f := m.retractions.federations[fed]
	var message *RetractionMessageStateOf[T]
	if f != nil && f.producers[sender] != nil {
		message = f.producers[sender].messages[id]
	}
	if message == nil {
		m.retractions.mu.Unlock()
		return nil, RetractionMessageStateOf[T]{}, core.ErrRetractionHandleInvalid
	}
	if message.Retracted || floor.Unbounded || !timestampAtOrBeyond(message.Timestamp, floor.Limit, true) {
		m.retractions.mu.Unlock()
		return nil, RetractionMessageStateOf[T]{}, core.ErrRetractionTooLate
	}
	state := cloneRetractionMessageOf[T](*message)
	m.retractions.mu.Unlock()
	return message, state, nil
}

func (m *ManagerOf[T]) reserveRetractionCallbacks(ctx context.Context, fed core.FederationName, callbacks []core.OutboxDelivery) (core.OutboxReservation, error) {
	var delivery core.OutboxReservation
	if len(callbacks) != 0 {
		if out, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
			var err error
			delivery, err = out.Reserve(ctx, fed, callbacks)
			if err != nil {
				return delivery, err
			}
			if delivery == nil {
				return delivery, fmt.Errorf("retract: nil callback reservation")
			}
		} else if len(callbacks) > 1 {
			return delivery, fmt.Errorf("retract: atomic callbacks require reservable outbox")
		}
	}
	return delivery, nil
}

func (m *ManagerOf[T]) publishRetractionCallbacks(ctx context.Context, fed core.FederationName, callbacks []core.OutboxDelivery, delivery, cleanup core.OutboxReservation) error {
	if delivery != nil {
		if err := delivery.Commit(); err != nil {
			return err
		}
	} else if len(callbacks) == 1 {
		if err := m.opts.Outbox.Send(ctx, fed, callbacks[0].Recipient, callbacks[0].Event); err != nil {
			return err
		}
	}
	if cleanup != nil {
		if err := cleanup.Commit(); err != nil {
			return err
		}
	}
	return nil
}
