package session

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// Host requires the caller's membership/generation operation lease, as does
// Authenticate. It returns no value for an unreported or replaced session.
func (m *Manager) Host(fed core.FederationName, h core.FederateHandle) (string, bool, error) {
	id, err := m.opts.Membership.MemberIdentity(fed, h)
	if err != nil {
		return "", false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[identityKey(id)]
	if m.closed || e == nil || e.identity.Incarnation != id.Incarnation || e.host == "" {
		return "", false, nil
	}
	return e.host, true, nil
}
