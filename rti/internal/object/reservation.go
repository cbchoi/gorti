// Object instance name reservation per IEEE 1516.1-2010 §6.1-6.5.
// M26 Phase F.
//
// Federates reserve names before registering object instances. The
// reservation table is per-federation, name-keyed, and records which
// federate holds the reservation. A reservation is consumed by a
// successful RegisterObjectInstance with that name (the name moves
// from the reservation table to the registered set), or released
// explicitly via ReleaseObjectInstanceName, or cleared on resign.
//
// Wire semantics: ReserveObjectInstanceName returns Empty
// synchronously; the actual success/failure is delivered as a
// FederateEvent (ObjectInstanceNameReservationSucceeded / Failed)
// on the federate's event stream. The async delivery matches IEEE
// 1516.1's callback model.

package object

import (
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// reservationStore is the per-federation name → holding-federate map.
// Goroutine-safe via a single mutex; reads + writes are short.
type reservationStore struct {
	mu sync.Mutex
	// reservations[fed][name] = holder
	reservations map[core.FederationName]map[string]core.FederateHandle
	// registered[fed] = set of names that have moved from reservation
	// to registered-instance status. Lookups treat both maps as
	// "name in use" — once registered, the name cannot be re-reserved
	// until the instance is deleted.
	registered map[core.FederationName]map[string]struct{}
}

func newReservationStore() *reservationStore {
	return &reservationStore{
		reservations: map[core.FederationName]map[string]core.FederateHandle{},
		registered:   map[core.FederationName]map[string]struct{}{},
	}
}

// Reserve attempts to reserve name in fed for holder. Returns
// (true, nil) on success, (false, nil) if name is already
// reserved/registered, or (false, err) on validation failure.
func (s *reservationStore) Reserve(fed core.FederationName, holder core.FederateHandle, name string) (bool, error) {
	if name == "" {
		return false, core.ErrObjectInstanceNameInUse
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inUseLocked(fed, name) {
		return false, nil
	}
	m := s.reservations[fed]
	if m == nil {
		m = map[string]core.FederateHandle{}
		s.reservations[fed] = m
	}
	m[name] = holder
	return true, nil
}

// ReserveBatch attempts an atomic batch reservation. On success
// returns (nil, nil). On collision returns (nil, collidingNames) —
// NO reservation is made. Holder-side ownership applies to every
// name in the batch.
func (s *reservationStore) ReserveBatch(fed core.FederationName, holder core.FederateHandle, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Pass 1: collect collisions. Atomic check-then-act.
	var colliding []string
	for _, n := range names {
		if n == "" || s.inUseLocked(fed, n) {
			colliding = append(colliding, n)
		}
	}
	// Detect duplicate names within the batch — same name twice in
	// one batch is itself a collision.
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if _, dup := seen[n]; dup {
			// Re-collide if not already in the list.
			alreadyListed := false
			for _, c := range colliding {
				if c == n {
					alreadyListed = true
					break
				}
			}
			if !alreadyListed {
				colliding = append(colliding, n)
			}
		}
		seen[n] = struct{}{}
	}
	if len(colliding) > 0 {
		return colliding, nil
	}
	// Pass 2: commit.
	m := s.reservations[fed]
	if m == nil {
		m = map[string]core.FederateHandle{}
		s.reservations[fed] = m
	}
	for _, n := range names {
		m[n] = holder
	}
	return nil, nil
}

// Release drops a reservation. Returns:
//   - nil on success
//   - ErrObjectInstanceNameNotReserved if name is not reserved
//   - ErrObjectInstanceNameReservedByOther if reserved by a different federate
func (s *reservationStore) Release(fed core.FederationName, holder core.FederateHandle, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.reservations[fed]
	if m == nil {
		return core.ErrObjectInstanceNameNotReserved
	}
	h, ok := m[name]
	if !ok {
		return core.ErrObjectInstanceNameNotReserved
	}
	if h != holder {
		return core.ErrObjectInstanceNameReservedByOther
	}
	delete(m, name)
	if len(m) == 0 {
		delete(s.reservations, fed)
	}
	return nil
}

// Consume moves a reservation into the registered-name set when
// RegisterObjectInstance succeeds with a named instance. Returns:
//   - nil on success (name was reserved by holder, moves to registered)
//   - ErrObjectInstanceNameNotReserved if name was never reserved
//   - ErrObjectInstanceNameReservedByOther if reserved by a different federate
func (s *reservationStore) Consume(fed core.FederationName, holder core.FederateHandle, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.reservations[fed]
	if m == nil {
		return core.ErrObjectInstanceNameNotReserved
	}
	h, ok := m[name]
	if !ok {
		return core.ErrObjectInstanceNameNotReserved
	}
	if h != holder {
		return core.ErrObjectInstanceNameReservedByOther
	}
	delete(m, name)
	if len(m) == 0 {
		delete(s.reservations, fed)
	}
	reg := s.registered[fed]
	if reg == nil {
		reg = map[string]struct{}{}
		s.registered[fed] = reg
	}
	reg[name] = struct{}{}
	return nil
}

// MarkRegistered records a name as registered without going through
// a prior reservation. Used by RegisterObjectInstance when the
// caller did NOT pre-reserve (the cut-1 / pre-M26 path that
// generates a name on the server side). Also used when the server
// auto-generates a name.
func (s *reservationStore) MarkRegistered(fed core.FederationName, name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reg := s.registered[fed]
	if reg == nil {
		reg = map[string]struct{}{}
		s.registered[fed] = reg
	}
	reg[name] = struct{}{}
}

// ForgetRegistered drops a name from the registered set (called
// when the instance is deleted, freeing the name for re-reservation).
func (s *reservationStore) ForgetRegistered(fed core.FederationName, name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if reg := s.registered[fed]; reg != nil {
		delete(reg, name)
		if len(reg) == 0 {
			delete(s.registered, fed)
		}
	}
}

// OnFederateResign drops every reservation owned by the resigning
// federate. The registered-name set is left alone — those names
// belong to live object instances that the object registry's
// resign-cleanup path manages independently (M24 ResignAction).
func (s *reservationStore) OnFederateResign(fed core.FederationName, h core.FederateHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.reservations[fed]
	if m == nil {
		return
	}
	for name, holder := range m {
		if holder == h {
			delete(m, name)
		}
	}
	if len(m) == 0 {
		delete(s.reservations, fed)
	}
}

// OnFederationDestroyed drops every reservation + registered name
// for the federation. Idempotent.
func (s *reservationStore) OnFederationDestroyed(fed core.FederationName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.reservations, fed)
	delete(s.registered, fed)
}

// inUseLocked reports whether name is currently reserved OR
// registered in fed. Caller must hold s.mu.
func (s *reservationStore) inUseLocked(fed core.FederationName, name string) bool {
	if m, ok := s.reservations[fed]; ok {
		if _, has := m[name]; has {
			return true
		}
	}
	if reg, ok := s.registered[fed]; ok {
		if _, has := reg[name]; has {
			return true
		}
	}
	return false
}
