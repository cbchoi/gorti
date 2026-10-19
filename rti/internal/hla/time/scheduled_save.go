package time

import (
	"context"
	"errors"
	"fmt"
	"slices"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var ErrScheduledSaveProfileUnsupported = errors.New("time: scheduled checkpoint profile unsupported")
var ErrScheduledSaveCeiling = errors.New("time: advance exceeds scheduled checkpoint ceiling")
var ErrScheduledSaveTimeStateChanged = errors.New("time: scheduled checkpoint time state changed")

type scheduledTimeMode struct{ regulating, constrained bool }

type scheduledTimeBarrierOf[T core.LogicalTimeScalar] struct {
	target            T
	modes             map[core.FederateHandle]scheduledTimeMode
	pausedAt          stdtime.Time
	participantPaused map[core.FederateHandle]stdtime.Time
}

// ReserveScheduledSave holds crossing constrained grants without changing the
// pending request or manufacturing an intermediate grant. Zero lookahead is
// supported when actual pending requests establish a strict horizon past target.
//
// The application holds exclusive checkpoint admission across reservation and
// readiness. Lock order: admission -> protocol -> evaluator -> regulation -> NER.
// Release only removes this reservation, so stale releases cannot clear a new
// federation's barrier. No callback into Save/Restore occurs while holding locks.
func (m *ManagerOf[T]) ReserveScheduledSave(fed core.FederationName, target T, members []core.FederateHandle) (func() (bool, error), func(), error) {
	if scalarNaN(target) || scalarInfinite(target) || target < 0 {
		return nil, nil, fmt.Errorf("%w: finite nonnegative target required", ErrScheduledSaveProfileUnsupported)
	}
	ext := extOfOf[T](m)
	eval := ext.evaluatorLock(fed)
	eval.Lock()
	defer func() { eval.Unlock(); m.finishDeletionDelivery(fed) }()
	if ext.isHalted(fed) {
		return nil, nil, core.ErrFederationHalted
	}
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if ext.scheduledSaves[fed] != nil {
		return nil, nil, core.ErrSaveAlreadyInProgress
	}
	barrier := &scheduledTimeBarrierOf[T]{target: target, modes: make(map[core.FederateHandle]scheduledTimeMode, len(members))}
	if err := m.captureScheduledSaveModesLocked(ext, fed, barrier, members); err != nil {
		return nil, nil, err
	}
	if ext.scheduledSaves == nil {
		ext.scheduledSaves = make(map[core.FederationName]*scheduledTimeBarrierOf[T])
	}
	ext.scheduledSaves[fed] = barrier
	return func() (bool, error) { return m.scheduledSaveReady(fed, barrier) }, func() {
		m.releaseScheduledSave(ext, fed, barrier)
	}, nil
}

// holdGrantForScheduledSaveLocked is called under the evaluator lease and
// ext.mu. A strict grant reaches the save boundary; an available grant must
// cross it. A merely scheduled save does not stop unconstrained advancement.
func (m *ManagerOf[T]) holdGrantForScheduledSaveLocked(ext *nerStoreOf[T], fed core.FederationName, h core.FederateHandle, grant T) bool {
	barrier := ext.scheduledSaves[fed]
	if barrier == nil {
		return false
	}
	if scheduledRecipientPausedLocked(ext, fed, h) {
		return true
	}
	if !barrier.modes[h].constrained {
		return false
	}
	ns := ext.getLocked(fed, h)
	if ns == nil || !ns.pendingNER {
		return false
	}
	return grant > barrier.target || (strictAdvance(ns.mode) && grant == barrier.target)
}

func (m *ManagerOf[T]) scheduledSaveReady(fed core.FederationName, barrier *scheduledTimeBarrierOf[T]) (bool, error) {
	// Complete ordinary next-message/flush grants at or below the save boundary
	// first. A crossing request remains pending and contributes its normal floor.
	if err := m.tryGrantPending(context.Background(), fed); err != nil {
		return false, err
	}
	ext := extOfOf[T](m)
	eval := ext.evaluatorLock(fed)
	eval.Lock()
	defer func() { eval.Unlock(); m.finishDeletionDelivery(fed) }()
	if ext.isHalted(fed) {
		return false, core.ErrFederationHalted
	}
	m.states.mu.Lock()
	ext.mu.Lock()
	ready, _, err := m.scheduledSaveStateLocked(fed, barrier, ext)
	ext.mu.Unlock()
	m.states.mu.Unlock()
	if err != nil || !ready {
		return false, err
	}
	// No earlier/equal TSO can now arrive from a regulator. Deliver queued data
	// before initiation, without changing the granted time or pending request.
	handles := make([]core.FederateHandle, 0, len(barrier.modes))
	for h := range barrier.modes {
		handles = append(handles, h)
	}
	slices.Sort(handles)
	for _, h := range handles {
		m.states.mu.Lock()
		state := m.states.getLocked(fed, h)
		implicitAdvance := state != nil && state.constrained && state.regulationPending
		m.states.mu.Unlock()
		if implicitAdvance {
			release := m.takeEnableMessages(fed, h, true, barrier.target)
			if err := m.deliverBufferedSequence(context.Background(), fed, h, release); err != nil {
				return false, err
			}
			continue
		}
		if err := m.drainBufferedRO(context.Background(), fed, h); err != nil {
			return false, err
		}
		if err := m.releaseBufferedTSO(context.Background(), fed, h, barrier.target); err != nil {
			return false, err
		}
	}
	ext.mu.Lock()
	if barrier.pausedAt.IsZero() {
		barrier.pausedAt = m.opts.Clock.Now()
	}
	ext.mu.Unlock()
	return true, nil
}

func (m *ManagerOf[T]) scheduledSaveStateLocked(fed core.FederationName, barrier *scheduledTimeBarrierOf[T], ext *nerStoreOf[T], only ...core.FederateHandle) (bool, []RegulatingFederateOf[T], error) {
	if ext.scheduledSaves[fed] != barrier {
		return false, nil, ErrScheduledSaveTimeStateChanged
	}
	regulators, err := m.scheduledSaveRegulatorsLocked(fed, barrier, ext)
	if err != nil {
		return false, nil, err
	}
	lbts := m.lbtsBound(regulators)
	pending := 0
	for key, ns := range ext.states {
		if key.fed == fed && ns.pendingNER {
			pending++
		}
	}
	ready := true
	for h, mode := range barrier.modes {
		if len(only) > 0 && h != only[0] {
			continue
		}
		if !mode.constrained {
			continue
		}
		if !m.scheduledParticipantReadyLocked(fed, h, barrier, ext, lbts, pending) {
			ready = false
		}
	}
	return ready, regulators, nil
}

// ResumeScheduledSave reevaluates original requests after the save coordinator
// has delivered the terminal outcome. The application calls this under fresh
// checkpoint admission, never by reentering from inside a time-manager lock.
func (m *ManagerOf[T]) ResumeScheduledSave(ctx context.Context, fed core.FederationName) (err error) {
	defer func() {
		if err != nil && !errors.Is(err, core.ErrFederationHalted) {
			m.markDeliveryRetry(fed)
		}
	}()
	ext := extOfOf[T](m)
	lock := ext.evaluatorLock(fed)
	lock.Lock()
	ext.mu.Lock()
	var recipients []core.FederateHandle
	for key, ns := range ext.states {
		if key.fed == fed && (len(ns.tsoBuffer) != 0 || len(ns.roBuffer) != 0) {
			recipients = append(recipients, key.h)
		}
	}
	ext.mu.Unlock()
	slices.Sort(recipients)
	for _, h := range recipients {
		if err := m.drainBufferedRO(ctx, fed, h); err != nil {
			lock.Unlock()
			m.finishDeletionDelivery(fed)
			return err
		}
		if err := m.drainAllBufferedTSO(ctx, fed, h); err != nil {
			lock.Unlock()
			m.finishDeletionDelivery(fed)
			return err
		}
	}
	lock.Unlock()
	m.finishDeletionDelivery(fed)
	return m.tryGrantPending(ctx, fed)
}

func (m *ManagerOf[T]) releaseScheduledSave(ext *nerStoreOf[T], fed core.FederationName, barrier *scheduledTimeBarrierOf[T]) {
	ext.mu.Lock()
	if ext.scheduledSaves[fed] == barrier {
		if !barrier.pausedAt.IsZero() {
			pause := m.opts.Clock.Now().Sub(barrier.pausedAt)
			for key, ns := range ext.states {
				if key.fed == fed && ns.pendingNER && !ns.pendingSince.IsZero() {
					ns.pendingSince = ns.pendingSince.Add(pause)
				}
			}
		}
		delete(ext.scheduledSaves, fed)
	}
	ext.mu.Unlock()
}

func (m *ManagerOf[T]) scheduledSaveRegulatorsLocked(fed core.FederationName, barrier *scheduledTimeBarrierOf[T], ext *nerStoreOf[T]) ([]RegulatingFederateOf[T], error) {
	var regulators []RegulatingFederateOf[T]
	for h, initial := range barrier.modes {
		mode := scheduledTimeMode{}
		if state := m.states.getLocked(fed, h); state != nil {
			mode = scheduledTimeMode{state.regulating, state.constrained}
			if state.regulating {
				regulators = append(regulators, regulatingBoundSnapshotOf[T](h, state, ext.getLocked(fed, h)))
			}
		}
		if mode != initial {
			return nil, ErrScheduledSaveTimeStateChanged
		}
		if ns := ext.getLocked(fed, h); ns != nil && mode.constrained && ns.currentTime > barrier.target {
			return nil, ErrScheduledSaveTimeStateChanged
		}
	}
	// Newly time-managed handles outside the captured roster cannot be omitted
	// from the GALT computation, even in direct in-process use.
	for key, state := range m.states.states {
		if key.fed == fed && (state.regulating || state.constrained) {
			if _, ok := barrier.modes[key.h]; !ok {
				return nil, ErrScheduledSaveTimeStateChanged
			}
		}
	}
	slices.SortFunc(regulators, func(a, b RegulatingFederateOf[T]) int {
		if a.Handle < b.Handle {
			return -1
		}
		if a.Handle > b.Handle {
			return 1
		}
		return 0
	})
	return regulators, nil
}

func (m *ManagerOf[T]) scheduledParticipantReadyLocked(fed core.FederationName, h core.FederateHandle, barrier *scheduledTimeBarrierOf[T], ext *nerStoreOf[T], lbts core.TimeBoundOf[T], pending int) bool {
	state := m.states.getLocked(fed, h)
	if state != nil && state.regulationPending {
		// Enabling regulation for an already-constrained federate is an
		// implicit TARA. Its enabled callback is the corresponding grant.
		target, possible := m.enableTargetStateLocked(ext, fed, h, true)
		if !possible || target <= barrier.target {
			return false
		}
		return true
	}
	ns := ext.getLocked(fed, h)
	if ns == nil || !ns.pendingNER {
		return false
	}
	bound := lbts
	if galt := m.availableHorizonLocked(ext, fed, h); galt != nil {
		if !horizonBeyond(*galt, barrier.target, true) {
			return false
		}
		bound = minimumHorizon(bound, *galt)
	}
	next, hasTSO := nextGrantMessage(ns)
	grant := m.opts.GrantStrategy.DecideGrant(GrantContextOf[T]{
		Mode: ns.mode, CurrentTime: ns.currentTime, Requested: ns.requestedTime, LBTS: horizonScalar(bound), LBTSUnbounded: bound.Unbounded, SolePending: pending == 1, NextTSOTime: next, HasTSO: hasTSO,
	})
	return grant.Fire && m.holdGrantForScheduledSaveLocked(ext, fed, h, grant.Time)
}

func (m *ManagerOf[T]) captureScheduledSaveModesLocked(ext *nerStoreOf[T], fed core.FederationName, barrier *scheduledTimeBarrierOf[T], members []core.FederateHandle) error {
	managed := false
	for _, h := range members {
		if h == core.InvalidFederateHandle {
			return ErrScheduledSaveProfileUnsupported
		}
		mode := scheduledTimeMode{}
		if state := m.states.getLocked(fed, h); state != nil {
			mode = scheduledTimeMode{state.regulating, state.constrained}
		}
		barrier.modes[h] = mode
		managed = managed || mode.regulating || mode.constrained
		if ns := ext.getLocked(fed, h); ns != nil && mode.constrained {
			if ns.currentTime > barrier.target {
				return core.ErrTimeRequestInPast
			}
		}
	}
	if !managed {
		return fmt.Errorf("%w: time-managed participant required", ErrScheduledSaveProfileUnsupported)
	}
	return nil
}
