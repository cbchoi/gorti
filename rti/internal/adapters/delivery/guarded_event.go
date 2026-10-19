package delivery

import (
	"errors"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// EventGuard must be nonblocking and lock-free. Queue admission evaluates it
// under recipient locks, including during all-recipient reservation.
type EventGuard interface {
	Validate() error
}

type guardedEvent struct {
	event         core.OutboundEvent
	guard         EventGuard
	obsoleteError error
}

var ErrObsoleteAdvisory = errors.New("obsolete cancellable callback advisory")

func (e *guardedEvent) Seq() uint64 { return e.event.Seq() }

// GuardEvent keeps the guard with the event through batching and reservation.
// The caller retains responsibility for terminal callback receipt and retry;
// this envelope never discards an admitted event or makes Commit fallible.
func GuardEvent(event core.OutboundEvent, guard EventGuard) core.OutboundEvent {
	return &guardedEvent{event: event, guard: guard}
}

// GuardCancellableEvent explicitly classifies advisory work that becomes
// obsolete on the specified guard error. Never use it for a terminal result.
func GuardCancellableEvent(event core.OutboundEvent, guard EventGuard, obsoleteError error) core.OutboundEvent {
	return &guardedEvent{event: event, guard: guard, obsoleteError: obsoleteError}
}

func validateEvent(event core.OutboundEvent) error {
	if guarded, ok := event.(*guardedEvent); ok {
		if guarded.guard == nil || guarded.event == nil {
			return errors.New("callback projection guard unavailable")
		}
		if err := guarded.guard.Validate(); err != nil {
			return err
		}
		return validateEvent(guarded.event)
	}
	return nil
}

// ResolveEvent validates immediately before protocol conversion. On failure the
// caller still owns the original envelope and must apply its service's explicit
// stale-event policy. In particular, terminal ownership notifications cannot be
// silently dropped. A receipt/epoch contract is still required after wire send.
func ResolveEvent(event core.OutboundEvent) (core.OutboundEvent, error) {
	for {
		guarded, ok := event.(*guardedEvent)
		if !ok {
			return event, nil
		}
		if guarded.guard == nil || guarded.event == nil {
			return nil, errors.New("callback projection guard unavailable")
		}
		if err := guarded.guard.Validate(); err != nil {
			if guarded.obsoleteError != nil && errors.Is(err, guarded.obsoleteError) {
				return nil, errors.Join(ErrObsoleteAdvisory, err)
			}
			return nil, err
		}
		event = guarded.event
	}
}
