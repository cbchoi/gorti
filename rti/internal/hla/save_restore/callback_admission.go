package savepoint

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// CheckFederateCallback checks eligibility while the transport holds checkpoint
// admission. It does not acquire another lease, which would invert that lock.
func (m *ManagerOf[T]) CheckFederateCallback(fed core.FederationName, h core.FederateHandle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if as := m.saves[fed]; as != nil {
		_, entered := as.callbackEntered[h]
		_, begun := as.begun[h]
		_, complete := as.complete[h]
		_, failed := as.notComp[h]
		if entered || begun || complete || failed {
			return core.ErrSaveAlreadyInProgress
		}
	}
	if ar := m.restore[fed]; ar != nil {
		if _, entered := ar.callbackEntered[h]; entered {
			return core.ErrRestoreAlreadyInProgress
		}
	}
	return nil
}

// ObserveCheckpointCallbackEntry records the participant's actual transition,
// not merely a control message waiting behind earlier callbacks in its queue.
// The caller authenticates the receipt and excludes superseded/replayed controls.
func (m *ManagerOf[T]) ObserveCheckpointCallbackEntry(fed core.FederationName, h core.FederateHandle, label string, restore bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if restore {
		ar := m.restore[fed]
		if ar == nil || label != "" && label != ar.label {
			return
		}
		if _, member := ar.required[h]; !member {
			return
		}
		if ar.callbackEntered == nil {
			ar.callbackEntered = make(map[core.FederateHandle]struct{})
		}
		ar.callbackEntered[h] = struct{}{}
		return
	}
	as := m.saves[fed]
	if as == nil || label != as.label || !saveParticipantStarted(as, h) {
		return
	}
	if as.callbackEntered == nil {
		as.callbackEntered = make(map[core.FederateHandle]struct{})
	}
	as.callbackEntered[h] = struct{}{}
}
