package time

import (
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// federateKey uniquely identifies a federate within a federation. Two
// federates with the same FederateHandle in different federations are
// distinct entries — see TestSpec_M3_PerFederationIsolation.
type federateKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

// federateState owns engagement, pending enable requests, actual lookahead,
// and the constrained receiver's strict delivery guarantee. Advance requests
// and message queues are held in nerState under the same evaluator lease.
type federateStateOf[T core.LogicalTimeScalar] struct {
	regulating           bool
	lookahead            T
	constrained          bool
	regulationPending    bool
	regulationLookahead  T
	constrainedPending   bool
	preservedReceiveTime *T
	receivedTSOTime      *T
	lookaheadTarget      T
	reducingLookahead    bool
	lookaheadOriginTime  T
	lookaheadOriginValue T
}

// stateStore is the goroutine-safe map from federateKey to
// *federateState backing Manager. Operations validate the requested
// transition and return the appropriate core sentinel error on
// invalid input.
//
// Concurrency model: a single mu guards the whole map plus every
// embedded *federateState. Per docs/sdd.md the cut-1 implementation
// trades simplicity for performance; M5 may shard if benchmarks demand.
type stateStoreOf[T core.LogicalTimeScalar] struct {
	mu     sync.Mutex
	states map[federateKey]*federateStateOf[T]
}

func newStateStoreOf[T core.LogicalTimeScalar]() *stateStoreOf[T] {
	return &stateStoreOf[T]{states: make(map[federateKey]*federateStateOf[T])}
}

// getOrCreateLocked returns (and creates if missing) the state for
// (fed, h). Caller MUST hold s.mu.
func (s *stateStoreOf[T]) getOrCreateLocked(fed core.FederationName, h core.FederateHandle) *federateStateOf[T] {
	k := federateKey{fed: fed, h: h}
	st, ok := s.states[k]
	if !ok {
		st = &federateStateOf[T]{}
		s.states[k] = st
	}
	return st
}

// getLocked returns the state for (fed, h) or nil if absent. Caller
// MUST hold s.mu.
func (s *stateStoreOf[T]) getLocked(fed core.FederationName, h core.FederateHandle) *federateStateOf[T] {
	return s.states[federateKey{fed: fed, h: h}]
}

// enableRegulation transitions (fed, h) into the regulating set.
// Returns:
//   - core.ErrTimeInvalidLookahead if lookahead is negative or NaN
//   - core.ErrTimeAlreadyRegulating if already regulating
func (s *stateStoreOf[T]) enableRegulation(fed core.FederationName, h core.FederateHandle, lookahead T) error {
	if !validLookaheadOf[T](lookahead) {
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
	st.lookaheadTarget = 0
	st.reducingLookahead = false
	st.lookaheadOriginTime, st.lookaheadOriginValue = 0, 0
	return nil
}

// advanceLookahead applies a requested decrease only as logical time advances
// (IEEE 1516.1-2010 8.19). The caller holds the regulation-state lock.
func (st *federateStateOf[T]) advanceLookahead(at T) {
	if st == nil || !st.regulating || !st.reducingLookahead {
		return
	}
	st.lookahead = st.projectedLookahead(at)
	if st.lookahead == st.lookaheadTarget {
		st.lookaheadTarget = 0
		st.reducingLookahead = false
		st.lookaheadOriginTime, st.lookaheadOriginValue = 0, 0
	}
}

// projectedLookahead is the actual lookahead at the given logical time, without
// changing the value returned by QueryLookahead while the request is pending.
func (st *federateStateOf[T]) projectedLookahead(at T) T {
	delta := at - st.lookaheadOriginTime
	if st.reducingLookahead && delta > 0 {
		// Retain the original interval so sub-ULP advances accumulate across grants.
		return max(st.lookaheadTarget, st.lookaheadOriginValue-delta)
	}
	return st.lookahead
}

// disableRegulation removes (fed, h) from the regulating set. The
// federate's constrained flag is preserved.
//
// Returns core.ErrTimeNotRegulating when not currently regulating
// (including the case where the federate has no entry at all).
func (s *stateStoreOf[T]) disableRegulation(fed core.FederationName, h core.FederateHandle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getLocked(fed, h)
	if st == nil || !st.regulating {
		return core.ErrTimeNotRegulating
	}
	st.regulating = false
	st.lookahead = 0
	st.lookaheadTarget = 0
	st.reducingLookahead = false
	st.lookaheadOriginTime, st.lookaheadOriginValue = 0, 0
	return nil
}

// enableConstrained transitions (fed, h) into the constrained set.
// Returns core.ErrTimeAlreadyConstrained if already constrained.
func (s *stateStoreOf[T]) enableConstrained(fed core.FederationName, h core.FederateHandle) error {
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
func (s *stateStoreOf[T]) disableConstrained(fed core.FederationName, h core.FederateHandle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getLocked(fed, h)
	if st == nil || !st.constrained {
		return core.ErrTimeNotConstrained
	}
	st.constrained = false
	st.preservedReceiveTime = nil
	st.receivedTSOTime = nil
	return nil
}

// snapshot returns a value copy of the current state for (fed, h).
// Absent federates yield the zero value (regulating = false,
// constrained = false). The returned struct is safe to read without
// locking; mutating it does NOT affect the stored state.
//
// snapshot is intended for tests and read-only inspection paths.
func (s *stateStoreOf[T]) snapshot(fed core.FederationName, h core.FederateHandle) federateStateOf[T] {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.getLocked(fed, h); st != nil {
		return *st
	}
	return federateStateOf[T]{}
}

// validLookahead enforces the cut-1 rule: lookahead must be a
// non-negative finite IEEE 754 double. Per FR-TM-1, NaN and negative
// values are rejected at the API boundary; +Inf is also rejected
// because it would make LBTS undefined for any regulating federate.
func validLookaheadOf[T core.LogicalTimeScalar](t T) bool {
	return t >= 0 && !scalarNaN(t) && !scalarInfinite(t)
}
