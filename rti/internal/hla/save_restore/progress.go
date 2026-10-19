package savepoint

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var ErrSaveNotInitiated = errors.New("savepoint: save not initiated for federate")

// ErrScheduledSaveUnsupported applies when no bounded time coordinator is wired.
var ErrScheduledSaveUnsupported = errors.New("savepoint: scheduled checkpoint coordinator is not configured")

// Status values follow the official 2010 Enums.h names, with zero reserved
// for an unspecified wire value. They are not federation-level SaveState.
type SaveStatus int32

const (
	NoSaveInProgress SaveStatus = iota + 1
	FederateInstructedToSave
	FederateSaving
	FederateWaitingForFederationToSave
)

type RestoreStatus int32

const (
	NoRestoreInProgress RestoreStatus = iota + 1
	FederateRestoreRequestPending
	FederateWaitingForRestoreToBegin
	FederatePreparedToRestore
	FederateRestoring
	FederateWaitingForFederationToRestore
)

type FederateSaveStatus struct {
	FederateHandle core.FederateHandle
	Status         SaveStatus
}
type FederateRestoreStatus struct {
	PreRestoreHandle  core.FederateHandle
	PostRestoreHandle core.FederateHandle
	Status            RestoreStatus
}

func (m *ManagerOf[T]) FederateSaveBegun(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
	unlock := m.lockOperation(fed)
	defer unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, busy := m.restore[fed]; busy {
		return core.ErrRestoreAlreadyInProgress
	}
	as, ok := m.saves[fed]
	if !ok || !saveParticipantStarted(as, h) || h == core.InvalidFederateHandle {
		return ErrSaveNotInitiated
	}
	if _, ok := as.begun[h]; ok {
		return ErrSaveNotInitiated
	}
	if _, ok := as.complete[h]; ok {
		return ErrSaveNotInitiated
	}
	if _, ok := as.notComp[h]; ok {
		return ErrSaveNotInitiated
	}
	if as.dynamic {
		as.required[h] = struct{}{}
		as.federates = sortedHandles(as.required)
	} else if _, ok := as.required[h]; !ok {
		return ErrSaveNotInitiated
	}
	as.begun[h] = struct{}{}
	return nil
}

func (m *ManagerOf[T]) currentMembers(fed core.FederationName) map[core.FederateHandle]struct{} {
	set := map[core.FederateHandle]struct{}{}
	if m.opts.Members != nil {
		for _, h := range m.opts.Members(fed) {
			set[h] = struct{}{}
		}
	} else if m.opts.Roster != nil {
		for _, member := range m.opts.Roster(fed) {
			set[member.Handle] = struct{}{}
		}
	}
	return set
}

// QueryFederationSaveStatus returns the current vector independently of the
// legacy label-indexed outcome query. A completed save reports NO_SAVE_IN_PROGRESS.
func (m *ManagerOf[T]) QueryFederationSaveStatus(fed core.FederationName) (string, []FederateSaveStatus, error) {
	// Membership may take the federation-manager lock or call back into us.
	set := m.currentMembers(fed)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, busy := m.restore[fed]; busy {
		return "", nil, core.ErrRestoreAlreadyInProgress
	}
	as := m.saves[fed]
	label := ""
	if as != nil {
		label = as.label
		for h := range as.required {
			set[h] = struct{}{}
		}
	}
	result := make([]FederateSaveStatus, 0, len(set))
	for _, h := range sortedHandles(set) {
		st := NoSaveInProgress
		if as != nil && saveParticipantStarted(as, h) {
			if _, ok := as.required[h]; ok {
				st = FederateInstructedToSave
			}
			if _, ok := as.begun[h]; ok {
				st = FederateSaving
			}
			if _, ok := as.complete[h]; ok {
				st = FederateWaitingForFederationToSave
			}
			if _, ok := as.notComp[h]; ok {
				st = FederateWaitingForFederationToSave
			}
		}
		result = append(result, FederateSaveStatus{h, st})
	}
	return label, result, nil
}

func (m *ManagerOf[T]) QueryFederationRestoreStatus(fed core.FederationName) (string, []FederateRestoreStatus, error) {
	set := m.currentMembers(fed)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, busy := m.saves[fed]; busy {
		return "", nil, core.ErrSaveAlreadyInProgress
	}
	ar := m.restore[fed]
	label := ""
	if ar != nil {
		label = ar.label
		for h := range ar.required {
			set[h] = struct{}{}
		}
	}
	result := make([]FederateRestoreStatus, 0, len(set))
	for _, h := range sortedHandles(set) {
		st, post := NoRestoreInProgress, core.InvalidFederateHandle
		if ar != nil {
			if ar.state == RestoreLoading {
				st = FederateRestoreRequestPending
			}
			if saved, ok := ar.postHandles[h]; ok {
				st, post = FederateRestoring, saved
			}
			if _, ok := ar.complete[h]; ok {
				st = FederateWaitingForFederationToRestore
			}
			if _, ok := ar.notComp[h]; ok {
				st = FederateWaitingForFederationToRestore
			}
		}
		result = append(result, FederateRestoreStatus{h, post, st})
	}
	return label, result, nil
}
