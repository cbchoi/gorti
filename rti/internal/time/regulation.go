package time

import (
	"math"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// federateKey uniquely identifies a federate within a federation. Two
// federates with the same FederateHandle in different federations are
// distinct entries — see TestSpec_M3_PerFederationIsolation.
type federateKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

// federateState is the per-federate, per-federation time-management
// state. Cut-1 fields only: regulating + lookahead, constrained.
// NER timestamps + outstanding requests are added by Wave 2 (W2).
type federateState struct {
	regulating  bool
	lookahead   core.LogicalTime
	constrained bool
}

// stateStore is the goroutine-safe map from federateKey to
// *federateState backing Manager. Operations validate the requested
// transition and return the appropriate core sentinel error on
// invalid input.
//
// Concurrency model: a single mu guards the whole map plus every
// embedded *federateState. Per docs/sdd.md the cut-1 implementation
// trades simplicity for performance; M5 may shard if benchmarks demand.
type stateStore struct {
	mu     sync.Mutex
	states map[federateKey]*federateState
}

func newStateStore() *stateStore {
	return &stateStore{states: make(map[federateKey]*federateState)}
}

// getOrCreateLocked returns (and creates if missing) the state for
// (fed, h). Caller MUST hold s.mu.
func (s *stateStore) getOrCreateLocked(fed core.FederationName, h core.FederateHandle) *federateState {
	k := federateKey{fed: fed, h: h}
	st, ok := s.states[k]
	if !ok {
		st = &federateState{}
		s.states[k] = st
	}
	return st
}

// getLocked returns the state for (fed, h) or nil if absent. Caller
// MUST hold s.mu.
func (s *stateStore) getLocked(fed core.FederationName, h core.FederateHandle) *federateState {
	return s.states[federateKey{fed: fed, h: h}]
}

// enableRegulation transitions (fed, h) into the regulating set.
// Returns:
//   - core.ErrTimeInvalidLookahead if lookahead is negative or NaN
//   - core.ErrTimeAlreadyRegulating if already regulating
func (s *stateStore) enableRegulation(fed core.FederationName, h core.FederateHandle, lookahead core.LogicalTime) error {
	if !validLookahead(lookahead) {
		return core.ErrTimeInvalidLookahead
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreateLocked(fed, h)
	if st.regulating {
		return core.ErrTimeAlreadyRegulating
	}
	st.regulating = true
	st.lookahead = lookahead
	return nil
}

// disableRegulation removes (fed, h) from the regulating set. The
// federate's constrained flag is preserved.
//
// Returns core.ErrTimeNotRegulating when not currently regulating
// (including the case where the federate has no entry at all).
func (s *stateStore) disableRegulation(fed core.FederationName, h core.FederateHandle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getLocked(fed, h)
	if st == nil || !st.regulating {
		return core.ErrTimeNotRegulating
	}
	st.regulating = false
	st.lookahead = 0
	return nil
}

// enableConstrained transitions (fed, h) into the constrained set.
// Returns core.ErrTimeAlreadyConstrained if already constrained.
func (s *stateStore) enableConstrained(fed core.FederationName, h core.FederateHandle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreateLocked(fed, h)
	if st.constrained {
		return core.ErrTimeAlreadyConstrained
	}
	st.constrained = true
	return nil
}

// disableConstrained removes (fed, h) from the constrained set.
// Returns core.ErrTimeNotConstrained when not currently constrained.
func (s *stateStore) disableConstrained(fed core.FederationName, h core.FederateHandle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getLocked(fed, h)
	if st == nil || !st.constrained {
		return core.ErrTimeNotConstrained
	}
	st.constrained = false
	return nil
}

// snapshot returns a value copy of the current state for (fed, h).
// Absent federates yield the zero value (regulating = false,
// constrained = false). The returned struct is safe to read without
// locking; mutating it does NOT affect the stored state.
//
// snapshot is intended for tests and read-only inspection paths.
func (s *stateStore) snapshot(fed core.FederationName, h core.FederateHandle) federateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.getLocked(fed, h); st != nil {
		return *st
	}
	return federateState{}
}

// validLookahead enforces the cut-1 rule: lookahead must be a
// non-negative finite IEEE 754 double. Per FR-TM-1, NaN and negative
// values are rejected at the API boundary; +Inf is also rejected
// because it would make LBTS undefined for any regulating federate.
func validLookahead(t core.LogicalTime) bool {
	f := float64(t)
	if math.IsNaN(f) {
		return false
	}
	if f < 0 {
		return false
	}
	if math.IsInf(f, 1) {
		return false
	}
	return true
}
