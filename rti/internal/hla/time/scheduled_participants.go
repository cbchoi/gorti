package time

import (
	"context"
	"slices"
	"sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type participantSaveReservationOf[T core.LogicalTimeScalar] struct {
	manager       *ManagerOf[T]
	federation    core.FederationName
	barrier       *scheduledTimeBarrierOf[T]
	evaluator     *sync.Mutex
	mu            sync.Mutex
	state         uint8 // prepared, committed, released
	pauseOnCommit bool
}

func (m *ManagerOf[T]) PrepareScheduledSave(fed core.FederationName, target T, members []core.FederateHandle, replace bool) (core.ScheduledSaveReservation, error) {
	if scalarNaN(target) || scalarInfinite(target) || target < 0 {
		return nil, core.ErrTimeInvalidLogicalTime
	}
	return m.prepareSaveBarrier(fed, target, members, replace, true)
}

func (m *ManagerOf[T]) PrepareImmediateSave(fed core.FederationName, members []core.FederateHandle, replace bool) (core.ScheduledSaveReservation, error) {
	return m.prepareSaveBarrier(fed, T(0), members, replace, false)
}

func (m *ManagerOf[T]) prepareSaveBarrier(fed core.FederationName, target T, members []core.FederateHandle, replace, timed bool) (core.ScheduledSaveReservation, error) {
	ext := extOfOf[T](m)
	eval := ext.evaluatorLock(fed)
	eval.Lock()
	ok := false
	defer func() {
		if !ok {
			eval.Unlock()
		}
	}()
	if ext.isHalted(fed) {
		return nil, core.ErrFederationHalted
	}
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if old := ext.scheduledSaves[fed]; old != nil {
		if !replace || !old.pausedAt.IsZero() || len(old.participantPaused) > 0 {
			return nil, core.ErrSaveAlreadyInProgress
		}
	}
	barrier := &scheduledTimeBarrierOf[T]{target: target, modes: make(map[core.FederateHandle]scheduledTimeMode, len(members)), participantPaused: make(map[core.FederateHandle]stdtime.Time)}
	if err := m.populateSaveBarrierLocked(ext, fed, barrier, members, timed); err != nil {
		return nil, err
	}
	ok = true
	return &participantSaveReservationOf[T]{manager: m, federation: fed, barrier: barrier, evaluator: eval, pauseOnCommit: !timed}, nil
}

func (r *participantSaveReservationOf[T]) Commit() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != 0 {
		return
	}
	ext := extOfOf[T](r.manager)
	ext.mu.Lock()
	if r.pauseOnCommit {
		for h := range r.barrier.modes {
			r.barrier.participantPaused[h] = r.manager.opts.Clock.Now()
		}
	}
	if ext.scheduledSaves == nil {
		ext.scheduledSaves = make(map[core.FederationName]*scheduledTimeBarrierOf[T])
	}
	ext.scheduledSaves[r.federation] = r.barrier
	ext.mu.Unlock()
	r.state = 1
	r.evaluator.Unlock()
}

func (r *participantSaveReservationOf[T]) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == 2 {
		return
	}
	if r.state == 0 {
		r.state = 2
		r.evaluator.Unlock()
		return
	}
	ext := extOfOf[T](r.manager)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if ext.scheduledSaves[r.federation] == r.barrier {
		now := r.manager.opts.Clock.Now()
		for h, at := range r.barrier.participantPaused {
			if ns := ext.getLocked(r.federation, h); ns != nil && ns.pendingNER && !ns.pendingSince.IsZero() {
				ns.pendingSince = ns.pendingSince.Add(now.Sub(at))
			}
		}
		delete(ext.scheduledSaves, r.federation)
	}
	r.state = 2
}

func (r *participantSaveReservationOf[T]) Pause(handles []core.FederateHandle) error {
	r.evaluator.Lock()
	defer r.evaluator.Unlock()
	ext := extOfOf[T](r.manager)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if ext.scheduledSaves[r.federation] != r.barrier {
		return ErrScheduledSaveTimeStateChanged
	}
	for _, h := range handles {
		if _, ok := r.barrier.modes[h]; !ok {
			return core.ErrFederateNotJoined
		}
	}
	for _, h := range handles {
		if r.barrier.participantPaused[h].IsZero() {
			r.barrier.participantPaused[h] = r.manager.opts.Clock.Now()
		}
	}
	return nil
}

func (r *participantSaveReservationOf[T]) Ready(ctx context.Context) ([]core.FederateHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, fed := r.manager, r.federation
	if err := m.tryGrantPending(ctx, fed); err != nil {
		return nil, err
	}
	r.evaluator.Lock()
	defer func() { r.evaluator.Unlock(); m.finishDeletionDelivery(fed) }()
	ext := extOfOf[T](m)
	if ext.isHalted(fed) {
		return nil, core.ErrFederationHalted
	}
	m.states.mu.Lock()
	ext.mu.Lock()
	ready, err := r.readyParticipantsLocked(ext)
	ext.mu.Unlock()
	m.states.mu.Unlock()
	if err != nil {
		return nil, err
	}
	slices.Sort(ready)
	for _, h := range ready {
		ext.mu.Lock()
		paused := scheduledRecipientPausedLocked(ext, fed, h)
		ext.mu.Unlock()
		if paused {
			continue
		}
		m.states.mu.Lock()
		state := m.states.getLocked(fed, h)
		implicit := state != nil && state.constrained && state.regulationPending
		m.states.mu.Unlock()
		if implicit {
			messages := m.takeEnableMessages(fed, h, true, r.barrier.target)
			if err := m.deliverBufferedSequence(ctx, fed, h, messages); err != nil {
				return nil, err
			}
		} else {
			if err := m.drainBufferedRO(ctx, fed, h); err != nil {
				return nil, err
			}
			if err := m.releaseBufferedTSO(ctx, fed, h, r.barrier.target); err != nil {
				return nil, err
			}
		}
	}
	return ready, nil
}

func scheduledPauseAtLocked[T core.LogicalTimeScalar](ext *nerStoreOf[T], fed core.FederationName, h core.FederateHandle) stdtime.Time {
	if b := ext.scheduledSaves[fed]; b != nil {
		if at := b.participantPaused[h]; !at.IsZero() {
			return at
		}
		return b.pausedAt
	}
	return stdtime.Time{}
}

func scheduledRecipientPausedLocked[T core.LogicalTimeScalar](ext *nerStoreOf[T], fed core.FederationName, h core.FederateHandle) bool {
	return !scheduledPauseAtLocked(ext, fed, h).IsZero()
}

func (m *ManagerOf[T]) populateSaveBarrierLocked(ext *nerStoreOf[T], fed core.FederationName, barrier *scheduledTimeBarrierOf[T], members []core.FederateHandle, timed bool) error {
	managed := false
	for _, h := range members {
		if h == core.InvalidFederateHandle {
			return core.ErrFederateNotJoined
		}
		mode := scheduledTimeMode{}
		if state := m.states.getLocked(fed, h); state != nil {
			mode = scheduledTimeMode{state.regulating, state.constrained}
		}
		barrier.modes[h] = mode
		managed = managed || mode.regulating || mode.constrained
		if timed && mode.constrained {
			current := T(0)
			if ns := ext.getLocked(fed, h); ns != nil {
				current = ns.currentTime
			}
			if barrier.target <= current {
				return core.ErrTimeRequestInPast
			}
		}
	}
	if timed && !managed || len(members) == 0 {
		return ErrScheduledSaveProfileUnsupported
	}
	return nil
}

func (r *participantSaveReservationOf[T]) readyParticipantsLocked(ext *nerStoreOf[T]) ([]core.FederateHandle, error) {
	m, fed := r.manager, r.federation
	allConstrained := true
	var ready, unconstrained []core.FederateHandle
	for h, mode := range r.barrier.modes {
		if !mode.constrained {
			unconstrained = append(unconstrained, h)
			continue
		}
		eligible, _, err := m.scheduledSaveStateLocked(fed, r.barrier, ext, h)
		if err != nil {
			return nil, err
		}
		if eligible {
			ready = append(ready, h)
		} else {
			allConstrained = false
		}
	}
	if ext.scheduledSaves[fed] != r.barrier {
		return nil, ErrScheduledSaveTimeStateChanged
	}
	if allConstrained {
		ready = append(ready, unconstrained...)
	}
	return ready, nil
}
