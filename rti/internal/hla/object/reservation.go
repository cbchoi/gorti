// Object instance name reservation per IEEE 1516.1-2010 §6.1-6.5.
// M26 Phase F.
//
// Federates reserve names before registering object instances. The
// reservation table is per-federation, name-keyed, and records which
// federate holds the reservation. Registration does not release that
// right: it lasts until explicit release or resign (6.3 and 6.6).
//
// Wire semantics: ReserveObjectInstanceName returns Empty
// synchronously; the actual success/failure is delivered as a
// FederateEvent (ObjectInstanceNameReservationSucceeded / Failed)
// on the federate's event stream. The async delivery matches IEEE
// 1516.1's callback model.

package object

import (
	"slices"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// reservationStore is the per-federation name → holding-federate map.
// Goroutine-safe via a single mutex; reads + writes are short.
type reservationStore struct {
	mu sync.Mutex
	// reservations[fed][name] = holder
	reservations map[core.FederationName]map[string]core.FederateHandle
	// Registered names and reservation rights have separate lifetimes.
	// A released reservation can be acquired while its object is still live;
	// registration remains prohibited until that object is deleted.
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
// reserved, or (false, err) on validation failure.
func (s *reservationStore) Reserve(fed core.FederationName, holder core.FederateHandle, name string) (bool, error) {
	return s.reserveWithAdmission(fed, holder, name, nil)
}

// admit queues the outcome before reservation rights change. It must not
// re-enter this store; a failed admission leaves both reservation tables intact.
func (s *reservationStore) reserveWithAdmission(fed core.FederationName, holder core.FederateHandle, name string, admit func(bool) error) (bool, error) {
	if name == "" || strings.HasPrefix(name, "HLA") {
		return false, core.ErrObjectInstanceIllegalName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ok := !s.reservedLocked(fed, name)
	if admit != nil {
		if err := admit(ok); err != nil {
			return false, err
		}
	}
	if !ok {
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
// returns (nil, nil). On collision returns (collidingNames, nil) —
// NO reservation is made. Holder-side ownership applies to every
// name in the batch.
func (s *reservationStore) ReserveBatch(fed core.FederationName, holder core.FederateHandle, names []string) ([]string, error) {
	return s.reserveBatchWithAdmission(fed, holder, names, nil)
}

func (s *reservationStore) reserveBatchWithAdmission(fed core.FederationName, holder core.FederateHandle, names []string, admit func([]string) error) ([]string, error) {
	if len(names) == 0 {
		return nil, core.ErrObjectInstanceNameSetEmpty
	}
	for _, name := range names {
		if name == "" || strings.HasPrefix(name, "HLA") {
			return nil, core.ErrObjectInstanceIllegalName
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Pass 1: collect collisions. Atomic check-then-act.
	var colliding []string
	for _, n := range names {
		if s.reservedLocked(fed, n) {
			colliding = append(colliding, n)
		}
	}
	// Detect duplicate names within the batch — same name twice in
	// one batch is itself a collision.
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if _, dup := seen[n]; dup {
			// Re-collide if not already in the list.
			if !slices.Contains(colliding, n) {
				colliding = append(colliding, n)
			}
		}
		seen[n] = struct{}{}
	}
	if admit != nil {
		if err := admit(colliding); err != nil {
			return nil, err
		}
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

// register validates name ownership, persists registration, and marks the name
// registered without releasing its reservation. On persistence failure neither
// table changes. Holding the lock also orders concurrent Reserve/Release calls
// against the result. persist must not re-enter the reservation store.
func (s *reservationStore) register(fed core.FederationName, holder core.FederateHandle, name string, requireReservation bool, persist func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, occupied := s.registered[fed][name]; occupied {
		return core.ErrObjectInstanceNameInUse
	}
	owner, reserved := s.reservations[fed][name]
	if requireReservation && (!reserved || owner != holder) {
		return core.ErrObjectInstanceNameNotReserved
	}
	if !requireReservation && reserved {
		return core.ErrObjectInstanceNameReservedByOther
	}
	if err := persist(); err != nil {
		return err
	}
	registered := s.registered[fed]
	if registered == nil {
		registered = make(map[string]struct{})
		s.registered[fed] = registered
	}
	registered[name] = struct{}{}
	return nil
}

// MarkRegistered records a name as registered without going through
// a prior reservation. Registration with persistence uses register instead so
// a failed write does not leave an occupied name without an object instance.
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
// when the instance is deleted, freeing the name for registration).
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

// reservedLocked checks reservation rights, not live object occupancy.
// Caller must hold s.mu.
func (s *reservationStore) reservedLocked(fed core.FederationName, name string) bool {
	if m, ok := s.reservations[fed]; ok {
		if _, has := m[name]; has {
			return true
		}
	}
	return false
}
