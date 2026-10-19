package ownership

import (
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.OwnershipCallbackRestorePreparer = (*Manager)(nil)

// Map-shaped composition port; the list-shaped validator also rejects duplicate
// recipients for callers whose planned roster has not yet been indexed.
func (m *Manager) PrepareOwnershipCallbackScopeRestore(fed core.FederationName, scopes map[core.FederateHandle]core.OwnershipCallbackScope) (core.OutboxReservation, error) {
	if uint64(len(scopes)) > MaxCallbackStateCount {
		return nil, core.ErrOwnershipCallbackScope
	}
	planned := make([]core.OwnershipCallbackScope, 0, len(scopes))
	for recipient, scope := range scopes {
		if recipient != scope.Recipient {
			return nil, core.ErrOwnershipCallbackScope
		}
		planned = append(planned, scope)
	}
	slices.SortFunc(planned, func(a, b core.OwnershipCallbackScope) int {
		return compareUint64(uint64(a.Recipient), uint64(b.Recipient))
	})
	return m.PrepareOwnershipCallbackRestoreScopes(fed, planned)
}

type callbackScopeRestoreReservation struct {
	m        *Manager
	sessions map[*ownershipCallbackSession]core.OwnershipCallbackScope
	once     sync.Once
}

// Roster scopes may include legacy recipients; only existing enabled sessions
// are rebound. Every imported enabled session must have a fresh target scope.
// The reservation pins ownership state; release it before queue Commit/Send.
func (m *Manager) PrepareOwnershipCallbackRestoreScopes(fed core.FederationName, scopes []core.OwnershipCallbackScope) (core.OutboxReservation, error) {
	if fed == "" || uint64(len(scopes)) > MaxCallbackStateCount {
		return nil, core.ErrOwnershipCallbackScope
	}
	planned := make(map[core.FederateHandle]core.OwnershipCallbackScope, len(scopes))
	for _, scope := range scopes {
		if !validCallbackScope(scope) || scope.Federation != fed {
			return nil, core.ErrOwnershipCallbackScope
		}
		if _, duplicate := planned[scope.Recipient]; duplicate {
			return nil, core.ErrOwnershipCallbackScope
		}
		planned[scope.Recipient] = scope
	}
	m.mu.Lock()
	r := &callbackScopeRestoreReservation{m: m, sessions: make(map[*ownershipCallbackSession]core.OwnershipCallbackScope)}
	if st := m.fed[fed]; st != nil {
		for recipient, session := range st.callbackSessions {
			if !session.enabled {
				continue
			}
			scope, exists := planned[recipient]
			if !exists || !session.restoreRequired || scope == session.scope {
				r.Release()
				return nil, core.ErrOwnershipCallbackScope
			}
			r.sessions[session] = scope
		}
	}
	return r, nil
}

func rebindCallbackSessionLocked(session *ownershipCallbackSession, scope core.OwnershipCallbackScope) {
	session.scope, session.restoreRequired = scope, false
	for _, entry := range session.pending {
		entry.event = cloneCallback(entry.event)
		entry.event.CallbackScope = scope
		entry.event.identity = &callbackIdentity{scope: scope, id: entry.event.CallbackID, strict: true}
		entry.enqueueing = false
		entry.admitted = false
		entry.lastAttempt, entry.inflightAttempt, entry.sentAttempt, entry.reportedAttempt = 0, 0, 0, 0
	}
}

func (r *callbackScopeRestoreReservation) Commit() error {
	r.once.Do(func() {
		defer r.m.mu.Unlock()
		for session, scope := range r.sessions {
			rebindCallbackSessionLocked(session, scope)
		}
	})
	return nil
}

func (r *callbackScopeRestoreReservation) Release() { r.once.Do(r.m.mu.Unlock) }
