package delivery

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// CallbackEpoch is read only while the caller holds federation admission that
// excludes restore/rebind. It is not a standalone identity validation service.
func (m *MultiOutbox) CallbackEpoch(fed core.FederationName, h core.FederateHandle) (uint64, error) {
	state := (*m.subs.Load())[fedHandleKey{fed: fed, h: h}]
	if state == nil {
		return 0, core.ErrOutboxUnavailable
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed || state.key != (fedHandleKey{fed: fed, h: h}) {
		return 0, core.ErrOutboxUnavailable
	}
	return state.callbackEpoch, nil
}
