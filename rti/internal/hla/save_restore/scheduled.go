package savepoint

import (
	"context"
	"errors"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var ErrInvalidScheduledSaveTime = errors.New("savepoint: scheduled checkpoint requires a finite nonnegative logical time")
var ErrScheduledSaveParticipantsChanged = errors.New("savepoint: scheduled checkpoint participants changed")

func (m *ManagerOf[T]) reserveSaveTime(ctx context.Context, fed core.FederationName, target T, members []core.FederateHandle) (func() (bool, error), func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if _, err := core.ScalarTimeValue(target); err != nil {
		return nil, nil, ErrInvalidScheduledSaveTime
	}
	if m.opts.ScheduledTime == nil || m.opts.Members == nil || len(members) == 0 {
		return nil, nil, ErrScheduledSaveUnsupported
	}
	ready, release, err := m.opts.ScheduledTime(fed, target, append([]core.FederateHandle(nil), members...))
	if err == nil && (ready == nil || release == nil) {
		err = errors.New("savepoint: invalid scheduled checkpoint coordinator reservation")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if release != nil {
			release()
		}
		return nil, nil, err
	}
	return ready, release, nil
}

func (m *ManagerOf[T]) releaseSaveTime(fed core.FederationName, as *activeSaveOf[T]) {
	if as != nil && as.releaseTime != nil {
		as.releaseTime()
		as.releaseTime = nil
		if m.opts.ResumeTime != nil {
			m.pendingResumes.Store(fed, as)
		}
	}
}

// HasPendingScheduledSave is the nonblocking release-path fast check. The
// exclusive admission in ProgressScheduledSave performs the authoritative check.
func (m *ManagerOf[T]) HasPendingScheduledSave(fed core.FederationName) bool {
	_, pending := m.pendingSchedules.Load(fed)
	_, resume := m.pendingResumes.Load(fed)
	return pending || resume
}

// ProgressScheduledSave must run after all caller membership/service leases
// have been released. Lock order: checkpoint admission -> protocol operation ->
// time evaluator -> time state. No time callback reenters this coordinator.
// Direct in-process integrations must apply the same admission discipline.
func (m *ManagerOf[T]) ProgressScheduledSave(ctx context.Context, fed core.FederationName) (err error) {
	if !m.HasPendingScheduledSave(fed) {
		return nil
	}
	release := m.AcquireCheckpoint(fed)
	defer release()
	unlock := m.lockOperation(fed)
	defer unlock()
	defer func() { err = errors.Join(err, m.resumeSaveTime(ctx, fed)) }()
	value, pending := m.pendingSchedules.Load(fed)
	if !pending {
		return nil
	}
	m.mu.Lock()
	as := m.saves[fed]
	if as == nil || as != value || !as.waiting {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.validateScheduledIdentity(fed, as); err != nil {
		return m.failScheduledSave(ctx, fed, as, err)
	}
	if m.opts.Halted != nil && m.opts.Halted(fed) {
		return m.failScheduledSave(ctx, fed, as, core.ErrFederationHalted)
	}
	if as.participantTime != nil {
		return m.progressParticipants(ctx, fed, as)
	}
	ready, err := as.timeReady()
	if err != nil {
		return m.failScheduledSave(ctx, fed, as, err)
	}
	if !ready {
		return nil
	}
	m.mu.Lock()
	as.waiting = false
	m.pendingSchedules.Delete(fed)
	m.mu.Unlock()
	return m.initiateSave(ctx, fed, as)
}

func (m *ManagerOf[T]) resumeSaveTime(ctx context.Context, fed core.FederationName) error {
	value, pending := m.pendingResumes.Load(fed)
	if !pending {
		return nil
	}
	m.mu.Lock()
	busy := m.saves[fed] != nil || m.restore[fed] != nil
	m.mu.Unlock()
	if busy {
		return nil
	}
	as := value.(*activeSaveOf[T])
	generation, _, err := m.resolveProvenance(fed, false)
	if err != nil || (generation == nil) != (as.generation == nil) || (generation != nil && *generation != *as.generation) {
		m.pendingResumes.CompareAndDelete(fed, value)
		return err
	}
	if err := m.opts.ResumeTime(ctx, fed); err != nil {
		return err
	}
	m.pendingResumes.CompareAndDelete(fed, value)
	return nil
}

func (m *ManagerOf[T]) validateScheduledIdentity(fed core.FederationName, as *activeSaveOf[T]) error {
	members := sortedHandles(m.currentMembers(fed))
	if !slices.Equal(members, as.federates) {
		return ErrScheduledSaveParticipantsChanged
	}
	generation, digest, err := m.resolveProvenance(fed, false)
	if err != nil {
		return err
	}
	if (generation == nil) != (as.generation == nil) || (generation != nil && *generation != *as.generation) || digest != as.fomSHA256 {
		return ErrSaveBundleIncompatible
	}
	return nil
}

func (m *ManagerOf[T]) failScheduledSave(ctx context.Context, fed core.FederationName, as *activeSaveOf[T], cause error) error {
	reason := SaveTimeCannotBeHonored
	// A departed recipient no longer has a delivery channel. Notify survivors
	// only, without enrolling newly joined federates into the failed operation.
	live := m.currentMembers(fed)
	if errors.Is(cause, ErrScheduledSaveParticipantsChanged) {
		reason = RTIDetectedFailureDuringSave
		for _, h := range as.federates {
			if _, present := live[h]; !present {
				reason = FederateResignedDuringSave
			}
		}
	}
	var recipients []core.FederateHandle
	for _, h := range as.federates {
		if _, ok := live[h]; ok && !errors.Is(cause, ErrSaveBundleIncompatible) {
			recipients = append(recipients, h)
		}
	}
	m.finalizeSave(fed, as.label, true)
	return errors.Join(cause, m.emitSaveOutcome(ctx, fed, as.label, recipients, reason))
}
