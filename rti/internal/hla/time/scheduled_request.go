package time

import (
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ValidateScheduledSaveRequest observes requester promises and receiver time in
// one evaluator lease. It never installs a barrier or modifies an old request.
func (m *ManagerOf[T]) ValidateScheduledSaveRequest(fed core.FederationName, requester core.FederateHandle, target T, members []core.FederateHandle) error {
	if requester == core.InvalidFederateHandle || !slices.Contains(members, requester) {
		return core.ErrFederateNotJoined
	}
	if scalarNaN(target) || scalarInfinite(target) || target < 0 {
		return core.ErrTimeInvalidLogicalTime
	}
	ext := extOfOf[T](m)
	eval := ext.evaluatorLock(fed)
	eval.Lock()
	defer eval.Unlock()
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	state := m.states.getLocked(fed, requester)
	if state != nil && state.regulating {
		base, lookahead, exclusive := outgoingTSOBoundOf[T](state, ext.getLocked(fed, requester))
		floor := addHorizon(base, lookahead)
		if floor.Unbounded || !timestampAtOrBeyond(target, floor.Limit, exclusive) {
			return core.ErrTimeInvalidLogicalTime
		}
	} else {
		galt := m.availableHorizonLocked(ext, fed, requester)
		if galt == nil {
			return core.ErrSaveTimeNotAllowed
		}
		if galt.Unbounded || target <= galt.Limit {
			return core.ErrTimeRequestInPast
		}
	}
	return m.validateSaveReceiverTimesLocked(ext, fed, target, members)
}

func (m *ManagerOf[T]) validateSaveReceiverTimesLocked(ext *nerStoreOf[T], fed core.FederationName, target T, members []core.FederateHandle) error {
	for _, h := range members {
		if receiver := m.states.getLocked(fed, h); receiver != nil && receiver.constrained {
			current := T(0)
			if advance := ext.getLocked(fed, h); advance != nil {
				current = advance.currentTime
			}
			if target <= current {
				return core.ErrTimeRequestInPast
			}
		}
	}
	return nil
}
