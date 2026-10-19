package ownership

import (
	"context"
	"errors"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func needsTerminalAdmission(entry *terminalCallback) bool {
	return !entry.admitted && !entry.enqueueing && entry.inflightAttempt == 0 && entry.sentAttempt == 0
}

// PendingOwnershipAdmissions contains only outcomes that never reached a queue,
// or whose wire delivery failed. Awaiting invocation receipts are not resent.
func (m *Manager) PendingOwnershipAdmissions() []core.OwnershipCallbackScope {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var scopes []core.OwnershipCallbackScope
	for _, st := range m.fed {
		for _, session := range st.callbackSessions {
			if session.restoreRequired {
				continue
			}
			for _, entry := range session.pending {
				if needsTerminalAdmission(entry) {
					scopes = append(scopes, session.scope)
					break
				}
			}
		}
	}
	slices.SortFunc(scopes, func(a, b core.OwnershipCallbackScope) int {
		if a.Federation < b.Federation {
			return -1
		}
		if a.Federation > b.Federation {
			return 1
		}
		return compareUint64(uint64(a.Recipient), uint64(b.Recipient))
	})
	return scopes
}

func (m *Manager) RetryUnadmittedOwnershipCallbacks(ctx context.Context, scope core.OwnershipCallbackScope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.RLock()
	st := m.fed[scope.Federation]
	if st == nil {
		m.mu.RUnlock()
		return core.ErrOwnershipCallbackScope
	}
	session := st.callbackSessions[scope.Recipient]
	if session == nil || session.restoreRequired || session.scope != scope {
		m.mu.RUnlock()
		return core.ErrOwnershipCallbackScope
	}
	var events []*CallbackEvent
	for _, entry := range session.pending {
		if needsTerminalAdmission(entry) {
			events = append(events, cloneCallback(entry.event))
		}
	}
	m.mu.RUnlock()
	slices.SortFunc(events, func(a, b *CallbackEvent) int { return compareUint64(a.CallbackID, b.CallbackID) })
	var failures []error
	for _, event := range events {
		if err := m.sendTerminalCallback(ctx, scope.Federation, scope.Recipient, event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
